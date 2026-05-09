package pushover_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/action/pushover"
	"github.com/mfeldheim/klyra/internal/state"
)

type testCase struct {
	name         string
	cfg          map[string]any
	event        state.AlarmEvent
	wantTitle    string
	wantInMsg    []string
	wantPriority string
	wantStatus   int
	wantErr      bool
}

func TestPushoverAction(t *testing.T) {
	firedAt := time.Now().Add(-5*time.Minute - 32*time.Second)

	tests := []testCase{
		{
			name: "FIRING notification contains monitor name, FIRING, and value",
			cfg: map[string]any{
				"token":         "app-token",
				"user":          "user-key",
				"dashboard_url": "https://klyra.example.com",
				"priority":      float64(1),
			},
			event: state.AlarmEvent{
				MonitorName: "stadtbranchenbuch-http",
				Transition:  state.TransitionFiring,
				Value:       false,
				FiredAt:     time.Now(),
			},
			wantTitle:    "FIRING: stadtbranchenbuch-http",
			wantInMsg:    []string{"stadtbranchenbuch-http", "FIRING", "false"},
			wantPriority: "1",
			wantStatus:   http.StatusOK,
		},
		{
			name: "RESOLVED notification uses priority 0 regardless of configured priority",
			cfg: map[string]any{
				"token":    "app-token",
				"user":     "user-key",
				"priority": float64(1),
			},
			event: state.AlarmEvent{
				MonitorName: "stadtbranchenbuch-http",
				Transition:  state.TransitionResolved,
				Value:       true,
				FiredAt:     firedAt,
			},
			wantTitle:    "RESOLVED: stadtbranchenbuch-http",
			wantInMsg:    []string{"resolved", "Duration:"},
			wantPriority: "0",
			wantStatus:   http.StatusOK,
		},
		{
			name: "server error returns descriptive error",
			cfg: map[string]any{
				"token": "app-token",
				"user":  "user-key",
			},
			event: state.AlarmEvent{
				MonitorName: "test-monitor",
				Transition:  state.TransitionFiring,
				FiredAt:     time.Now(),
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name: "missing token returns error",
			cfg: map[string]any{
				"user": "user-key",
			},
			wantErr: true,
		},
		{
			name: "missing user returns error",
			cfg: map[string]any{
				"token": "app-token",
			},
			wantErr: true,
		},
		{
			name: "dashboard_url sets url and url_title fields",
			cfg: map[string]any{
				"token":         "app-token",
				"user":          "user-key",
				"dashboard_url": "https://klyra.example.com",
			},
			event: state.AlarmEvent{
				MonitorName: "mymonitor",
				Transition:  state.TransitionFiring,
				Value:       1,
				FiredAt:     time.Now(),
			},
			wantStatus: http.StatusOK,
			wantInMsg:  []string{"mymonitor", "FIRING"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedForm url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Errorf("parse form: %v", err)
				}
				capturedForm = r.PostForm
				w.WriteHeader(tc.wantStatus)
			}))
			defer srv.Close()

			a, err := pushover.NewWithEndpoint("test", tc.cfg, srv.URL)
			if tc.wantErr && err != nil {
				// config-level error expected
				return
			}
			if err != nil {
				t.Fatalf("unexpected NewWithEndpoint error: %v", err)
			}

			fireErr := a.Fire(context.Background(), tc.event)
			if tc.wantErr {
				if fireErr == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if fireErr != nil {
				t.Fatalf("unexpected Fire error: %v", fireErr)
			}

			if tc.wantTitle != "" {
				if got := capturedForm.Get("title"); got != tc.wantTitle {
					t.Errorf("title: got %q, want %q", got, tc.wantTitle)
				}
			}

			msg := capturedForm.Get("message")
			for _, want := range tc.wantInMsg {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q missing %q", msg, want)
				}
			}

			if tc.wantPriority != "" {
				if got := capturedForm.Get("priority"); got != tc.wantPriority {
					t.Errorf("priority: got %q, want %q", got, tc.wantPriority)
				}
			}

			// Verify dashboard_url wires up url and url_title
			if dashURL, ok := tc.cfg["dashboard_url"].(string); ok && dashURL != "" {
				if got := capturedForm.Get("url"); got != dashURL {
					t.Errorf("url: got %q, want %q", got, dashURL)
				}
				if got := capturedForm.Get("url_title"); got != "Open Klyra Dashboard" {
					t.Errorf("url_title: got %q, want %q", got, "Open Klyra Dashboard")
				}
			}
		})
	}
}
