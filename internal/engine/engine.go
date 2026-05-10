package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/monitor"
	k8smon "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	"github.com/mfeldheim/klyra/internal/state"
)

const defaultMonitorInterval = 30 * time.Second

// Engine wires together monitors, evaluator, dispatcher, and state writer.
type Engine struct {
	cfg        *config.Config
	store      *state.Store
	dispatcher *Dispatcher
	writer     *StateWriter
	k8sClient  kubernetes.Interface
	namespace  string
}

// New creates an Engine from configuration, wiring all subsystems.
func New(cfg *config.Config, st *state.Store, k8sClient kubernetes.Interface, namespace string) (*Engine, error) {
	// Build actions map.
	actions := make(map[string]action.Action, len(cfg.Actions))
	for _, ac := range cfg.Actions {
		a, err := action.New(ac.Type, ac.Name, ac.Config)
		if err != nil {
			return nil, fmt.Errorf("engine: build action %q: %w", ac.Name, err)
		}
		actions[ac.Name] = a
	}

	// Build monitor→actions map.
	monitorActions := make(map[string][]string, len(cfg.Monitors))
	for _, mc := range cfg.Monitors {
		if len(mc.Actions) > 0 {
			monitorActions[mc.Name] = mc.Actions
		}
	}

	dispatcher := NewDispatcher(st, actions, monitorActions, nil)
	writer := NewStateWriter(st, k8sClient, namespace, "klyra-state")

	return &Engine{
		cfg:        cfg,
		store:      st,
		dispatcher: dispatcher,
		writer:     writer,
		k8sClient:  k8sClient,
		namespace:  namespace,
	}, nil
}

// SetIncidentManager wires an incident.Manager into the dispatcher.
// Call before Run.
func (e *Engine) SetIncidentManager(mgr *incident.Manager) {
	e.dispatcher.incMgr = mgr
}

// Run starts all monitors, the state writer, and the evaluate loop.
// It returns when ctx is cancelled (after all goroutines exit).
func (e *Engine) Run(ctx context.Context) error {
	results := make(chan state.CheckResult, 100)

	// Phase 1: instantiate all monitors before starting any goroutines.
	type monitorEntry struct {
		m        monitor.Monitor
		interval time.Duration
		thr      config.ThresholdConfig
	}
	monitors := make([]monitorEntry, 0, len(e.cfg.Monitors))
	for _, mc := range e.cfg.Monitors {
		var m monitor.Monitor
		var err error
		if mc.Type == "kubernetes" {
			m, err = k8smon.NewWithClient(mc.Name, mc.Config, e.k8sClient)
		} else {
			m, err = monitor.New(mc.Type, mc.Name, mc.Config)
		}
		if err != nil {
			return fmt.Errorf("engine: instantiate monitor %q: %w", mc.Name, err)
		}
		interval := mc.Interval.Duration
		if interval == 0 {
			interval = defaultMonitorInterval
		}
		monitors = append(monitors, monitorEntry{m, interval, mc.Threshold})
	}

	// Build threshold map and monitor metadata (icon, priority).
	type monitorMeta struct {
		icon     string
		priority *int
	}
	thresholds := make(map[string]config.ThresholdConfig, len(e.cfg.Monitors))
	meta := make(map[string]monitorMeta, len(e.cfg.Monitors))
	for _, mc := range e.cfg.Monitors {
		thresholds[mc.Name] = mc.Threshold
		meta[mc.Name] = monitorMeta{
			icon:     state.ResolveIcon(mc.Icon),
			priority: state.ParsePriority(mc.Priority),
		}
	}

	// Seed monitor metadata into alarm states so the API serves them before the first check.
	for _, mc := range e.cfg.Monitors {
		existing, _ := e.store.GetAlarm(mc.Name)
		existing.MonitorName = mc.Name
		existing.Icon = meta[mc.Name].icon
		existing.Group = mc.Group
		e.store.SetAlarm(existing)
	}

	// Phase 2: start all goroutines.
	var wg sync.WaitGroup
	for _, entry := range monitors {
		entry := entry
		wg.Add(1)
		go func() {
			defer wg.Done()
			monitor.Run(ctx, entry.m, entry.interval, results)
		}()
	}

	// Writer and evaluate loop also tracked by wg.
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.writer.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case r, ok := <-results:
				if !ok {
					return
				}
				thr, ok := thresholds[r.MonitorName]
				if !ok {
					log.Printf("engine: no threshold for monitor %q, skipping", r.MonitorName)
					continue
				}
				if ev := ApplyResult(e.store, thr, r); ev != nil {
					m := meta[r.MonitorName]
					ev.Icon = m.icon
					ev.Priority = m.priority
					log.Printf("alarm %q: %s", ev.MonitorName, ev.Transition)
					e.dispatcher.Dispatch(ctx, *ev)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	return nil
}
