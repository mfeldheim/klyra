package incident

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

// Manager manages incident lifecycle and in-memory state.
type Manager struct {
	store  Store
	mu     sync.RWMutex
	active map[string]*Incident // keyed by incident ID
}

// NewManager creates a Manager backed by the given Store.
func NewManager(store Store) *Manager {
	return &Manager{
		store:  store,
		active: make(map[string]*Incident),
	}
}

// OnFiring creates a new incident for a FIRING alarm event.
func (m *Manager) OnFiring(ctx context.Context, ev state.AlarmEvent) (*Incident, error) {
	inc := &Incident{
		ID:          NewID(ev.MonitorName, ev.FiredAt),
		MonitorName: ev.MonitorName,
		FiredAt:     ev.FiredAt,
		Status:      StatusActive,
		Value:       ev.Value,
		Message:     ev.Message,
		Icon:        ev.Icon,
	}

	if err := m.store.WriteIncident(ctx, inc); err != nil {
		return nil, fmt.Errorf("manager.OnFiring: write incident: %w", err)
	}
	if err := m.store.UpdateIndex(ctx, inc.Summary()); err != nil {
		return nil, fmt.Errorf("manager.OnFiring: update index: %w", err)
	}

	m.mu.Lock()
	m.active[inc.ID] = inc
	m.mu.Unlock()

	return inc, nil
}

// OnResolved closes the incident, writes final status to S3, and clears memory.
func (m *Manager) OnResolved(ctx context.Context, ev state.AlarmEvent) error {
	// Find the active incident for this monitor (latest active one).
	incID := m.activeIDForMonitor(ev.MonitorName)
	if incID == "" {
		return nil
	}

	m.mu.Lock()
	inc, ok := m.active[incID]
	delete(m.active, incID)
	m.mu.Unlock()

	if !ok {
		return nil
	}

	// Update metadata in S3
	now := ev.FiredAt // FiredAt on a RESOLVED event is the original fire time
	resolvedAt := time.Now().UTC()
	inc.Status = StatusResolved
	inc.ResolvedAt = &resolvedAt

	if err := m.store.WriteIncident(ctx, inc); err != nil {
		log.Printf("manager.OnResolved: rewrite incident %s: %v", incID, err)
	}

	footer := fmt.Sprintf("\n---\n\n**Status:** resolved  \n**Resolved At:** %s  \n**Duration:** %s  \n",
		resolvedAt.Format(time.RFC3339),
		resolvedAt.Sub(now).Round(time.Second))
	if err := m.store.AppendContent(ctx, incID, footer); err != nil {
		log.Printf("manager.OnResolved: append footer %s: %v", incID, err)
	}

	if err := m.store.UpdateIndex(ctx, inc.Summary()); err != nil {
		log.Printf("manager.OnResolved: update index %s: %v", incID, err)
	}

	return nil
}

// IsActive reports whether an incident ID has an active in-memory entry.
func (m *Manager) IsActive(incidentID string) bool {
	m.mu.RLock()
	_, ok := m.active[incidentID]
	m.mu.RUnlock()
	return ok
}

func (m *Manager) activeIDForMonitor(monitorName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, inc := range m.active {
		if inc.MonitorName == monitorName {
			return id
		}
	}
	return ""
}
