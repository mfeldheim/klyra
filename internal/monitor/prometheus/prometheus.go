package prommon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("prometheus", New)
}

type prometheusMonitor struct {
	name       string
	url        string
	query      string
	resultType string
	client     *http.Client
}

// New creates a new Prometheus monitor from a name and config map.
func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	// Required: url
	urlVal, ok := cfg["url"]
	if !ok {
		return nil, fmt.Errorf("prometheus monitor %q: missing required field 'url'", name)
	}
	u, ok := urlVal.(string)
	if !ok || u == "" {
		return nil, fmt.Errorf("prometheus monitor %q: 'url' must be a non-empty string", name)
	}

	// Required: query
	queryVal, ok := cfg["query"]
	if !ok {
		return nil, fmt.Errorf("prometheus monitor %q: missing required field 'query'", name)
	}
	query, ok := queryVal.(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("prometheus monitor %q: 'query' must be a non-empty string", name)
	}

	// Optional: result (default "scalar")
	resultType := "scalar"
	if v, ok := cfg["result"]; ok {
		if s, ok := v.(string); ok && s != "" {
			resultType = s
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	return &prometheusMonitor{
		name:       name,
		url:        u,
		query:      query,
		resultType: resultType,
		client:     client,
	}, nil
}

func (m *prometheusMonitor) Name() string { return m.name }

func (m *prometheusMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()

	endpoint := m.url + "/api/v1/query?query=" + url.QueryEscape(m.query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}
	defer resp.Body.Close()

	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     any    `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	if envelope.Status != "success" {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("prometheus returned status %q", envelope.Status),
			Timestamp:   now,
		}, nil
	}

	// Re-marshal data.result for structured parsing
	resultBytes, err := json.Marshal(envelope.Data.Result)
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	var val float64

	switch m.resultType {
	case "scalar":
		// result is [timestamp, "value_string"]
		var scalarResult []json.RawMessage
		if err := json.Unmarshal(resultBytes, &scalarResult); err != nil || len(scalarResult) < 2 {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     "unexpected scalar result format",
				Timestamp:   now,
			}, nil
		}
		var valStr string
		if err := json.Unmarshal(scalarResult[1], &valStr); err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     "failed to parse scalar value string",
				Timestamp:   now,
			}, nil
		}
		val, err = strconv.ParseFloat(valStr, 64)
		if err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     fmt.Sprintf("failed to parse scalar value %q: %s", valStr, err.Error()),
				Timestamp:   now,
			}, nil
		}

	case "first_value":
		// result is a vector array; take result[0].value[1]
		var vectorResult []struct {
			Value []json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(resultBytes, &vectorResult); err != nil || len(vectorResult) == 0 {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     "unexpected vector result format",
				Timestamp:   now,
			}, nil
		}
		if len(vectorResult[0].Value) < 2 {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     "vector result[0].value has fewer than 2 elements",
				Timestamp:   now,
			}, nil
		}
		var valStr string
		if err := json.Unmarshal(vectorResult[0].Value[1], &valStr); err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     "failed to parse first_value string",
				Timestamp:   now,
			}, nil
		}
		val, err = strconv.ParseFloat(valStr, 64)
		if err != nil {
			return state.CheckResult{
				MonitorName: m.name,
				Status:      state.CheckUnknown,
				Message:     fmt.Sprintf("failed to parse first_value %q: %s", valStr, err.Error()),
				Timestamp:   now,
			}, nil
		}

	default:
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("unsupported result type %q", m.resultType),
			Timestamp:   now,
		}, nil
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       val,
		Message:     fmt.Sprintf("%.6g", val),
		Timestamp:   now,
	}, nil
}
