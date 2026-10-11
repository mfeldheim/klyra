package incident

import (
	"context"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

// fakeStore is an in-memory Store for manager tests.
type fakeStore struct {
	incidents map[string]*Incident
	idx       Index
	content   map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		incidents: make(map[string]*Incident),
		content:   make(map[string]string),
	}
}

func (f *fakeStore) WriteIncident(_ context.Context, inc *Incident) error {
	cp := *inc
	f.incidents[inc.ID] = &cp
	return nil
}

func (f *fakeStore) ReadIncident(_ context.Context, id string) (*Incident, error) {
	inc, ok := f.incidents[id]
	if !ok {
		return nil, &fakeNotFound{key: id}
	}
	cp := *inc
	return &cp, nil
}

func (f *fakeStore) AppendContent(_ context.Context, id, content string) error {
	f.content[id] += content
	return nil
}

func (f *fakeStore) ReadContent(_ context.Context, id string) (string, error) {
	return f.content[id], nil
}

func (f *fakeStore) ListIncidents(_ context.Context) (Index, error) {
	return f.idx, nil
}

func (f *fakeStore) UpdateIndex(_ context.Context, summary IncidentSummary) error {
	for i, s := range f.idx.Incidents {
		if s.ID == summary.ID {
			f.idx.Incidents[i] = summary
			return nil
		}
	}
	f.idx.Incidents = append([]IncidentSummary{summary}, f.idx.Incidents...)
	return nil
}

func TestManagerOnFiring(t *testing.T) {
	mgr := NewManager(newFakeStore())
	ev := state.AlarmEvent{
		MonitorName: "api-latency",
		Transition:  state.TransitionFiring,
		Message:     "p99 high",
		Value:       "842ms",
		FiredAt:     time.Now(),
	}
	inc, err := mgr.OnFiring(context.Background(), ev)
	if err != nil {
		t.Fatalf("OnFiring: %v", err)
	}
	if inc.ID == "" {
		t.Error("expected non-empty ID")
	}
	if !mgr.IsActive(inc.ID) {
		t.Error("expected incident to be active")
	}
}

func TestManagerOnResolved(t *testing.T) {
	mgr := NewManager(newFakeStore())
	ev := state.AlarmEvent{
		MonitorName: "api-latency",
		Transition:  state.TransitionFiring,
		FiredAt:     time.Now(),
	}
	inc, _ := mgr.OnFiring(context.Background(), ev)

	resolveEv := state.AlarmEvent{
		MonitorName: "api-latency",
		Transition:  state.TransitionResolved,
		FiredAt:     inc.FiredAt,
	}
	if err := mgr.OnResolved(context.Background(), resolveEv); err != nil {
		t.Fatalf("OnResolved: %v", err)
	}
	if mgr.IsActive(inc.ID) {
		t.Error("expected incident to no longer be active after resolve")
	}
}

// TestManagerOnResolvedAfterFailover reproduces the orphaned-active-incident bug:
// the leader that created an incident is not necessarily the one that resolves
// it (leader election failover / pod restart). A fresh Manager shares only the
// persisted store, so the resolve must still close the open incident.
func TestManagerOnResolvedAfterFailover(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	firedAt := time.Date(2026, 10, 10, 21, 5, 51, 0, time.UTC)

	// Leader A creates the incident.
	mgrA := NewManager(store)
	inc, err := mgrA.OnFiring(ctx, state.AlarmEvent{
		MonitorName: "muenchen-branchenbuch",
		Transition:  state.TransitionFiring,
		FiredAt:     firedAt,
	})
	if err != nil {
		t.Fatalf("OnFiring: %v", err)
	}

	// Failover: leader B has an empty in-memory set but the same store.
	mgrB := NewManager(store)
	if err := mgrB.OnResolved(ctx, state.AlarmEvent{
		MonitorName: "muenchen-branchenbuch",
		Transition:  state.TransitionResolved,
		FiredAt:     firedAt,
	}); err != nil {
		t.Fatalf("OnResolved: %v", err)
	}

	got, err := store.ReadIncident(ctx, inc.ID)
	if err != nil {
		t.Fatalf("ReadIncident: %v", err)
	}
	if got.Status != StatusResolved {
		t.Fatalf("incident orphaned as %q after failover; want %q", got.Status, StatusResolved)
	}
	if mgrB.IsActive(inc.ID) {
		t.Error("expected incident to be closed in memory after resolve")
	}
}

// TestActiveIDForMonitorReturnsLatest ensures that when several incidents are
// open for one monitor, a resolve closes the most recent one rather than a
// random map entry.
func TestActiveIDForMonitorReturnsLatest(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newFakeStore())
	older := time.Date(2026, 10, 8, 2, 43, 16, 0, time.UTC)
	newer := older.Add(2 * time.Hour)

	incOld, err := mgr.OnFiring(ctx, state.AlarmEvent{MonitorName: "m", Transition: state.TransitionFiring, FiredAt: older})
	if err != nil {
		t.Fatalf("OnFiring older: %v", err)
	}
	incNew, err := mgr.OnFiring(ctx, state.AlarmEvent{MonitorName: "m", Transition: state.TransitionFiring, FiredAt: newer})
	if err != nil {
		t.Fatalf("OnFiring newer: %v", err)
	}

	if got := mgr.activeIDForMonitor("m"); got != incNew.ID {
		t.Fatalf("activeIDForMonitor = %q, want latest %q", got, incNew.ID)
	}

	if err := mgr.OnResolved(ctx, state.AlarmEvent{MonitorName: "m", Transition: state.TransitionResolved, FiredAt: newer}); err != nil {
		t.Fatalf("OnResolved: %v", err)
	}
	if mgr.IsActive(incNew.ID) {
		t.Error("expected newest incident to be resolved")
	}
	if !mgr.IsActive(incOld.ID) {
		t.Error("expected older incident to remain active")
	}
}

// TestManagerReconcile verifies startup reconciliation: on becoming leader, open
// incidents whose monitor is no longer firing are closed, incidents that match a
// still-firing alarm are rehydrated into memory, and incidents from older
// firing episodes are closed even when the monitor is currently firing again.
func TestManagerReconcile(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	firedAt := time.Date(2026, 10, 10, 21, 5, 51, 0, time.UTC)

	seeder := NewManager(store)
	currentInc, err := seeder.OnFiring(ctx, state.AlarmEvent{MonitorName: "api", Transition: state.TransitionFiring, FiredAt: firedAt})
	if err != nil {
		t.Fatalf("OnFiring current: %v", err)
	}
	oldEpisode, err := seeder.OnFiring(ctx, state.AlarmEvent{MonitorName: "api", Transition: state.TransitionFiring, FiredAt: firedAt.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatalf("OnFiring old episode: %v", err)
	}
	staleInc, err := seeder.OnFiring(ctx, state.AlarmEvent{MonitorName: "recovered", Transition: state.TransitionFiring, FiredAt: firedAt})
	if err != nil {
		t.Fatalf("OnFiring stale: %v", err)
	}

	// The new leader sees alarm state: "api" is firing since firedAt,
	// "recovered" is OK.
	alarms := map[string]state.AlarmState{
		"api":       {Status: state.AlarmFiring, FiredAt: &firedAt},
		"recovered": {Status: state.AlarmOK},
	}

	mgr := NewManager(store)
	if err := mgr.Reconcile(ctx, func(name string) (time.Time, bool) {
		a, ok := alarms[name]
		if !ok || a.Status != state.AlarmFiring || a.FiredAt == nil {
			return time.Time{}, false
		}
		return *a.FiredAt, true
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Recovered monitor: incident must be closed.
	gotStale, err := store.ReadIncident(ctx, staleInc.ID)
	if err != nil {
		t.Fatalf("ReadIncident stale: %v", err)
	}
	if gotStale.Status != StatusResolved {
		t.Errorf("recovered monitor incident status = %q, want %q", gotStale.Status, StatusResolved)
	}

	// Old episode of a still-firing monitor must be closed too.
	gotOld, err := store.ReadIncident(ctx, oldEpisode.ID)
	if err != nil {
		t.Fatalf("ReadIncident old episode: %v", err)
	}
	if gotOld.Status != StatusResolved {
		t.Errorf("older episode incident status = %q, want %q", gotOld.Status, StatusResolved)
	}

	// Current episode must remain active and be rehydrated into memory.
	gotCurrent, err := store.ReadIncident(ctx, currentInc.ID)
	if err != nil {
		t.Fatalf("ReadIncident current: %v", err)
	}
	if gotCurrent.Status != StatusActive {
		t.Errorf("current episode incident status = %q, want %q", gotCurrent.Status, StatusActive)
	}
	if !mgr.IsActive(currentInc.ID) {
		t.Error("expected current episode incident to be rehydrated into memory")
	}
}
