package engine_test

import (
	"context"
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

	d := engine.NewDispatcher(st, actionMap, monitorActions)
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

	d := engine.NewDispatcher(st, actionMap, monitorActions)
	ev := state.AlarmEvent{MonitorName: "test", Transition: state.TransitionFiring, FiredAt: time.Now()}
	d.Dispatch(context.Background(), ev)

	if len(fa.fired) != 0 {
		t.Fatalf("expected 0 fired events (silenced), got %d", len(fa.fired))
	}
}
