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
	ps.Trim(24 * time.Hour)
	if len(ps.History) != 1 || ps.History[0].MonitorName != "b" {
		t.Fatalf("expected 1 recent event, got %+v", ps.History)
	}
}
