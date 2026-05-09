package httpaction

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mfeldheim/klyra/internal/action"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	action.Register("http", New)
}

type httpAction struct {
	name     string
	url      string
	method   string
	token    string
	priority string
	tags     []string
	client   *http.Client
}

func New(name string, cfg map[string]any) (action.Action, error) {
	urlVal, ok := cfg["url"]
	if !ok {
		return nil, fmt.Errorf("http action %q: missing required field 'url'", name)
	}
	urlStr, ok := urlVal.(string)
	if !ok || urlStr == "" {
		return nil, fmt.Errorf("http action %q: 'url' must be a non-empty string", name)
	}

	method := "POST"
	if m, ok := cfg["method"]; ok {
		if ms, ok := m.(string); ok && ms != "" {
			method = strings.ToUpper(ms)
		}
	}

	var token string
	if authRaw, ok := cfg["auth"]; ok {
		if authMap, ok := authRaw.(map[string]any); ok {
			if t, ok := authMap["token"]; ok {
				if ts, ok := t.(string); ok {
					token = ts
				}
			}
		}
	}

	var priority string
	var tags []string
	if ntfyRaw, ok := cfg["ntfy"]; ok {
		if ntfyMap, ok := ntfyRaw.(map[string]any); ok {
			if p, ok := ntfyMap["priority"]; ok {
				if ps, ok := p.(string); ok {
					priority = ps
				}
			}
			if t, ok := ntfyMap["tags"]; ok {
				if tagsSlice, ok := t.([]any); ok {
					for _, tag := range tagsSlice {
						if ts, ok := tag.(string); ok {
							tags = append(tags, ts)
						}
					}
				}
			}
		}
	}

	return &httpAction{
		name:     name,
		url:      urlStr,
		method:   method,
		token:    token,
		priority: priority,
		tags:     tags,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *httpAction) Name() string {
	return a.name
}

func (a *httpAction) Fire(ctx context.Context, ev state.AlarmEvent) error {
	payload := map[string]any{
		"monitor":  ev.MonitorName,
		"status":   string(ev.Transition),
		"message":  ev.Message,
		"value":    ev.Value,
		"fired_at": ev.FiredAt.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("http action %q: marshal payload: %w", a.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, a.method, a.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http action %q: create request: %w", a.name, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", ev.MonitorName)

	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if a.priority != "" {
		req.Header.Set("X-Priority", a.priority)
	}
	if len(a.tags) > 0 {
		req.Header.Set("X-Tags", strings.Join(a.tags, ","))
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http action %q: send request: %w", a.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http action %q: server returned status %d", a.name, resp.StatusCode)
	}

	return nil
}
