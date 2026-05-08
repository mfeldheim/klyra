package monitor_test

import (
	"context"
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/monitor"
	"github.com/mfeldheim/klyra/internal/state"
)

type fakeMonitor struct {
	name   string
	result state.CheckResult
}

func (f *fakeMonitor) Name() string { return f.name }
func (f *fakeMonitor) Check(_ context.Context) (state.CheckResult, error) {
	return f.result, nil
}

func TestRunnerSendsResults(t *testing.T) {
	results := make(chan state.CheckResult, 5)
	m := &fakeMonitor{
		name:   "test",
		result: state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go monitor.Run(ctx, m, 50*time.Millisecond, results)

	var got []state.CheckResult
	for r := range results {
		got = append(got, r)
		if len(got) >= 2 {
			break
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(got))
	}
	if got[0].MonitorName != "test" {
		t.Errorf("unexpected monitor name: %s", got[0].MonitorName)
	}
}
