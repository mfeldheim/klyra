package engine_test

import (
	"testing"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/state"
)

func TestEvaluateThresholdLt(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "lt", Value: float64(2)}
	if !engine.EvaluateThreshold(thr, float64(1)) {
		t.Error("1 < 2 should be true")
	}
	if engine.EvaluateThreshold(thr, float64(3)) {
		t.Error("3 < 2 should be false")
	}
}

func TestEvaluateThresholdEqBool(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "eq", Value: false}
	if !engine.EvaluateThreshold(thr, false) {
		t.Error("false eq false should be true")
	}
	if engine.EvaluateThreshold(thr, true) {
		t.Error("true eq false should be false")
	}
}

func TestEvaluateThresholdContains(t *testing.T) {
	thr := config.ThresholdConfig{Operator: "contains", Value: "error"}
	if !engine.EvaluateThreshold(thr, "some error occurred") {
		t.Error("expected contains to match")
	}
	if engine.EvaluateThreshold(thr, "everything fine") {
		t.Error("expected contains not to match")
	}
}

func TestApplyResultFiresAfterForDuration(t *testing.T) {
	st := state.NewStore()
	thr := config.ThresholdConfig{
		Operator: "lt",
		Value:    float64(2),
		For:      config.Duration{Duration: 100 * time.Millisecond},
	}
	result := state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: float64(1), Timestamp: time.Now()}

	ev := engine.ApplyResult(st, thr, result)
	if ev != nil {
		t.Fatal("should not fire before for-duration elapses")
	}

	time.Sleep(110 * time.Millisecond)
	result.Timestamp = time.Now()
	ev = engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionFiring {
		t.Fatalf("expected FIRING event, got %v", ev)
	}
}

func TestApplyResultResolvesWhenThresholdClears(t *testing.T) {
	st := state.NewStore()
	now := time.Now()
	st.SetAlarm(state.AlarmState{
		MonitorName: "test",
		Status:      state.AlarmFiring,
		FiredAt:     &now,
	})
	thr := config.ThresholdConfig{Operator: "lt", Value: float64(2)}
	result := state.CheckResult{MonitorName: "test", Status: state.CheckOK, Value: float64(5), Timestamp: time.Now()}

	ev := engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionResolved {
		t.Fatalf("expected RESOLVED event, got %v", ev)
	}
}
