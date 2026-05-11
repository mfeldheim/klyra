package httpmon_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpmon "github.com/mfeldheim/klyra/internal/monitor/http"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestHTTPMonitorOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"method":        "GET",
		"expect_status": float64(200),
		"expect_body":   "ok",
		"timeout":       "5s",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v", r.Value)
	}
}

func TestHTTPMonitorFailsOnWrongStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"expect_status": float64(200),
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected status CheckOK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != false {
		t.Errorf("expected value false, got %v", r.Value)
	}
}

func TestHTTPMonitorFailsOnMissingBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("healthy"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url":           srv.URL,
		"expect_status": float64(200),
		"expect_body":   "ok",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, _ := m.Check(context.Background())
	if r.Status != state.CheckOK {
		t.Errorf("expected status CheckOK, got %s: %s", r.Status, r.Message)
	}
	if r.Value != false {
		t.Errorf("expected false when body mismatch, got %v", r.Value)
	}
}

func TestHTTPMonitorStatusExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url": srv.URL,
		"status": map[string]any{
			"operator": "gte",
			"value":    float64(200),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected status CheckOK, got %s", r.Status)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v", r.Value)
	}
}

func TestHTTPMonitorJSONExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok","disk":{"used_pct":91.2}}`))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url": srv.URL,
		"json": map[string]any{
			"path":     "disk.used_pct",
			"operator": "gt",
			"value":    float64(90),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v", r.Value)
	}
}

func TestHTTPMonitorJSONBooleanExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"system_disk_total_bytes":1000,"system_disk_used_bytes":920}`))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url": srv.URL,
		"json": map[string]any{
			"expression": "(system_disk_used_bytes / system_disk_total_bytes) * 100 > 90",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v; message=%s", r.Value, r.Message)
	}
}

func TestHTTPMonitorBodyAndHeaderExpressions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Env", "prod-eu")
		w.WriteHeader(200)
		w.Write([]byte("service is healthy and green"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url": srv.URL,
		"body": map[string]any{
			"operator": "contains",
			"value":    "healthy",
		},
		"header": map[string]any{
			"name":     "X-Env",
			"operator": "matches",
			"value":    "^prod-",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v; message=%s", r.Value, r.Message)
	}
}

func TestHTTPMonitorLatencyExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(25 * time.Millisecond)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	m, err := httpmon.New("test", map[string]any{
		"url": srv.URL,
		"latency_ms": map[string]any{
			"operator": "gt",
			"value":    float64(10),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Value != true {
		t.Errorf("expected value true, got %v; message=%s", r.Value, r.Message)
	}
	if !strings.Contains(r.Message, "latency_ms") {
		t.Errorf("expected latency in message, got %q", r.Message)
	}
}
