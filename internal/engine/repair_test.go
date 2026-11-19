package engine_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestRepairStaleFiring(t *testing.T) {
	st := state.NewStore()
	firedAt := time.Date(2026, 11, 18, 21, 40, 38, 0, time.UTC)
	now := firedAt.Add(15 * time.Hour)
	incID := "inc-opendi-api-1"
	st.SetAlarm(state.AlarmState{MonitorName: "stale", Status: state.AlarmOK, FiredAt: &firedAt, IncidentID: &incID})
	st.SetAlarm(state.AlarmState{MonitorName: "firing", Status: state.AlarmFiring, FiredAt: &firedAt})
	st.SetAlarm(state.AlarmState{MonitorName: "clean", Status: state.AlarmOK})

	got := engine.RepairStaleFiring(st, now)
	if len(got) != 1 || got[0] != "stale" {
		t.Fatalf("repaired = %v, want [stale]", got)
	}
	a, _ := st.GetAlarm("stale")
	if a.FiredAt != nil || a.IncidentID != nil {
		t.Errorf("stale fire state not cleared: %+v", a)
	}
	if f, _ := st.GetAlarm("firing"); f.FiredAt == nil {
		t.Error("firing alarm must keep its FiredAt")
	}
	h := st.History()
	if len(h) != 1 || h[0].Transition != state.TransitionResolved || !h[0].At.Equal(now) || h[0].IncidentID != incID {
		t.Errorf("unexpected history: %+v", h)
	}
	if again := engine.RepairStaleFiring(st, now); len(again) != 0 {
		t.Errorf("second run repaired %v, want none", again)
	}
}
