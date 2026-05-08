package state

import (
	"encoding/json"
	"sync"
	"time"
)

// Store is a thread-safe in-memory state store.
type Store struct {
	mu      sync.RWMutex
	alarms  map[string]AlarmState
	history []HistoryEvent
	silences []Silence
	dirty   bool
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		alarms:   make(map[string]AlarmState),
		history:  []HistoryEvent{},
		silences: []Silence{},
	}
}

// GetAlarm returns the AlarmState for the named monitor, and whether it exists.
func (s *Store) GetAlarm(name string) (AlarmState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.alarms[name]
	return a, ok
}

// SetAlarm stores an AlarmState and marks the store dirty.
func (s *Store) SetAlarm(a AlarmState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alarms[a.MonitorName] = a
	s.dirty = true
}

// Alarms returns a shallow copy of all alarm states.
func (s *Store) Alarms() map[string]AlarmState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]AlarmState, len(s.alarms))
	for k, v := range s.alarms {
		out[k] = v
	}
	return out
}

// AppendHistory appends a HistoryEvent and marks the store dirty.
func (s *Store) AppendHistory(e HistoryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, e)
	s.dirty = true
}

// History returns a copy of all history events.
func (s *Store) History() []HistoryEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]HistoryEvent, len(s.history))
	copy(out, s.history)
	return out
}

// AddSilence appends a Silence and marks the store dirty.
func (s *Store) AddSilence(sl Silence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.silences = append(s.silences, sl)
	s.dirty = true
}

// RemoveSilence removes a Silence by ID. Returns false if not found.
// Sets dirty=true if found and removed.
func (s *Store) RemoveSilence(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sl := range s.silences {
		if sl.ID == id {
			s.silences = append(s.silences[:i], s.silences[i+1:]...)
			s.dirty = true
			return true
		}
	}
	return false
}

// Silences returns a copy of all silences.
func (s *Store) Silences() []Silence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Silence, len(s.silences))
	copy(out, s.silences)
	return out
}

// IsSilenced returns true if any active silence matches monitorName or is a
// wildcard (empty MonitorName).
func (s *Store) IsSilenced(monitorName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	for _, sl := range s.silences {
		if !sl.IsActive(now) {
			continue
		}
		if sl.MonitorName == "" || sl.MonitorName == monitorName {
			return true
		}
	}
	return false
}

// IsDirty reports whether the store has unsaved changes.
func (s *Store) IsDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}

// ClearDirty marks the store as clean.
func (s *Store) ClearDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = false
}

// Snapshot returns a PersistedState trimmed to window duration.
func (s *Store) Snapshot(window time.Duration) PersistedState {
	s.mu.RLock()
	alarms := make(map[string]AlarmState, len(s.alarms))
	for k, v := range s.alarms {
		alarms[k] = v
	}
	history := make([]HistoryEvent, len(s.history))
	copy(history, s.history)
	silences := make([]Silence, len(s.silences))
	copy(silences, s.silences)
	s.mu.RUnlock()

	ps := PersistedState{
		UpdatedAt: time.Now(),
		Alarms:    alarms,
		History:   history,
		Silences:  silences,
	}
	ps.Trim(window, time.Now())
	return ps
}

// LoadSnapshot replaces the store's state with the given PersistedState and
// marks the store as clean.
func (s *Store) LoadSnapshot(ps PersistedState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.alarms = make(map[string]AlarmState, len(ps.Alarms))
	for k, v := range ps.Alarms {
		s.alarms[k] = v
	}
	s.history = make([]HistoryEvent, len(ps.History))
	copy(s.history, ps.History)
	s.silences = make([]Silence, len(ps.Silences))
	copy(s.silences, ps.Silences)
	s.dirty = false
}

// MarshalJSON serialises a 24-hour snapshot of the store.
func (s *Store) MarshalJSON() ([]byte, error) {
	ps := s.Snapshot(24 * time.Hour)
	return json.Marshal(ps)
}
