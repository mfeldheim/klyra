package incident

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusResolved Status = "resolved"
)

type InvestigationStatus string

const (
	InvPending  InvestigationStatus = "pending"
	InvRunning  InvestigationStatus = "running"
	InvComplete InvestigationStatus = "complete"
	InvFailed   InvestigationStatus = "failed"
)

type Incident struct {
	ID                  string              `json:"id"`
	MonitorName         string              `json:"monitorName"`
	FiredAt             time.Time           `json:"firedAt"`
	ResolvedAt          *time.Time          `json:"resolvedAt,omitempty"`
	Status              Status              `json:"status"`
	InvestigationStatus InvestigationStatus `json:"investigationStatus"`
	Value               any                 `json:"value,omitempty"`
	Message             string              `json:"message,omitempty"`
	Icon                string              `json:"icon,omitempty"`
	MonitorType         string              `json:"monitorType,omitempty"`
}

type IncidentSummary struct {
	ID                  string              `json:"id"`
	MonitorName         string              `json:"monitorName"`
	FiredAt             time.Time           `json:"firedAt"`
	ResolvedAt          *time.Time          `json:"resolvedAt,omitempty"`
	Status              Status              `json:"status"`
	InvestigationStatus InvestigationStatus `json:"investigationStatus"`
}

type Index struct {
	Incidents []IncidentSummary `json:"incidents"`
}

// ConvRole is the role in a Bedrock conversation.
type ConvRole string

const (
	RoleUser      ConvRole = "user"
	RoleAssistant ConvRole = "assistant"
)

// ConvBlock is one content block in a conversation message.
type ConvBlock struct {
	Type       string          `json:"type"`                  // "text" | "tool_use" | "tool_result"
	Text       string          `json:"text,omitempty"`        // for text
	ID         string          `json:"id,omitempty"`          // tool_use id / tool_result id
	Name       string          `json:"name,omitempty"`        // tool name
	Input      json.RawMessage `json:"input,omitempty"`       // tool_use input JSON
	ToolResult string          `json:"toolResult,omitempty"`  // tool_result content
	IsError    bool            `json:"isError,omitempty"`
}

// ConvMessage is one turn in the Bedrock conversation.
type ConvMessage struct {
	Role   ConvRole    `json:"role"`
	Blocks []ConvBlock `json:"blocks"`
}

// NewID generates a unique incident ID from the monitor name and timestamp.
func NewID(monitorName string, t time.Time) string {
	slug := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, monitorName)
	slug = strings.Trim(slug, "-")
	ts := t.UTC().Format("20060102-150405")
	short := uuid.New().String()[:8]
	return fmt.Sprintf("inc-%s-%s-%s", slug, ts, short)
}

// Summary returns an IncidentSummary for use in the index.
func (inc *Incident) Summary() IncidentSummary {
	return IncidentSummary{
		ID:                  inc.ID,
		MonitorName:         inc.MonitorName,
		FiredAt:             inc.FiredAt,
		ResolvedAt:          inc.ResolvedAt,
		Status:              inc.Status,
		InvestigationStatus: inc.InvestigationStatus,
	}
}

// InitialMarkdown returns the starting markdown content for a new incident.
func (inc *Incident) InitialMarkdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Incident: %s\n\n", inc.MonitorName)
	fmt.Fprintf(&sb, "**ID:** %s  \n", inc.ID)
	fmt.Fprintf(&sb, "**Monitor:** %s  \n", inc.MonitorName)
	fmt.Fprintf(&sb, "**Fired At:** %s  \n", inc.FiredAt.UTC().Format(time.RFC3339))
	if inc.Value != nil {
		fmt.Fprintf(&sb, "**Value:** %v  \n", inc.Value)
	}
	if inc.Message != "" {
		fmt.Fprintf(&sb, "**Message:** %s  \n", inc.Message)
	}
	fmt.Fprintf(&sb, "**Status:** %s  \n", inc.Status)
	fmt.Fprintf(&sb, "**Investigation:** %s  \n", inc.InvestigationStatus)
	sb.WriteString("\n---\n\n## Investigation\n\n")
	return sb.String()
}
