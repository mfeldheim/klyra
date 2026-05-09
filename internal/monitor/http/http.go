package httpmon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("http", New)
}

type httpMonitor struct {
	name         string
	url          string
	method       string
	client       *http.Client
	expectStatus int
	expectBody   string
	headers      map[string]string
}

// New creates a new HTTP monitor from a name and config map.
func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	// Required: url
	urlVal, ok := cfg["url"]
	if !ok {
		return nil, fmt.Errorf("http monitor %q: missing required field 'url'", name)
	}
	url, ok := urlVal.(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("http monitor %q: 'url' must be a non-empty string", name)
	}

	// Optional: method (default "GET")
	method := "GET"
	if v, ok := cfg["method"]; ok {
		if s, ok := v.(string); ok && s != "" {
			method = s
		}
	}
	method = strings.ToUpper(method)

	// Optional: timeout (default "10s")
	timeout := 10 * time.Second
	if v, ok := cfg["timeout"]; ok {
		if s, ok := v.(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("http monitor %q: invalid 'timeout': %w", name, err)
			}
			timeout = d
		}
	}

	// Optional: expect_status (default 200)
	expectStatus := 200
	if v, ok := cfg["expect_status"]; ok {
		switch n := v.(type) {
		case float64:
			expectStatus = int(n)
		case int:
			expectStatus = n
		default:
			return nil, fmt.Errorf("http monitor %q: 'expect_status' must be a number", name)
		}
	}

	// Optional: expect_body
	var expectBody string
	if v, ok := cfg["expect_body"]; ok {
		if s, ok := v.(string); ok {
			expectBody = s
		}
	}

	// Optional: headers
	headers := map[string]string{}
	if v, ok := cfg["headers"]; ok {
		if m, ok := v.(map[string]any); ok {
			for k, val := range m {
				if s, ok := val.(string); ok {
					headers[k] = s
				}
			}
		}
	}

	client := &http.Client{Timeout: timeout}

	return &httpMonitor{
		name:         name,
		url:          url,
		method:       method,
		client:       client,
		expectStatus: expectStatus,
		expectBody:   expectBody,
		headers:      headers,
	}, nil
}

func (m *httpMonitor) Name() string { return m.name }

func (m *httpMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()

	req, err := http.NewRequestWithContext(ctx, m.method, m.url, nil)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	for k, v := range m.headers {
		req.Header.Set(k, v)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckOK,
			Value:       false,
			Message:     fmt.Sprintf("request failed: %s", err.Error()),
			Timestamp:   now,
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckOK,
			Value:       false,
			Message:     fmt.Sprintf("failed to read response body: %s", err.Error()),
			Timestamp:   now,
		}, nil
	}

	statusOK := resp.StatusCode == m.expectStatus
	bodyOK := m.expectBody == "" || strings.Contains(string(body), m.expectBody)
	ok := statusOK && bodyOK

	var msg string
	if ok {
		msg = fmt.Sprintf("HTTP %d OK", resp.StatusCode)
	} else if !statusOK {
		msg = fmt.Sprintf("unexpected status: got %d, expected %d", resp.StatusCode, m.expectStatus)
	} else {
		msg = fmt.Sprintf("body does not contain expected string %q", m.expectBody)
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       ok,
		Message:     msg,
		Timestamp:   now,
	}, nil
}
