package prommon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	prommon "github.com/mfeldheim/klyra/internal/monitor/prometheus"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestPrometheusMonitorScalar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "scalar",
				"result":     []any{float64(1234567890), "0.042"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m, err := prommon.New("test", map[string]any{
		"url":    srv.URL,
		"query":  `rate(http_errors_total[5m])`,
		"result": "scalar",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected OK, got %s", r.Status)
	}
	val, ok := r.Value.(float64)
	if !ok || val < 0.04 || val > 0.05 {
		t.Errorf("unexpected value: %v", r.Value)
	}
}

func TestPrometheusMonitorFirstValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{
					map[string]any{
						"metric": map[string]any{"job": "api"},
						"value":  []any{float64(1234567890), "99.5"},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m, err := prommon.New("test", map[string]any{
		"url":    srv.URL,
		"query":  `up`,
		"result": "first_value",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := m.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != state.CheckOK {
		t.Errorf("expected CheckOK, got %s: %s", r.Status, r.Message)
	}
	val, ok := r.Value.(float64)
	if !ok || val < 99.4 || val > 99.6 {
		t.Errorf("expected ~99.5, got %v", r.Value)
	}
}
