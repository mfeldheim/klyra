package investigate

import (
	"context"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/state"
)

// globalFactory is set by RegisterInvestigateFactory before engine.New is called.
var globalFactory func(name string, cfg map[string]any) (action.Action, error)

// RegisterInvestigateFactory wires a real ai_investigate factory into the registry.
// Call this before engine.New so that action instantiation uses the real implementation.
func RegisterInvestigateFactory(mgr *incident.Manager, agent *Agent) {
	globalFactory = func(name string, cfg map[string]any) (action.Action, error) {
		return NewInvestigateAction(name, mgr, agent), nil
	}
}

func init() {
	action.Register("ai_investigate", func(name string, cfg map[string]any) (action.Action, error) {
		if globalFactory != nil {
			return globalFactory(name, cfg)
		}
		// no-op registration satisfies the registry for config validation
		return &noopAction{name: name}, nil
	})
}

type noopAction struct{ name string }

func (n *noopAction) Name() string                                      { return n.name }
func (n *noopAction) Fire(_ context.Context, _ state.AlarmEvent) error { return nil }

// investigateAction implements action.Action and triggers AI investigation.
type investigateAction struct {
	name    string
	manager *incident.Manager
	agent   *Agent
}

// NewInvestigateAction creates an ai_investigate action wired to the given manager and agent.
func NewInvestigateAction(name string, mgr *incident.Manager, agent *Agent) action.Action {
	return &investigateAction{name: name, manager: mgr, agent: agent}
}

func (a *investigateAction) Name() string { return a.name }

func (a *investigateAction) Fire(ctx context.Context, ev state.AlarmEvent) error {
	if ev.Transition != state.TransitionFiring || ev.IncidentID == "" {
		return nil
	}
	incidentID := ev.IncidentID
	mgr := a.manager
	ag := a.agent

	mgr.RunInvestigation(incidentID, func(runCtx context.Context, history *[]incident.ConvMessage, emit func(string)) error {
		return ag.Investigate(runCtx, ev, history, emit)
	})
	return nil
}

