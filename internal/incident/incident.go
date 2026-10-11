package incident

import (
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

type Incident struct {
	ID          string     `json:"id"`
	MonitorName string     `json:"monitorName"`
	FiredAt     time.Time  `json:"firedAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Status      Status     `json:"status"`
	Value       any        `json:"value,omitempty"`
	Message     string     `json:"message,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	MonitorType string     `json:"monitorType,omitempty"`
}

type IncidentSummary struct {
	ID          string     `json:"id"`
	MonitorName string     `json:"monitorName"`
	FiredAt     time.Time  `json:"firedAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Status      Status     `json:"status"`
}

type Index struct {
	Incidents []IncidentSummary `json:"incidents"`
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
		ID:          inc.ID,
		MonitorName: inc.MonitorName,
		FiredAt:     inc.FiredAt,
		ResolvedAt:  inc.ResolvedAt,
		Status:      inc.Status,
	}
}

// InitialMarkdown returns the starting markdown content for a new incident.
// Investigations are performed externally (e.g. via the MCP server); the
// incident log records the alarm details and the eventual resolution.
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
	return sb.String()
}
