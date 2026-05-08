package httpmon_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
