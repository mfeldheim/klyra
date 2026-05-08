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

	dispatcher := NewDispatcher(st, actions, monitorActions)
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

// Run starts all monitors, the state writer, and the evaluate loop.
// It returns when ctx is cancelled (after all monitor goroutines exit).
func (e *Engine) Run(ctx context.Context) error {
	results := make(chan state.CheckResult, 100)

	// Build thresholds map.
	thresholds := make(map[string]config.ThresholdConfig, len(e.cfg.Monitors))
	for _, mc := range e.cfg.Monitors {
		thresholds[mc.Name] = mc.Threshold
	}

	var wg sync.WaitGroup

	// Start a goroutine per monitor.
	for _, mc := range e.cfg.Monitors {
		mc := mc // capture loop variable

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

		wg.Add(1)
		go func() {
			defer wg.Done()
			monitor.Run(ctx, m, interval, results)
		}()
	}

	// Start state writer.
	go e.writer.Run(ctx)

	// Evaluate loop.
	go func() {
		for {
			select {
			case r, ok := <-results:
				if !ok {
					return
				}
				thr, hasThr := thresholds[r.MonitorName]
				if !hasThr {
					log.Printf("engine: no threshold for monitor %q, skipping", r.MonitorName)
					continue
				}
				ev := ApplyResult(e.store, thr, r)
				if ev != nil {
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
