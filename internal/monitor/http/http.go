package httpmon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Knetic/govaluate"
	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("http", New)
}

type httpMonitor struct {
	name       string
	url        string
	method     string
	client     *http.Client
	headers    map[string]string
	checks     []httpCheck
	hasChecks  bool
}

type expression struct {
	operator string
	value    any
}

type httpCheck struct {
	label    string
	evaluate func(resp *http.Response, body []byte, jsonBody any, latencyMs float64) (bool, string)
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

	checks := make([]httpCheck, 0, 6)
	_, hasStatusExpression := cfg["status"]

	// Backward compatibility: legacy expect_status and expect_body are translated
	// into expression checks when present.
	if !hasStatusExpression {
		expectStatus := 200
		if v, ok := cfg["expect_status"]; ok {
			n, ok := asInt(v)
			if !ok {
				return nil, fmt.Errorf("http monitor %q: 'expect_status' must be a number", name)
			}
			expectStatus = n
		}
		checks = append(checks, httpCheck{
			label: "status",
			evaluate: func(resp *http.Response, _ []byte, _ any, _ float64) (bool, string) {
				ok := resp.StatusCode == expectStatus
				if ok {
					return true, fmt.Sprintf("status=%d", resp.StatusCode)
				}
				return false, fmt.Sprintf("status=%d expected=%d", resp.StatusCode, expectStatus)
			},
		})
	}

	if v, ok := cfg["expect_body"]; ok {
		expectBody, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("http monitor %q: 'expect_body' must be a string", name)
		}
		checks = append(checks, httpCheck{
			label: "body",
			evaluate: func(_ *http.Response, body []byte, _ any, _ float64) (bool, string) {
				ok := strings.Contains(string(body), expectBody)
				if ok {
					return true, fmt.Sprintf("body contains %q", expectBody)
				}
				return false, fmt.Sprintf("body missing %q", expectBody)
			},
		})
	}

	if v, ok := cfg["status"]; ok {
		expr, err := parseExpression(v)
		if err != nil {
			return nil, fmt.Errorf("http monitor %q: invalid 'status': %w", name, err)
		}
		checks = append(checks, httpCheck{
			label: "status",
			evaluate: func(resp *http.Response, _ []byte, _ any, _ float64) (bool, string) {
				ok := evalExpression(expr, float64(resp.StatusCode))
				if ok {
					return true, fmt.Sprintf("status=%d", resp.StatusCode)
				}
				return false, fmt.Sprintf("status=%d does not satisfy %s %v", resp.StatusCode, expr.operator, expr.value)
			},
		})
	}

	if v, ok := cfg["body"]; ok {
		expr, err := parseExpression(v)
		if err != nil {
			return nil, fmt.Errorf("http monitor %q: invalid 'body': %w", name, err)
		}
		checks = append(checks, httpCheck{
			label: "body",
			evaluate: func(_ *http.Response, body []byte, _ any, _ float64) (bool, string) {
				bodyText := string(body)
				ok := evalExpression(expr, bodyText)
				if ok {
					return true, "body expression matched"
				}
				return false, fmt.Sprintf("body does not satisfy %s %v", expr.operator, expr.value)
			},
		})
	}

	if v, ok := cfg["header"]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("http monitor %q: 'header' must be an object", name)
		}
		headerNameAny, ok := m["name"]
		if !ok {
			return nil, fmt.Errorf("http monitor %q: 'header.name' is required", name)
		}
		headerName, ok := headerNameAny.(string)
		if !ok || headerName == "" {
			return nil, fmt.Errorf("http monitor %q: 'header.name' must be a non-empty string", name)
		}
		expr, err := parseExpression(m)
		if err != nil {
			return nil, fmt.Errorf("http monitor %q: invalid 'header' expression: %w", name, err)
		}
		checks = append(checks, httpCheck{
			label: "header",
			evaluate: func(resp *http.Response, _ []byte, _ any, _ float64) (bool, string) {
				val := resp.Header.Get(headerName)
				ok := evalExpression(expr, val)
				if ok {
					return true, fmt.Sprintf("header %s matched", headerName)
				}
				return false, fmt.Sprintf("header %s value %q does not satisfy %s %v", headerName, val, expr.operator, expr.value)
			},
		})
	}

	if v, ok := cfg["json"]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("http monitor %q: 'json' must be an object", name)
		}

		if exprRaw, exists := m["expression"]; exists {
			exprText, ok := exprRaw.(string)
			if !ok || strings.TrimSpace(exprText) == "" {
				return nil, fmt.Errorf("http monitor %q: 'json.expression' must be a non-empty string", name)
			}
			parsedExpr, err := govaluate.NewEvaluableExpression(exprText)
			if err != nil {
				return nil, fmt.Errorf("http monitor %q: invalid 'json.expression': %w", name, err)
			}
			checks = append(checks, httpCheck{
				label: "json",
				evaluate: func(_ *http.Response, _ []byte, jsonBody any, _ float64) (bool, string) {
					if jsonBody == nil {
						return false, "response body is not valid JSON"
					}
					vars := map[string]any{}
					flattenJSON(vars, "", jsonBody)
					result, err := parsedExpr.Evaluate(vars)
					if err != nil {
						return false, fmt.Sprintf("json expression error: %v", err)
					}
					matched, ok := result.(bool)
					if !ok {
						return false, "json expression must return a boolean"
					}
					if matched {
						return true, "json expression matched"
					}
					return false, fmt.Sprintf("json expression %q is false", exprText)
				},
			})
		} else {
			pathAny, ok := m["path"]
			if !ok {
				return nil, fmt.Errorf("http monitor %q: 'json.path' is required when 'json.expression' is not set", name)
			}
			path, ok := pathAny.(string)
			if !ok || strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("http monitor %q: 'json.path' must be a non-empty string", name)
			}
			expr, err := parseExpression(m)
			if err != nil {
				return nil, fmt.Errorf("http monitor %q: invalid 'json' expression: %w", name, err)
			}
			checks = append(checks, httpCheck{
				label: "json",
				evaluate: func(_ *http.Response, _ []byte, jsonBody any, _ float64) (bool, string) {
					if jsonBody == nil {
						return false, "response body is not valid JSON"
					}
					v, err := getJSONPath(jsonBody, path)
					if err != nil {
						return false, err.Error()
					}
					ok := evalExpression(expr, v)
					if ok {
						return true, fmt.Sprintf("json %s matched", path)
					}
					return false, fmt.Sprintf("json %s=%v does not satisfy %s %v", path, v, expr.operator, expr.value)
				},
			})
		}
	}

	if v, ok := cfg["latency_ms"]; ok {
		expr, err := parseExpression(v)
		if err != nil {
			return nil, fmt.Errorf("http monitor %q: invalid 'latency_ms': %w", name, err)
		}
		checks = append(checks, httpCheck{
			label: "latency_ms",
			evaluate: func(_ *http.Response, _ []byte, _ any, latencyMs float64) (bool, string) {
				ok := evalExpression(expr, latencyMs)
				if ok {
					return true, fmt.Sprintf("latency_ms=%.2f", latencyMs)
				}
				return false, fmt.Sprintf("latency_ms=%.2f does not satisfy %s %v", latencyMs, expr.operator, expr.value)
			},
		})
	}

	return &httpMonitor{
		name:      name,
		url:       url,
		method:    method,
		client:    client,
		headers:   headers,
		checks:    checks,
		hasChecks: len(checks) > 0,
	}, nil
}

func (m *httpMonitor) Name() string { return m.name }

func (m *httpMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()
	start := time.Now()

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
	latencyMs := time.Since(start).Seconds() * 1000
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckOK,
			Value:       false,
			Message:     fmt.Sprintf("failed to read response body: %s", err.Error()),
			Timestamp:   now,
		}, nil
	}

	var jsonBody any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &jsonBody)
	}

	ok := true
	parts := make([]string, 0, len(m.checks)+1)
	for _, check := range m.checks {
		matched, msg := check.evaluate(resp, body, jsonBody, latencyMs)
		if !matched {
			ok = false
		}
		if msg != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", check.label, msg))
		}
	}
	parts = append(parts, fmt.Sprintf("latency_ms=%.2f", latencyMs))
	msg := strings.Join(parts, "; ")

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       ok,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

func parseExpression(v any) (expression, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return expression{}, fmt.Errorf("must be an object with 'operator' and 'value'")
	}
	opAny, ok := m["operator"]
	if !ok {
		return expression{}, fmt.Errorf("missing 'operator'")
	}
	op, ok := opAny.(string)
	if !ok || op == "" {
		return expression{}, fmt.Errorf("'operator' must be a non-empty string")
	}
	val, ok := m["value"]
	if !ok {
		return expression{}, fmt.Errorf("missing 'value'")
	}
	return expression{operator: op, value: val}, nil
}

func evalExpression(expr expression, actual any) bool {
	switch expr.operator {
	case "lt":
		return toFloat(actual) < toFloat(expr.value)
	case "gt":
		return toFloat(actual) > toFloat(expr.value)
	case "lte":
		return toFloat(actual) <= toFloat(expr.value)
	case "gte":
		return toFloat(actual) >= toFloat(expr.value)
	case "eq":
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expr.value)
	case "neq":
		return fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expr.value)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", actual), fmt.Sprintf("%v", expr.value))
	case "matches":
		re, err := regexp.Compile(fmt.Sprintf("%v", expr.value))
		if err != nil {
			return false
		}
		return re.MatchString(fmt.Sprintf("%v", actual))
	default:
		return false
	}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case uint:
		return float64(n)
	case uint64:
		return float64(n)
	case uint32:
		return float64(n)
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err == nil {
			return f
		}
	}
	return math.NaN()
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case uint:
		return int(n), true
	case uint64:
		return int(n), true
	case uint32:
		return int(n), true
	default:
		return 0, false
	}
}

func getJSONPath(root any, path string) (any, error) {
	current := root
	parts := strings.Split(path, ".")
	for _, rawPart := range parts {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return nil, fmt.Errorf("invalid json path %q", path)
		}

		if idx, err := strconv.Atoi(part); err == nil {
			arr, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("json path %q: %q is not an array", path, part)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("json path %q: index %d out of range", path, idx)
			}
			current = arr[idx]
			continue
		}

		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("json path %q: %q is not an object", path, part)
		}
		next, ok := obj[part]
		if !ok {
			return nil, fmt.Errorf("json path %q: key %q not found", path, part)
		}
		current = next
	}
	return current, nil
}

func flattenJSON(vars map[string]any, prefix string, v any) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			key := k
			if prefix != "" {
				key = prefix + "_" + k
			}
			flattenJSON(vars, key, child)
		}
	case []any:
		for i, child := range n {
			idxKey := strconv.Itoa(i)
			if prefix != "" {
				idxKey = prefix + "_" + idxKey
			}
			flattenJSON(vars, idxKey, child)
		}
	default:
		if prefix != "" {
			vars[prefix] = n
		}
	}
}
