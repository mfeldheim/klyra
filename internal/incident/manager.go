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

// OnResolved closes the incident for the monitor, writes final status to S3,
// and clears memory. The incident is resolved from the persisted store when it
// is not in memory, so a resolve handled by a different process than the one
// that created the incident (leader-election failover, pod restart) still
// closes it instead of orphaning it as "active".
func (m *Manager) OnResolved(ctx context.Context, ev state.AlarmEvent) error {
	inc := m.takeActive(ctx, ev.MonitorName)
	if inc == nil {
		return nil
	}
	return m.close(ctx, inc, time.Now().UTC())
}

// takeActive removes and returns the open incident for a monitor. It prefers the
// in-memory set and falls back to the persisted index (newest active) so that
// resolution is not tied to the lifetime of the process that fired the alarm.
func (m *Manager) takeActive(ctx context.Context, monitorName string) *Incident {
	m.mu.Lock()
	id := m.latestActiveIDLocked(monitorName)
	var inc *Incident
	if id != "" {
		inc = m.active[id]
		delete(m.active, id)
	}
	m.mu.Unlock()
	if inc != nil {
		return inc
	}
	return m.activeFromStore(ctx, monitorName)
}

// close marks an incident resolved and persists the final status, log footer,
// and index entry, then clears it from the in-memory active set.
func (m *Manager) close(ctx context.Context, inc *Incident, resolvedAt time.Time) error {
	m.persistResolution(ctx, inc, resolvedAt)
	m.mu.Lock()
	delete(m.active, inc.ID)
	m.mu.Unlock()
	return nil
}

// persistResolution writes the resolved status, log footer, and index entry for
// an incident without touching the in-memory active set.
func (m *Manager) persistResolution(ctx context.Context, inc *Incident, resolvedAt time.Time) {
	inc.Status = StatusResolved
	inc.ResolvedAt = &resolvedAt

	if err := m.store.WriteIncident(ctx, inc); err != nil {
		log.Printf("manager: rewrite incident %s: %v", inc.ID, err)
	}

	footer := fmt.Sprintf("\n---\n\n**Status:** resolved  \n**Resolved At:** %s  \n**Duration:** %s  \n",
		resolvedAt.Format(time.RFC3339),
		resolvedAt.Sub(inc.FiredAt).Round(time.Second))
	if err := m.store.AppendContent(ctx, inc.ID, footer); err != nil {
		log.Printf("manager: append footer %s: %v", inc.ID, err)
	}

	if err := m.store.UpdateIndex(ctx, inc.Summary()); err != nil {
		log.Printf("manager: update index %s: %v", inc.ID, err)
	}
}

// IsActive reports whether an incident ID has an active in-memory entry.
func (m *Manager) IsActive(incidentID string) bool {
	m.mu.RLock()
	_, ok := m.active[incidentID]
	m.mu.RUnlock()
	return ok
}

// activeIDForMonitor returns the newest active in-memory incident for a monitor.
func (m *Manager) activeIDForMonitor(monitorName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latestActiveIDLocked(monitorName)
}

// latestActiveIDLocked returns the ID of the most recently fired active incident
// for a monitor. Callers must hold m.mu.
func (m *Manager) latestActiveIDLocked(monitorName string) string {
	id := ""
	var latest time.Time
	for _, inc := range m.active {
		if inc.MonitorName != monitorName {
			continue
		}
		if id == "" || inc.FiredAt.After(latest) {
			id = inc.ID
			latest = inc.FiredAt
		}
	}
	return id
}

// activeFromStore loads the newest active incident for a monitor from the
// persisted index. Returns nil when there is none.
func (m *Manager) activeFromStore(ctx context.Context, monitorName string) *Incident {
	idx, err := m.store.ListIncidents(ctx)
	if err != nil {
		log.Printf("manager: list incidents: %v", err)
		return nil
	}
	latestID := ""
	var latestFired time.Time
	for _, s := range idx.Incidents {
		if s.Status != StatusActive || s.MonitorName != monitorName {
			continue
		}
		if latestID == "" || s.FiredAt.After(latestFired) {
			latestID = s.ID
			latestFired = s.FiredAt
		}
	}
	if latestID == "" {
		return nil
	}
	inc, err := m.store.ReadIncident(ctx, latestID)
	if err != nil {
		log.Printf("manager: read incident %s: %v", latestID, err)
		return nil
	}
	return inc
}

// Reconcile synchronizes the in-memory active set with the persisted store and
// alarm state. It runs when a process becomes leader (restart / election
// failover) and self-heals incidents orphaned by a restart:
//
//   - incidents for monitors that are no longer firing are closed;
//   - incidents matching the current fire time of a still-firing monitor are
//     rehydrated into memory so a subsequent RESOLVED closes them;
//   - incidents from older firing episodes of a still-firing monitor (the
//     current one is derived from the alarm's FiredAt) are closed.
//
// firingSince reports the time a monitor started firing, and whether it is
// currently firing at all.
func (m *Manager) Reconcile(ctx context.Context, firingSince func(monitorName string) (time.Time, bool)) error {
	idx, err := m.store.ListIncidents(ctx)
	if err != nil {
		return fmt.Errorf("manager.Reconcile: list incidents: %w", err)
	}
	now := time.Now().UTC()
	for _, s := range idx.Incidents {
		if s.Status != StatusActive {
			continue
		}
		inc, err := m.store.ReadIncident(ctx, s.ID)
		if err != nil {
			log.Printf("manager.Reconcile: read incident %s: %v", s.ID, err)
			continue
		}
		firedAt, firing := firingSince(s.MonitorName)
		if firing && s.FiredAt.Equal(firedAt) {
			// Current episode: keep it open and rehydrate into memory.
			m.mu.Lock()
			m.active[s.ID] = inc
			m.mu.Unlock()
			continue
		}
		// Orphaned episode: monitor recovered, or the incident predates the
		// current firing episode. Close it.
		m.persistResolution(ctx, inc, now)
	}
	return nil
}
