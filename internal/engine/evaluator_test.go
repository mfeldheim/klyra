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

// TestRecoveryForZeroResolvesImmediately verifies that when recovery_for is 0
// the alarm resolves immediately on the first good check (existing behavior).
func TestRecoveryForZeroResolvesImmediately(t *testing.T) {
	st := state.NewStore()
	now := time.Now()
	firedAt := now.Add(-5 * time.Minute)
	st.SetAlarm(state.AlarmState{
		MonitorName: "test",
		Status:      state.AlarmFiring,
		FiredAt:     &firedAt,
	})
	thr := config.ThresholdConfig{
		Operator:    "lt",
		Value:       float64(2),
		RecoveryFor: config.Duration{Duration: 0},
	}
	result := state.CheckResult{
		MonitorName: "test",
		Status:      state.CheckOK,
		Value:       float64(5), // threshold NOT met → good check
		Timestamp:   now,
	}

	ev := engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionResolved {
		t.Fatalf("expected immediate RESOLVED event, got %v", ev)
	}
	alarm, _ := st.GetAlarm("test")
	if alarm.Status != state.AlarmOK {
		t.Errorf("expected alarm status OK, got %v", alarm.Status)
	}
}

// TestRecoveryForStaysFiringUntilDurationElapsed verifies that when recovery_for
// is set, the alarm stays FIRING until the required duration of good checks elapses.
func TestRecoveryForStaysFiringUntilDurationElapsed(t *testing.T) {
	st := state.NewStore()
	base := time.Now()
	firedAt := base.Add(-5 * time.Minute)
	st.SetAlarm(state.AlarmState{
		MonitorName: "test",
		Status:      state.AlarmFiring,
		FiredAt:     &firedAt,
	})
	thr := config.ThresholdConfig{
		Operator:    "lt",
		Value:       float64(2),
		RecoveryFor: config.Duration{Duration: 100 * time.Millisecond},
	}

	// First good check — should start recovery window but stay FIRING
	result := state.CheckResult{
		MonitorName: "test",
		Status:      state.CheckOK,
		Value:       float64(5),
		Timestamp:   base,
	}
	ev := engine.ApplyResult(st, thr, result)
	if ev != nil {
		t.Fatalf("expected no event during recovery window, got %v", ev)
	}
	alarm, _ := st.GetAlarm("test")
	if alarm.Status != state.AlarmFiring {
		t.Errorf("expected alarm to remain FIRING during recovery window, got %v", alarm.Status)
	}
	if alarm.RecoverySince == nil {
		t.Error("expected RecoverySince to be set")
	}

	// Wait for recovery_for to elapse, then submit another good check
	time.Sleep(110 * time.Millisecond)
	result.Timestamp = time.Now()
	ev = engine.ApplyResult(st, thr, result)
	if ev == nil || ev.Transition != state.TransitionResolved {
		t.Fatalf("expected RESOLVED event after recovery_for elapsed, got %v", ev)
	}
	alarm, _ = st.GetAlarm("test")
	if alarm.Status != state.AlarmOK {
		t.Errorf("expected alarm status OK after resolution, got %v", alarm.Status)
	}
	if alarm.RecoverySince != nil {
		t.Error("expected RecoverySince to be cleared after resolution")
	}
}

// TestRecoveryWindowResetOnRefiring verifies that if the threshold is met again
// during the recovery window, RecoverySince is cleared.
func TestRecoveryWindowResetOnRefiring(t *testing.T) {
	st := state.NewStore()
	base := time.Now()
	firedAt := base.Add(-5 * time.Minute)
	recoverySince := base
	st.SetAlarm(state.AlarmState{
		MonitorName:   "test",
		Status:        state.AlarmFiring,
		FiredAt:       &firedAt,
		RecoverySince: &recoverySince,
	})
	thr := config.ThresholdConfig{
		Operator:    "lt",
		Value:       float64(2),
		RecoveryFor: config.Duration{Duration: 60 * time.Second},
	}

	// Threshold met again (bad check) during recovery window
	result := state.CheckResult{
		MonitorName: "test",
		Status:      state.CheckOK,
		Value:       float64(1), // threshold MET → bad check
		Timestamp:   base.Add(10 * time.Millisecond),
	}
	ev := engine.ApplyResult(st, thr, result)
	if ev != nil {
		t.Fatalf("expected no new event (already firing), got %v", ev)
	}
	alarm, _ := st.GetAlarm("test")
	if alarm.Status != state.AlarmFiring {
		t.Errorf("expected alarm to remain FIRING, got %v", alarm.Status)
	}
	if alarm.RecoverySince != nil {
		t.Error("expected RecoverySince to be cleared when threshold is met again")
	}
}
