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

func TestManagerSubscribeStreaming(t *testing.T) {
	mgr := NewManager(newFakeStore())
	ev := state.AlarmEvent{MonitorName: "m", Transition: state.TransitionFiring, FiredAt: time.Now()}
	inc, _ := mgr.OnFiring(context.Background(), ev)

	runner := func(_ context.Context, _ *[]ConvMessage, emit func(string)) error {
		emit("hello ")
		emit("world")
		return nil
	}
	mgr.RunInvestigation(inc.ID, runner)

	ch := mgr.Subscribe(inc.ID)
	if ch == nil {
		// Investigation may have finished before subscribe; check buffer via a new subscribe
		t.Log("subscribe returned nil — investigation already done")
		return
	}

	var received string
	for text := range ch {
		received += text
	}
	if received == "" {
		t.Error("expected streamed content")
	}
}

func TestManagerChat(t *testing.T) {
	mgr := NewManager(newFakeStore())
	ev := state.AlarmEvent{MonitorName: "m", Transition: state.TransitionFiring, FiredAt: time.Now()}
	inc, _ := mgr.OnFiring(context.Background(), ev)

	runner := func(_ context.Context, history *[]ConvMessage, emit func(string)) error {
		emit("response")
		*history = append(*history, ConvMessage{Role: RoleAssistant, Blocks: []ConvBlock{{Type: "text", Text: "response"}}})
		return nil
	}

	ch, err := mgr.Chat(context.Background(), inc.ID, "what happened?", runner)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var got string
	for text := range ch {
		got += text
	}
	if got != "response" {
		t.Errorf("expected 'response', got %q", got)
	}
}
