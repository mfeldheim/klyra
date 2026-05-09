package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/state"
)

const defaultAPIURL = "https://api.pushover.net/1/messages.json"

func init() {
	action.Register("pushover", New)
}

type pushoverAction struct {
	name         string
	token        string
	user         string
	dashboardURL string
	priority     int
	apiURL       string
	client       *http.Client
}

// New constructs a pushover Action from config. Required keys: token, user.
// Optional keys: dashboard_url, priority (int or float64, default 1).
func New(name string, cfg map[string]any) (action.Action, error) {
	return NewWithEndpoint(name, cfg, defaultAPIURL)
}

// NewWithEndpoint is like New but accepts a custom API endpoint, primarily
// useful for redirecting requests to a test server.
func NewWithEndpoint(name string, cfg map[string]any, apiURL string) (action.Action, error) {
	token, err := requireString(cfg, "token", name)
	if err != nil {
		return nil, err
	}
	user, err := requireString(cfg, "user", name)
	if err != nil {
		return nil, err
	}

	dashboardURL, _ := cfg["dashboard_url"].(string)

	priority := 1
	if p, ok := cfg["priority"]; ok {
		switch v := p.(type) {
		case int:
			priority = v
		case float64:
			priority = int(v)
		}
	}

	return &pushoverAction{
		name:         name,
		token:        token,
		user:         user,
		dashboardURL: dashboardURL,
		priority:     priority,
		apiURL:       apiURL,
		client:       &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func requireString(cfg map[string]any, key, actionName string) (string, error) {
	v, ok := cfg[key]
	if !ok {
		return "", fmt.Errorf("pushover action %q: missing required field %q", actionName, key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("pushover action %q: field %q must be a non-empty string", actionName, key)
	}
	return s, nil
}

func (a *pushoverAction) Name() string {
	return a.name
}

func (a *pushoverAction) Fire(ctx context.Context, ev state.AlarmEvent) error {
	title, message := buildNotification(ev)

	priority := a.priority
	if ev.Transition == state.TransitionResolved {
		priority = 0
	}

	form := url.Values{}
	form.Set("token", a.token)
	form.Set("user", a.user)
	form.Set("title", title)
	form.Set("message", message)
	form.Set("priority", strconv.Itoa(priority))
	if a.dashboardURL != "" {
		form.Set("url", a.dashboardURL)
		form.Set("url_title", "Open Klyra Dashboard")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("pushover action %q: create request: %w", a.name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("pushover action %q: send request: %w", a.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pushover action %q: server returned status %d", a.name, resp.StatusCode)
	}

	return nil
}

func buildNotification(ev state.AlarmEvent) (title, message string) {
	if ev.Transition == state.TransitionFiring {
		title = "FIRING: " + ev.MonitorName
		message = fmt.Sprintf("Monitor %s is FIRING\nValue: %v\nSince: %s",
			ev.MonitorName, ev.Value, ev.FiredAt.Format(time.RFC3339))
	} else {
		title = "RESOLVED: " + ev.MonitorName
		duration := time.Since(ev.FiredAt).Round(time.Second)
		message = fmt.Sprintf("Monitor %s resolved\nValue: %v\nDuration: %s",
			ev.MonitorName, ev.Value, duration)
	}
	return title, message
}
