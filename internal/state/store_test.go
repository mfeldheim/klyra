package state_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func TestStoreGetSet(t *testing.T) {
	s := state.NewStore()
	alarm := state.AlarmState{MonitorName: "test", Status: state.AlarmOK}
	s.SetAlarm(alarm)

	got, ok := s.GetAlarm("test")
	if !ok {
		t.Fatal("expected alarm to exist")
	}
	if got.MonitorName != "test" {
		t.Errorf("unexpected name: %s", got.MonitorName)
	}
}

func TestStoreAppendHistory(t *testing.T) {
	s := state.NewStore()
	s.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionFiring, At: time.Now()})
	s.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionResolved, At: time.Now()})

	h := s.History()
	if len(h) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(h))
	}
}

func TestStoreDirty(t *testing.T) {
	s := state.NewStore()
	if s.IsDirty() {
		t.Fatal("new store should not be dirty")
	}
	s.SetAlarm(state.AlarmState{MonitorName: "x"})
	if !s.IsDirty() {
		t.Fatal("store should be dirty after SetAlarm")
	}
	s.ClearDirty()
	if s.IsDirty() {
		t.Fatal("store should not be dirty after ClearDirty")
	}
}

func TestStoreActiveSilences(t *testing.T) {
	s := state.NewStore()
	s.AddSilence(state.Silence{ID: "1", MonitorName: "test", Until: time.Now().Add(time.Hour)})
	s.AddSilence(state.Silence{ID: "2", MonitorName: "other", Until: time.Now().Add(-time.Minute)})

	if !s.IsSilenced("test") {
		t.Error("expected test to be silenced")
	}
	if s.IsSilenced("other") {
		t.Error("expected other not to be silenced (expired)")
	}
}
