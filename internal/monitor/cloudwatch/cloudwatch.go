// Package cloudwatch implements an AWS CloudWatch metric monitor.
package cloudwatch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	monitor "github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

func init() {
	monitor.Register("cloudwatch", New)
}

type metricAPI interface {
	GetMetricStatistics(ctx context.Context, params *cw.GetMetricStatisticsInput, optFns ...func(*cw.Options)) (*cw.GetMetricStatisticsOutput, error)
}

type cloudWatchMonitor struct {
	name       string
	namespace  string
	metricName string
	stat       string
	periodSec  int32
	lookback   time.Duration
	dimensions []cwtypes.Dimension
	client     metricAPI
}

func New(name string, cfg map[string]any) (monitor.Monitor, error) {
	region, err := cfgString(cfg, "region", true)
	if err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	}
	namespace, err := cfgString(cfg, "namespace", true)
	if err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	}
	metricName, err := cfgString(cfg, "metric", true)
	if err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	}

	stat := "Maximum"
	if v, err := cfgString(cfg, "stat", false); err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	} else if v != "" {
		stat = v
	}
	if !isSupportedStat(stat) {
		return nil, fmt.Errorf("cloudwatch monitor %q: unsupported stat %q (allowed: Average, Sum, Minimum, Maximum, SampleCount)", name, stat)
	}

	periodSec := int32(300)
	if p, ok, err := cfgInt(cfg, "period"); err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	} else if ok {
		if p <= 0 {
			return nil, fmt.Errorf("cloudwatch monitor %q: period must be > 0", name)
		}
		periodSec = int32(p)
	}

	lookback := 10 * time.Minute
	if raw, ok := cfg["lookback"]; ok {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("cloudwatch monitor %q: field %q must be a duration string", name, "lookback")
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("cloudwatch monitor %q: invalid lookback: %w", name, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("cloudwatch monitor %q: lookback must be > 0", name)
		}
		lookback = d
	}
	if lookback < time.Duration(periodSec)*time.Second {
		lookback = time.Duration(periodSec) * time.Second
	}

	dimensions, err := cfgDimensions(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: %w", name, err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("cloudwatch monitor %q: load AWS config: %w", name, err)
	}

	return &cloudWatchMonitor{
		name:       name,
		namespace:  namespace,
		metricName: metricName,
		stat:       stat,
		periodSec:  periodSec,
		lookback:   lookback,
		dimensions: dimensions,
		client:     cw.NewFromConfig(awsCfg),
	}, nil
}

func (m *cloudWatchMonitor) Name() string { return m.name }

func (m *cloudWatchMonitor) Check(ctx context.Context) (state.CheckResult, error) {
	now := time.Now().UTC()
	out, err := m.client.GetMetricStatistics(ctx, &cw.GetMetricStatisticsInput{
		Namespace:  &m.namespace,
		MetricName: &m.metricName,
		Dimensions: m.dimensions,
		StartTime:  ptrTime(now.Add(-m.lookback)),
		EndTime:    ptrTime(now),
		Period:     &m.periodSec,
		Statistics: []cwtypes.Statistic{cwtypes.Statistic(m.stat)},
	})
	if err != nil {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     err.Error(),
			Timestamp:   now,
		}, nil
	}

	if len(out.Datapoints) == 0 {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     "no CloudWatch datapoints returned",
			Timestamp:   now,
		}, nil
	}

	sort.Slice(out.Datapoints, func(i, j int) bool {
		return out.Datapoints[i].Timestamp.Before(*out.Datapoints[j].Timestamp)
	})
	latest := out.Datapoints[len(out.Datapoints)-1]

	value, ok := datapointValue(latest, m.stat)
	if !ok {
		return state.CheckResult{
			MonitorName: m.name,
			Status:      state.CheckUnknown,
			Message:     fmt.Sprintf("latest datapoint missing %s value", m.stat),
			Timestamp:   now,
		}, nil
	}

	msg := fmt.Sprintf("%s=%g", m.stat, value)
	if latest.Timestamp != nil {
		msg = fmt.Sprintf("%s at %s", msg, latest.Timestamp.UTC().Format(time.RFC3339))
	}

	return state.CheckResult{
		MonitorName: m.name,
		Status:      state.CheckOK,
		Value:       value,
		Message:     msg,
		Timestamp:   now,
	}, nil
}

func datapointValue(dp cwtypes.Datapoint, stat string) (float64, bool) {
	switch stat {
	case "Average":
		if dp.Average == nil {
			return 0, false
		}
		return *dp.Average, true
	case "Sum":
		if dp.Sum == nil {
			return 0, false
		}
		return *dp.Sum, true
	case "Minimum":
		if dp.Minimum == nil {
			return 0, false
		}
		return *dp.Minimum, true
	case "Maximum":
		if dp.Maximum == nil {
			return 0, false
		}
		return *dp.Maximum, true
	case "SampleCount":
		if dp.SampleCount == nil {
			return 0, false
		}
		return *dp.SampleCount, true
	default:
		return 0, false
	}
}

func isSupportedStat(stat string) bool {
	switch stat {
	case "Average", "Sum", "Minimum", "Maximum", "SampleCount":
		return true
	default:
		return false
	}
}

func cfgDimensions(cfg map[string]any) ([]cwtypes.Dimension, error) {
	var out []cwtypes.Dimension

	if raw, ok := cfg["dimensions"]; ok {
		dimsMap, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field %q must be a map", "dimensions")
		}
		for k, v := range dimsMap {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("dimension %q must be a string", k)
			}
			key := k
			val := s
			out = append(out, cwtypes.Dimension{Name: &key, Value: &val})
		}
	}

	if streamName, err := cfgString(cfg, "stream_name", false); err != nil {
		return nil, err
	} else if streamName != "" {
		key := "StreamName"
		val := streamName
		out = append(out, cwtypes.Dimension{Name: &key, Value: &val})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("at least one dimension is required (use dimensions or stream_name)")
	}
	return out, nil
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
	if required && strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("field %q must not be empty", key)
	}
	return s, nil
}

func cfgInt(cfg map[string]any, key string) (int, bool, error) {
	v, ok := cfg[key]
	if !ok {
		return 0, false, nil
	}
	switch n := v.(type) {
	case int:
		return n, true, nil
	case int64:
		return int(n), true, nil
	case float64:
		return int(n), true, nil
	default:
		return 0, true, fmt.Errorf("field %q must be a number", key)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
