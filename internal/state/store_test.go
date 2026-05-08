package state_test

import (
	"fmt"
	"sync"
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

func TestStoreRemoveSilence(t *testing.T) {
	s := state.NewStore()
	s.AddSilence(state.Silence{ID: "s1", MonitorName: "test", Until: time.Now().Add(time.Hour)})

	if !s.RemoveSilence("s1") {
		t.Error("expected RemoveSilence to return true for existing ID")
	}
	if s.RemoveSilence("s1") {
		t.Error("expected RemoveSilence to return false for already-removed ID")
	}
	if s.IsSilenced("test") {
		t.Error("expected monitor to no longer be silenced after removal")
	}
}

func TestStoreWildcardSilence(t *testing.T) {
	s := state.NewStore()
	s.AddSilence(state.Silence{ID: "all", MonitorName: "", Until: time.Now().Add(time.Hour)})

	if !s.IsSilenced("anything") {
		t.Error("expected wildcard silence to match any monitor")
	}
	if !s.IsSilenced("other") {
		t.Error("expected wildcard silence to match other monitor")
	}
}

func TestStoreGetAlarmMissing(t *testing.T) {
	s := state.NewStore()
	_, ok := s.GetAlarm("nonexistent")
	if ok {
		t.Error("expected ok=false for missing alarm")
	}
}

func TestStoreSnapshotLoadRoundTrip(t *testing.T) {
	s := state.NewStore()
	now := time.Now()
	s.SetAlarm(state.AlarmState{MonitorName: "m1", Status: state.AlarmFiring, LastCheck: now})
	s.AppendHistory(state.HistoryEvent{MonitorName: "m1", Transition: state.TransitionFiring, At: now})

	snap := s.Snapshot(24 * time.Hour)

	s2 := state.NewStore()
	s2.LoadSnapshot(snap)

	alarm, ok := s2.GetAlarm("m1")
	if !ok {
		t.Fatal("expected alarm to exist after LoadSnapshot")
	}
	if alarm.Status != state.AlarmFiring {
		t.Errorf("expected FIRING, got %s", alarm.Status)
	}
	if len(s2.History()) != 1 {
		t.Errorf("expected 1 history event, got %d", len(s2.History()))
	}
	if s2.IsDirty() {
		t.Error("store should not be dirty after LoadSnapshot")
	}
}

func TestStoreConcurrent(t *testing.T) {
	s := state.NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		n := i
		go func() {
			defer wg.Done()
			s.SetAlarm(state.AlarmState{MonitorName: fmt.Sprintf("m%d", n)})
		}()
		go func() {
			defer wg.Done()
			_ = s.Alarms()
		}()
	}
	wg.Wait()
}
