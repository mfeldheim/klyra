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
