// Package promscr implements a monitor that scrapes a raw Prometheus text-format endpoint.
package promscr

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("prometheus_scrape", New)
}

type promScrapeMonitor struct {
	name   string
	url    string
	metric string
	agg    string // first | min | max | sum | count
	client *http.Client
}

// New creates a prometheus_scrape monitor that fetches raw Prometheus text exposition format.
// Required config: url, metric.
// Optional: result (aggregation over labeled series: first, min, max, sum, count; default "first").
func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	u, err := cfgString(cfg, "url", true)
	if err != nil {
		return nil, fmt.Errorf("prometheus_scrape monitor %q: %w", name, err)
	}
	metric, err := cfgString(cfg, "metric", true)
	if err != nil {
		return nil, fmt.Errorf("prometheus_scrape monitor %q: %w", name, err)
	}
	agg := "first"
	if v, _ := cfgString(cfg, "result", false); v != "" {
		switch v {
		case "first", "min", "max", "sum", "count":
			agg = v
		default:
			return nil, fmt.Errorf("prometheus_scrape monitor %q: result must be one of: first, min, max, sum, count", name)
		}
	}

	return &promScrapeMonitor{
		name:   name,
		url:    u,
		metric: metric,
		agg:    agg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (m *promScrapeMonitor) Name() string { return m.name }

func (m *promScrapeMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url, nil)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: now}, nil
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: err.Error(), Timestamp: now}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("HTTP %d from %s", resp.StatusCode, m.url),
			Timestamp:   now,
		}, nil
	}

	val, found, parseErr := aggregateMetric(resp.Body, m.metric, m.agg)
	if parseErr != nil {
		return state.CheckResult{MonitorName: m.name, Status: state.CheckUnknown, Message: parseErr.Error(), Timestamp: now}, nil
	}
	if !found {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("metric %q not found", m.metric),
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

// aggregateMetric parses Prometheus text exposition format, collects all values for
// metrics whose name matches target, and reduces them with the given aggregation.
func aggregateMetric(r io.Reader, target, agg string) (float64, bool, error) {
	scanner := bufio.NewScanner(r)

	var vals []float64
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		nameAndLabels := parts[0]
		metricName := nameAndLabels
		if i := strings.IndexByte(nameAndLabels, '{'); i >= 0 {
			metricName = nameAndLabels[:i]
		}
		if metricName != target {
			continue
		}
		v, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, false, fmt.Errorf("failed to parse value for %q: %w", target, err)
		}
		vals = append(vals, v)
		if agg == "first" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, false, err
	}
	if len(vals) == 0 {
		return 0, false, nil
	}

	switch agg {
	case "first":
		return vals[0], true, nil
	case "min":
		m := math.MaxFloat64
		for _, v := range vals {
			if v < m {
				m = v
			}
		}
		return m, true, nil
	case "max":
		m := -math.MaxFloat64
		for _, v := range vals {
			if v > m {
				m = v
			}
		}
		return m, true, nil
	case "sum":
		var s float64
		for _, v := range vals {
			s += v
		}
		return s, true, nil
	case "count":
		return float64(len(vals)), true, nil
	default:
		return vals[0], true, nil
	}
}

func cfgString(cfg map[string]any, key string, required bool) (string, error) {
	v, ok := cfg[key]
	if !ok {
		if required {
			return "", fmt.Errorf("missing required field %q", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string", key)
	}
	return s, nil
}
