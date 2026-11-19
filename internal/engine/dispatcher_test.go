package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/state"
)

type fakeAction struct {
	name  string
	fired []state.AlarmEvent
}

func (f *fakeAction) Name() string { return f.name }
func (f *fakeAction) Fire(_ context.Context, ev state.AlarmEvent) error {
	f.fired = append(f.fired, ev)
	return nil
}

func TestDispatcherFiresOnEvent(t *testing.T) {
	fa := &fakeAction{name: "notify"}
	actionMap := map[string]action.Action{"notify": fa}
	monitorActions := map[string][]string{"test": {"notify"}}
	st := state.NewStore()

	d := engine.NewDispatcher(st, actionMap, monitorActions, nil)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 1 {
		t.Fatalf("expected 1 fired event, got %d", len(fa.fired))
	}
}

func TestDispatcherSkipsSilenced(t *testing.T) {
	fa := &fakeAction{name: "notify"}
	actionMap := map[string]action.Action{"notify": fa}
	monitorActions := map[string][]string{"test": {"notify"}}
	st := state.NewStore()
	st.AddSilence(state.Silence{ID: "1", MonitorName: "test", Until: time.Now().Add(time.Hour)})

	d := engine.NewDispatcher(st, actionMap, monitorActions, nil)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 0 {
		t.Fatalf("expected 0 fired events (silenced), got %d", len(fa.fired))
	}
}

func TestDispatcherSkipsMissingAction(t *testing.T) {
	fa := &fakeAction{name: "real"}
	actionMap := map[string]action.Action{"real": fa}
	// "ghost" doesn't exist in actionMap
	monitorActions := map[string][]string{"test": {"ghost", "real"}}
	st := state.NewStore()

	d := engine.NewDispatcher(st, actionMap, monitorActions, nil)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 1 {
		t.Fatalf("expected 1 fired event (ghost skipped, real fires), got %d", len(fa.fired))
	}
}

type errorAction struct {
	name  string
	fired int
}

func (e *errorAction) Name() string { return e.name }
func (e *errorAction) Fire(_ context.Context, _ state.AlarmEvent) error {
	e.fired++
	return fmt.Errorf("simulated failure")
}

func TestDispatcherContinuesOnFireError(t *testing.T) {
	errA := &errorAction{name: "err"}
	okA := &fakeAction{name: "ok"}
	actionMap := map[string]action.Action{"err": errA, "ok": okA}
	monitorActions := map[string][]string{"test": {"err", "ok"}}
	st := state.NewStore()

	d := engine.NewDispatcher(st, actionMap, monitorActions, nil)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if errA.fired != 1 {
		t.Errorf("expected errAction to be called once, got %d", errA.fired)
	}
	if len(okA.fired) != 1 {
		t.Errorf("expected ok action to fire despite error, got %d", len(okA.fired))
	}
}

// TestDispatcherRecordsTransitionTime guards against recording a RESOLVED
// history event at the original fire time: the history must show when the
// transition happened and which incident it closed.
func TestDispatcherRecordsTransitionTime(t *testing.T) {
	st := state.NewStore()
	firedAt := time.Date(2026, 11, 18, 13, 51, 4, 0, time.UTC)
	resolvedAt := firedAt.Add(2 * time.Minute)
	incID := "inc-test-1"
	st.SetAlarm(state.AlarmState{MonitorName: "test", Status: state.AlarmOK, IncidentID: &incID})

	d := engine.NewDispatcher(st, nil, nil, nil)
	d.Dispatch(context.Background(), state.AlarmEvent{
		MonitorName: "test",
		Transition:  state.TransitionResolved,
		FiredAt:     firedAt,
		At:          resolvedAt,
		IncidentID:  incID,
	})

	h := st.History()
	if len(h) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(h))
	}
	if !h[0].At.Equal(resolvedAt) {
		t.Errorf("history At = %v, want resolve time %v", h[0].At, resolvedAt)
	}
	if h[0].IncidentID != incID {
		t.Errorf("history IncidentID = %q, want %q", h[0].IncidentID, incID)
	}
}
