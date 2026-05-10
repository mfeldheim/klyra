package incident

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

// InvRunner is the function signature for running an investigation or chat turn.
// It receives a pointer to the conversation history (to append to) and an emit
// function to stream text deltas. The history pointer is safe to use only while
// the runner is executing.
type InvRunner func(ctx context.Context, history *[]ConvMessage, emit func(string)) error

type activeIncident struct {
	incident *Incident
	history  []ConvMessage
	subs     []chan string
	buf      strings.Builder
	done     bool
	mu       sync.Mutex
}

func (a *activeIncident) emit(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buf.WriteString(text)
	for _, ch := range a.subs {
		select {
		case ch <- text:
		default:
		}
	}
}

func (a *activeIncident) subscribe() chan string {
	ch := make(chan string, 128)
	a.mu.Lock()
	defer a.mu.Unlock()
	// Send buffered content as catchup
	if a.buf.Len() > 0 {
		ch <- a.buf.String()
	}
	if a.done {
		close(ch)
		return ch
	}
	a.subs = append(a.subs, ch)
	return ch
}

func (a *activeIncident) closeSubs() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.done = true
	for _, ch := range a.subs {
		close(ch)
	}
	a.subs = nil
}

func (a *activeIncident) unsubscribe(ch chan string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, s := range a.subs {
		if s == ch {
			a.subs = append(a.subs[:i], a.subs[i+1:]...)
			return
		}
	}
}

// Manager manages incident lifecycle and in-memory state.
type Manager struct {
	store  Store
	mu     sync.RWMutex
	active map[string]*activeIncident // keyed by incident ID
}

// NewManager creates a Manager backed by the given Store.
func NewManager(store Store) *Manager {
	return &Manager{
		store:  store,
		active: make(map[string]*activeIncident),
	}
}

// OnFiring creates a new incident for a FIRING alarm event.
func (m *Manager) OnFiring(ctx context.Context, ev state.AlarmEvent) (*Incident, error) {
	inc := &Incident{
		ID:                  NewID(ev.MonitorName, ev.FiredAt),
		MonitorName:         ev.MonitorName,
		FiredAt:             ev.FiredAt,
		Status:              StatusActive,
		InvestigationStatus: InvPending,
		Value:               ev.Value,
		Message:             ev.Message,
		Icon:                ev.Icon,
	}

	if err := m.store.WriteIncident(ctx, inc); err != nil {
		return nil, fmt.Errorf("manager.OnFiring: write incident: %w", err)
	}
	if err := m.store.UpdateIndex(ctx, inc.Summary()); err != nil {
		return nil, fmt.Errorf("manager.OnFiring: update index: %w", err)
	}

	ai := &activeIncident{incident: inc}
	m.mu.Lock()
	m.active[inc.ID] = ai
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
	ai, ok := m.active[incID]
	delete(m.active, incID)
	m.mu.Unlock()

	if !ok {
		return nil
	}

	ai.closeSubs()

	// Update metadata in S3
	inc := ai.incident
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

// RunInvestigation sets the investigation status to running and executes runner
// in a goroutine, streaming output through the fan-out and S3.
func (m *Manager) RunInvestigation(incidentID string, runner InvRunner) {
	m.mu.RLock()
	ai, ok := m.active[incidentID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	m.updateStatus(incidentID, InvRunning)

	go func() {
		ctx := context.Background()
		var chunkBuf strings.Builder

		emit := func(text string) {
			chunkBuf.WriteString(text)
			ai.emit(text)
		}

		err := runner(ctx, &ai.history, emit)

		// Persist all streamed content to S3
		if chunkBuf.Len() > 0 {
			if appendErr := m.store.AppendContent(ctx, incidentID, chunkBuf.String()); appendErr != nil {
				log.Printf("manager: append investigation content %s: %v", incidentID, appendErr)
			}
		}

		status := InvComplete
		if err != nil {
			status = InvFailed
			log.Printf("manager: investigation %s failed: %v", incidentID, err)
			ai.emit("\n\n> **Investigation failed:** " + err.Error() + "\n")
		}
		m.updateStatus(incidentID, status)
		ai.closeSubs()
	}()
}

// Chat appends a user message to the conversation and streams the response.
// Returns a channel that receives text deltas and is closed when done.
// Returns nil if the incident is not active.
func (m *Manager) Chat(ctx context.Context, incidentID, userMsg string, runner InvRunner) (<-chan string, error) {
	m.mu.RLock()
	ai, ok := m.active[incidentID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("incident %s not found or not active", incidentID)
	}

	ch := make(chan string, 128)

	go func() {
		defer close(ch)

		var responseBuf strings.Builder
		emit := func(text string) {
			responseBuf.WriteString(text)
			select {
			case ch <- text:
			default:
			}
		}

		// Prepend user message to history (runner for chat expects it already there)
		ai.mu.Lock()
		ai.history = append(ai.history, ConvMessage{
			Role:   RoleUser,
			Blocks: []ConvBlock{{Type: "text", Text: userMsg}},
		})
		ai.mu.Unlock()

		if err := runner(ctx, &ai.history, emit); err != nil {
			log.Printf("manager.Chat %s: %v", incidentID, err)
			return
		}

		// Persist chat exchange to S3
		ts := time.Now().UTC().Format(time.RFC3339)
		content := fmt.Sprintf("\n---\n\n### User — %s\n\n%s\n\n### Assistant — %s\n\n%s\n",
			ts, userMsg, ts, responseBuf.String())
		if err := m.store.AppendContent(context.Background(), incidentID, content); err != nil {
			log.Printf("manager.Chat: append to S3 %s: %v", incidentID, err)
		}
	}()

	return ch, nil
}

// Subscribe returns a channel of text deltas for the investigation stream.
// The channel is pre-filled with buffered content. It is closed when the
// investigation completes or the incident resolves. Returns nil if not found.
func (m *Manager) Subscribe(incidentID string) chan string {
	m.mu.RLock()
	ai, ok := m.active[incidentID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return ai.subscribe()
}

// Unsubscribe removes a subscriber channel from the fan-out.
func (m *Manager) Unsubscribe(incidentID string, ch chan string) {
	m.mu.RLock()
	ai, ok := m.active[incidentID]
	m.mu.RUnlock()
	if ok {
		ai.unsubscribe(ch)
	}
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
	for id, ai := range m.active {
		if ai.incident.MonitorName == monitorName {
			return id
		}
	}
	return ""
}

func (m *Manager) updateStatus(incidentID string, status InvestigationStatus) {
	m.mu.RLock()
	ai, ok := m.active[incidentID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	ai.mu.Lock()
	ai.incident.InvestigationStatus = status
	ai.mu.Unlock()

	ctx := context.Background()
	if err := m.store.UpdateIndex(ctx, ai.incident.Summary()); err != nil {
		log.Printf("manager: update index status %s: %v", incidentID, err)
	}
}
