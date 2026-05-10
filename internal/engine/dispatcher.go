package engine

import (
	"context"
	"log"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
)

// Dispatcher fires actions on alarm state transitions, checking silences first.
type Dispatcher struct {
	store          *state.Store
	actions        map[string]action.Action
	monitorActions map[string][]string // monitorName → []actionName
	incMgr         *incident.Manager  // nil if incidents not configured
}

// NewDispatcher creates a new Dispatcher. incMgr may be nil.
func NewDispatcher(st *state.Store, actions map[string]action.Action, monitorActions map[string][]string, incMgr *incident.Manager) *Dispatcher {
	return &Dispatcher{
		store:          st,
		actions:        actions,
		monitorActions: monitorActions,
		incMgr:         incMgr,
	}
}

// Dispatch fires all actions for the given AlarmEvent, skipping silenced monitors.
func (d *Dispatcher) Dispatch(ctx context.Context, ev state.AlarmEvent) {
	if d.store.IsSilenced(ev.MonitorName) {
		return
	}

	// Create or close incidents before firing actions so IncidentID is available.
	if d.incMgr != nil {
		switch ev.Transition {
		case state.TransitionFiring:
			inc, err := d.incMgr.OnFiring(ctx, ev)
			if err != nil {
				log.Printf("dispatcher: create incident for %q: %v", ev.MonitorName, err)
			} else {
				ev.IncidentID = inc.ID
				// Persist the incident ID on the alarm state so the API can surface it.
				if alarm, ok := d.store.GetAlarm(ev.MonitorName); ok {
					alarm.IncidentID = &inc.ID
					d.store.SetAlarm(alarm)
				}
			}
		case state.TransitionResolved:
			if err := d.incMgr.OnResolved(ctx, ev); err != nil {
				log.Printf("dispatcher: close incident for %q: %v", ev.MonitorName, err)
			}
			// Clear the incident ID from alarm state on resolution.
			if alarm, ok := d.store.GetAlarm(ev.MonitorName); ok {
				alarm.IncidentID = nil
				d.store.SetAlarm(alarm)
			}
		}
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
