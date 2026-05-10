package investigate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
)

// BedrockClient is the subset of the Bedrock runtime client we use.
type BedrockClient interface {
	ConverseStream(ctx context.Context, in *bedrockruntime.ConverseStreamInput, opts ...func(*bedrockruntime.Options)) (StreamResponse, error)
}

// StreamResponse abstracts the event stream returned by Bedrock for testability.
type StreamResponse interface {
	Events() <-chan brtypes.ConverseStreamOutput
	Err() error
}

// RealBedrockClient wraps the AWS SDK client and implements BedrockClient.
type RealBedrockClient struct {
	inner *bedrockruntime.Client
}

// NewRealBedrockClient wraps an AWS SDK bedrockruntime.Client.
func NewRealBedrockClient(c *bedrockruntime.Client) *RealBedrockClient {
	return &RealBedrockClient{inner: c}
}

func (r *RealBedrockClient) ConverseStream(ctx context.Context, in *bedrockruntime.ConverseStreamInput, opts ...func(*bedrockruntime.Options)) (StreamResponse, error) {
	out, err := r.inner.ConverseStream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return out.GetStream(), nil
}

// Agent runs investigations via the Bedrock Converse API.
type Agent struct {
	client     BedrockClient
	tools      *K8sTools
	model      string
	toolConfig *brtypes.ToolConfiguration
}

// NewAgent creates an Agent. model should be a Bedrock cross-region inference profile ARN or model ID.
func NewAgent(client BedrockClient, tools *K8sTools, model string) *Agent {
	return &Agent{
		client:     client,
		tools:      tools,
		model:      model,
		toolConfig: buildToolConfig(),
	}
}

func buildToolConfig() *brtypes.ToolConfiguration {
	defs := Definitions()
	tools := make([]brtypes.Tool, 0, len(defs))
	for _, def := range defs {
		def := def
		tools = append(tools, &brtypes.ToolMemberToolSpec{
			Value: brtypes.ToolSpecification{
				Name:        aws.String(def.Name),
				Description: aws.String(def.Description),
				InputSchema: &brtypes.ToolInputSchemaMemberJson{
					Value: brdoc.NewLazyDocument(def.Schema),
				},
			},
		})
	}
	return &brtypes.ToolConfiguration{Tools: tools}
}

// Investigate runs the initial investigation for a FIRING event, appending to history
// and emitting text deltas via emit.
func (a *Agent) Investigate(ctx context.Context, ev state.AlarmEvent, history *[]incident.ConvMessage, emit func(string)) error {
	prompt := buildInvestigationPrompt(ev)
	*history = append(*history, incident.ConvMessage{
		Role:   incident.RoleUser,
		Blocks: []incident.ConvBlock{{Type: "text", Text: prompt}},
	})
	return a.runLoop(ctx, history, emit)
}

// Chat appends a user message to history and runs a response turn.
func (a *Agent) Chat(ctx context.Context, history *[]incident.ConvMessage, userMsg string, emit func(string)) error {
	*history = append(*history, incident.ConvMessage{
		Role:   incident.RoleUser,
		Blocks: []incident.ConvBlock{{Type: "text", Text: userMsg}},
	})
	return a.runLoop(ctx, history, emit)
}

// Continue runs the next response turn without prepending a new message.
// Use this when the caller has already appended the user message to history.
func (a *Agent) Continue(ctx context.Context, history *[]incident.ConvMessage, emit func(string)) error {
	return a.runLoop(ctx, history, emit)
}

func buildInvestigationPrompt(ev state.AlarmEvent) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Monitor **%s** has fired.\n\n", ev.MonitorName)
	fmt.Fprintf(&sb, "- Value: %v\n", ev.Value)
	if ev.Message != "" {
		fmt.Fprintf(&sb, "- Message: %s\n", ev.Message)
	}
	fmt.Fprintf(&sb, "- Fired at: %s\n\n", ev.FiredAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	sb.WriteString("Please investigate the root cause using the available tools. " +
		"Conclude your investigation with:\n" +
		"1. **Root cause** (or top candidates if uncertain)\n" +
		"2. **Proposed fix** (specific kubectl commands or YAML)\n" +
		"3. **Confidence:** high / medium / low\n")
	return sb.String()
}

// toolUseAccum accumulates a streaming tool use block.
type toolUseAccum struct {
	id        string
	name      string
	inputJSON string
}

func (a *Agent) runLoop(ctx context.Context, history *[]incident.ConvMessage, emit func(string)) error {
	systemPrompt := []brtypes.SystemContentBlock{
		&brtypes.SystemContentBlockMemberText{
			Value: "You are an incident response assistant with read-only access to a Kubernetes cluster. " +
				"Use the available tools to investigate issues. Be systematic and thorough. " +
				"ConfigMap values are not available for security reasons — only names are shown.",
		},
	}

	for {
		msgs, err := toBedrockMessages(*history)
		if err != nil {
			return fmt.Errorf("build messages: %w", err)
		}

		out, err := a.client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
			ModelId:    aws.String(a.model),
			Messages:   msgs,
			System:     systemPrompt,
			ToolConfig: a.toolConfig,
		})
		if err != nil {
			return fmt.Errorf("ConverseStream: %w", err)
		}

		var textBuf strings.Builder
		var toolUses []toolUseAccum
		var curTool *toolUseAccum
		var stopReason brtypes.StopReason

		for event := range out.Events() {
			switch v := event.(type) {
			case *brtypes.ConverseStreamOutputMemberContentBlockStart:
				if tu, ok := v.Value.Start.(*brtypes.ContentBlockStartMemberToolUse); ok {
					curTool = &toolUseAccum{
						id:   aws.ToString(tu.Value.ToolUseId),
						name: aws.ToString(tu.Value.Name),
					}
				}
			case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
				switch d := v.Value.Delta.(type) {
				case *brtypes.ContentBlockDeltaMemberText:
					textBuf.WriteString(d.Value)
					emit(d.Value)
				case *brtypes.ContentBlockDeltaMemberToolUse:
					if curTool != nil && d.Value.Input != nil {
						curTool.inputJSON += *d.Value.Input
					}
				}
			case *brtypes.ConverseStreamOutputMemberContentBlockStop:
				if curTool != nil {
					toolUses = append(toolUses, *curTool)
					curTool = nil
				}
			case *brtypes.ConverseStreamOutputMemberMessageStop:
				stopReason = v.Value.StopReason
			}
		}
		if err := out.Err(); err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		// Append assistant message to history
		assistantMsg := incident.ConvMessage{Role: incident.RoleAssistant}
		if textBuf.Len() > 0 {
			assistantMsg.Blocks = append(assistantMsg.Blocks, incident.ConvBlock{
				Type: "text",
				Text: textBuf.String(),
			})
		}
		for _, tu := range toolUses {
			inputJSON := tu.inputJSON
			if inputJSON == "" {
				inputJSON = "{}"
			}
			assistantMsg.Blocks = append(assistantMsg.Blocks, incident.ConvBlock{
				Type:  "tool_use",
				ID:    tu.id,
				Name:  tu.name,
				Input: json.RawMessage(inputJSON),
			})
		}
		*history = append(*history, assistantMsg)

		if stopReason != brtypes.StopReasonToolUse {
			break
		}

		// Execute tools and build tool result message
		userMsg := incident.ConvMessage{Role: incident.RoleUser}
		for _, tu := range toolUses {
			result := a.tools.Execute(ctx, tu.name, json.RawMessage(tu.inputJSON))
			content := result.Content
			if content == "" {
				content = "(no output)"
			}
			userMsg.Blocks = append(userMsg.Blocks, incident.ConvBlock{
				Type:       "tool_result",
				ID:         tu.id,
				ToolResult: content,
				IsError:    result.IsError,
			})
		}
		*history = append(*history, userMsg)
	}

	return nil
}

func toBedrockMessages(msgs []incident.ConvMessage) ([]brtypes.Message, error) {
	out := make([]brtypes.Message, 0, len(msgs))
	for _, m := range msgs {
		var role brtypes.ConversationRole
		switch m.Role {
		case incident.RoleUser:
			role = brtypes.ConversationRoleUser
		case incident.RoleAssistant:
			role = brtypes.ConversationRoleAssistant
		default:
			return nil, fmt.Errorf("unknown role: %s", m.Role)
		}

		content := make([]brtypes.ContentBlock, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			switch b.Type {
			case "text":
				content = append(content, &brtypes.ContentBlockMemberText{Value: b.Text})
			case "tool_use":
				var inputMap map[string]any
				if len(b.Input) > 0 {
					json.Unmarshal(b.Input, &inputMap) //nolint:errcheck
				}
				if inputMap == nil {
					inputMap = map[string]any{}
				}
				content = append(content, &brtypes.ContentBlockMemberToolUse{
					Value: brtypes.ToolUseBlock{
						ToolUseId: aws.String(b.ID),
						Name:      aws.String(b.Name),
						Input:     brdoc.NewLazyDocument(inputMap),
					},
				})
			case "tool_result":
				content = append(content, &brtypes.ContentBlockMemberToolResult{
					Value: brtypes.ToolResultBlock{
						ToolUseId: aws.String(b.ID),
						Content: []brtypes.ToolResultContentBlock{
							&brtypes.ToolResultContentBlockMemberText{Value: b.ToolResult},
						},
					},
				})
			}
		}
		out = append(out, brtypes.Message{Role: role, Content: content})
	}
	return out, nil
}
