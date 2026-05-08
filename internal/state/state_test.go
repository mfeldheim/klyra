package state_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func TestSilenceIsActive(t *testing.T) {
	s := state.Silence{Until: time.Now().Add(time.Hour)}
	if !s.IsActive(time.Now()) {
		t.Fatal("expected silence to be active")
	}
	expired := state.Silence{Until: time.Now().Add(-time.Minute)}
	if expired.IsActive(time.Now()) {
		t.Fatal("expected expired silence to be inactive")
	}
}

func TestPersistedStateTrim(t *testing.T) {
	ps := &state.PersistedState{}
	old := time.Now().Add(-25 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	ps.History = []state.HistoryEvent{
		{At: old, MonitorName: "a"},
		{At: recent, MonitorName: "b"},
	}
	ps.Trim(24*time.Hour, time.Now())
	if len(ps.History) != 1 || ps.History[0].MonitorName != "b" {
		t.Fatalf("expected 1 recent event, got %+v", ps.History)
	}
}

func TestPersistedStateTrimEmpty(t *testing.T) {
	ps := &state.PersistedState{}
	ps.Trim(24*time.Hour, time.Now()) // should not panic
	if len(ps.History) != 0 {
		t.Fatal("expected empty history")
	}
}

func TestPersistedStateTrimAllOld(t *testing.T) {
	ps := &state.PersistedState{}
	ps.History = []state.HistoryEvent{
		{At: time.Now().Add(-48 * time.Hour), MonitorName: "a"},
		{At: time.Now().Add(-36 * time.Hour), MonitorName: "b"},
	}
	ps.Trim(24*time.Hour, time.Now())
	if len(ps.History) != 0 {
		t.Fatalf("expected 0 events, got %d", len(ps.History))
	}
}

func TestPersistedStateTrimNoneOld(t *testing.T) {
	ps := &state.PersistedState{}
	ps.History = []state.HistoryEvent{
		{At: time.Now().Add(-1 * time.Hour), MonitorName: "a"},
		{At: time.Now().Add(-2 * time.Hour), MonitorName: "b"},
	}
	ps.Trim(24*time.Hour, time.Now())
	if len(ps.History) != 2 {
		t.Fatalf("expected 2 events, got %d", len(ps.History))
	}
}
