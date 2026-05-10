package incident

import (
	"strings"
	"testing"
	"time"
)

func TestNewID(t *testing.T) {
	t0 := time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC)

	id := NewID("api-latency", t0)
	if !strings.HasPrefix(id, "inc-api-latency-20260510-143000-") {
		t.Errorf("unexpected ID prefix: %s", id)
	}
	if len(id) != len("inc-api-latency-20260510-143000-")+8 {
		t.Errorf("unexpected ID length: %s", id)
	}

	// Special characters become dashes
	id2 := NewID("My Monitor!", t0)
	if !strings.HasPrefix(id2, "inc-my-monitor-") {
		t.Errorf("unexpected ID with special chars: %s", id2)
	}

	// Two calls produce different IDs
	if NewID("x", t0) == NewID("x", t0) {
		t.Error("expected unique IDs")
	}
}

func TestInitialMarkdown(t *testing.T) {
	firedAt := time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC)
	inc := &Incident{
		ID:                  "inc-test-20260510-143000-abcd1234",
		MonitorName:         "test",
		FiredAt:             firedAt,
		Value:               "842ms",
		Message:             "p99 above threshold",
		Status:              StatusActive,
		InvestigationStatus: InvPending,
	}
	md := inc.InitialMarkdown()
	if !strings.Contains(md, "## Investigation") {
		t.Error("expected Investigation section in markdown")
	}
	if !strings.Contains(md, inc.ID) {
		t.Error("expected ID in markdown")
	}
}

func TestSummary(t *testing.T) {
	inc := &Incident{
		ID:                  "inc-x",
		MonitorName:         "x",
		FiredAt:             time.Now(),
		Status:              StatusActive,
		InvestigationStatus: InvPending,
	}
	s := inc.Summary()
	if s.ID != inc.ID || s.MonitorName != inc.MonitorName {
		t.Error("summary fields mismatch")
	}
}
