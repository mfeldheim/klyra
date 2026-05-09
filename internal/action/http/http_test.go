package httpaction_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpaction "github.com/mfeldheim/klyra/internal/action/http"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestHTTPActionFiresWithNtfyHeaders(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	a, err := httpaction.New("notify", map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"auth": map[string]any{
			"type":  "bearer",
			"token": "mytoken",
		},
		"ntfy": map[string]any{
			"priority": "high",
			"tags":     []any{"warning", "k8s"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	ev := state.AlarmEvent{
		MonitorName: "test-monitor",
		Transition:  state.TransitionFiring,
		Message:     "replica count low",
		Value:       float64(1),
		FiredAt:     now,
	}
	if err := a.Fire(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if gotReq.Header.Get("Authorization") != "Bearer mytoken" {
		t.Errorf("expected Bearer auth, got %s", gotReq.Header.Get("Authorization"))
	}
	if gotReq.Header.Get("X-Priority") != "high" {
		t.Errorf("expected X-Priority high, got %s", gotReq.Header.Get("X-Priority"))
	}
	if gotReq.Header.Get("X-Tags") != "warning,k8s" {
		t.Errorf("expected X-Tags warning,k8s, got %s", gotReq.Header.Get("X-Tags"))
	}
	if gotReq.Header.Get("X-Title") != "test-monitor" {
		t.Errorf("expected X-Title test-monitor, got %s", gotReq.Header.Get("X-Title"))
	}

	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if payload["status"] != "FIRING" {
		t.Errorf("expected status FIRING, got %v", payload["status"])
	}
	if gotReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", gotReq.Header.Get("Content-Type"))
	}
	firedAtStr, _ := payload["fired_at"].(string)
	if _, err := time.Parse(time.RFC3339, firedAtStr); err != nil {
		t.Errorf("expected fired_at in RFC3339 format, got %q: %v", firedAtStr, err)
	}
}

func TestHTTPActionErrorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	a, err := httpaction.New("notify", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	err = a.Fire(context.Background(), state.AlarmEvent{
		MonitorName: "test",
		Transition:  state.TransitionFiring,
		FiredAt:     time.Now(),
	})
	if err == nil {
		t.Error("expected error on 500 response")
	}
}
