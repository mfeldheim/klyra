// internal/server/handlers_test.go
package server_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/server"
	"github.com/mfeldheim/klyra/internal/state"
)

func makeStore() *state.Store {
	st := state.NewStore()
	st.SetAlarm(state.AlarmState{MonitorName: "test", Status: state.AlarmOK, LastCheck: time.Now()})
	return st
}

func makeCfg() *config.Config {
	return &config.Config{
		Monitors: []config.MonitorConfig{{Name: "test", Type: "http"}},
	}
}

func TestStatusHandler(t *testing.T) {
	h := server.NewHandlers(makeStore(), makeCfg())
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["alarms"]; !ok {
		t.Error("expected alarms key in response")
	}
}

func TestHistoryHandler(t *testing.T) {
	st := makeStore()
	st.AppendHistory(state.HistoryEvent{MonitorName: "test", Transition: state.TransitionFiring, At: time.Now()})
	h := server.NewHandlers(st, makeCfg())
	req := httptest.NewRequest("GET", "/api/history", nil)
	w := httptest.NewRecorder()
	h.History(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []any
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestCreateSilenceHandler(t *testing.T) {
	h := server.NewHandlers(makeStore(), makeCfg())
	body := `{"monitor":"test","duration":"1h","reason":"maintenance"}`
	req := httptest.NewRequest("POST", "/api/silences", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.CreateSilence(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
