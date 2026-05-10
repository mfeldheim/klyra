package investigate

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
	"k8s.io/client-go/kubernetes/fake"
)

// fakeStream implements StreamResponse for testing.
type fakeStream struct {
	ch chan brtypes.ConverseStreamOutput
}

func newFakeStream(events ...brtypes.ConverseStreamOutput) *fakeStream {
	ch := make(chan brtypes.ConverseStreamOutput, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return &fakeStream{ch: ch}
}

func (f *fakeStream) Events() <-chan brtypes.ConverseStreamOutput { return f.ch }
func (f *fakeStream) Err() error                                  { return nil }

// fakeBedrockClient returns a single end_turn response with predefined text.
type fakeBedrockClient struct {
	response string
}

func (f *fakeBedrockClient) ConverseStream(_ context.Context, _ *bedrockruntime.ConverseStreamInput, _ ...func(*bedrockruntime.Options)) (StreamResponse, error) {
	return newFakeStream(
		&brtypes.ConverseStreamOutputMemberContentBlockDelta{
			Value: brtypes.ContentBlockDeltaEvent{
				Delta: &brtypes.ContentBlockDeltaMemberText{Value: f.response},
			},
		},
		&brtypes.ConverseStreamOutputMemberMessageStop{
			Value: brtypes.MessageStopEvent{StopReason: brtypes.StopReasonEndTurn},
		},
	), nil
}

func TestAgentInvestigatePromptBuilding(t *testing.T) {
	ev := state.AlarmEvent{
		MonitorName: "api-latency",
		Value:       "842ms",
		Message:     "p99 above threshold",
		FiredAt:     time.Now(),
	}
	prompt := buildInvestigationPrompt(ev)
	if !contains(prompt, "api-latency") {
		t.Error("expected monitor name in prompt")
	}
	if !contains(prompt, "842ms") {
		t.Error("expected value in prompt")
	}
	if !contains(prompt, "Root cause") {
		t.Error("expected conclusion instructions in prompt")
	}
}

func TestAgentInvestigate(t *testing.T) {
	client := &fakeBedrockClient{response: "The issue is OOMKilled pods."}
	tools := NewK8sTools(fake.NewSimpleClientset(), nil)
	agent := NewAgent(client, tools, "test-model")

	ev := state.AlarmEvent{
		MonitorName: "api-latency",
		Value:       "high",
		FiredAt:     time.Now(),
	}

	var history []incident.ConvMessage
	var got string
	err := agent.Investigate(context.Background(), ev, &history, func(text string) { got += text })
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	if !contains(got, "OOMKilled") {
		t.Errorf("expected streamed response, got: %q", got)
	}
	if len(history) != 2 { // user + assistant
		t.Errorf("expected 2 history messages, got %d", len(history))
	}
}

func TestAgentChat(t *testing.T) {
	client := &fakeBedrockClient{response: "Restarting should help."}
	tools := NewK8sTools(fake.NewSimpleClientset(), nil)
	agent := NewAgent(client, tools, "test-model")

	history := []incident.ConvMessage{
		{Role: incident.RoleUser, Blocks: []incident.ConvBlock{{Type: "text", Text: "initial"}}},
		{Role: incident.RoleAssistant, Blocks: []incident.ConvBlock{{Type: "text", Text: "findings"}}},
	}

	var got string
	err := agent.Chat(context.Background(), &history, "what should I do?", func(text string) { got += text })
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !contains(got, "Restarting") {
		t.Errorf("expected chat response, got: %q", got)
	}
}

func TestToBedrockMessagesRoundtrip(t *testing.T) {
	history := []incident.ConvMessage{
		{
			Role:   incident.RoleUser,
			Blocks: []incident.ConvBlock{{Type: "text", Text: "hello"}},
		},
		{
			Role: incident.RoleAssistant,
			Blocks: []incident.ConvBlock{
				{Type: "text", Text: "I will check the pods"},
				{Type: "tool_use", ID: "tu1", Name: "list_pods", Input: []byte(`{"namespace":"default"}`)},
			},
		},
		{
			Role: incident.RoleUser,
			Blocks: []incident.ConvBlock{
				{Type: "tool_result", ID: "tu1", ToolResult: "pod1: Running"},
			},
		},
	}

	msgs, err := toBedrockMessages(history)
	if err != nil {
		t.Fatalf("toBedrockMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if len(msgs[1].Content) != 2 {
		t.Errorf("expected 2 content blocks in assistant message, got %d", len(msgs[1].Content))
	}
}

func TestBuildToolConfig(t *testing.T) {
	cfg := buildToolConfig()
	if cfg == nil {
		t.Fatal("expected non-nil tool config")
	}
	if len(cfg.Tools) == 0 {
		t.Error("expected at least one tool")
	}
}
