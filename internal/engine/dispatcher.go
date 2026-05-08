package engine

import (
	"context"
	"log"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/state"
)

// Dispatcher fires actions on alarm state transitions, checking silences first.
type Dispatcher struct {
	store          *state.Store
	actions        map[string]action.Action
	monitorActions map[string][]string // monitorName → []actionName
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher(st *state.Store, actions map[string]action.Action, monitorActions map[string][]string) *Dispatcher {
	return &Dispatcher{
		store:          st,
		actions:        actions,
		monitorActions: monitorActions,
	}
}

// Dispatch fires all actions for the given AlarmEvent, skipping silenced monitors.
func (d *Dispatcher) Dispatch(ctx context.Context, ev state.AlarmEvent) {
	if d.store.IsSilenced(ev.MonitorName) {
		return
	}

	names, ok := d.monitorActions[ev.MonitorName]
	if !ok {
		return
	}

	for _, name := range names {
		a, found := d.actions[name]
		if !found {
			log.Printf("dispatcher: action %q not found for monitor %q", name, ev.MonitorName)
			continue
		}
		if err := a.Fire(ctx, ev); err != nil {
			log.Printf("dispatcher: action %q fire error for monitor %q: %v", name, ev.MonitorName, err)
		}
	}
}
