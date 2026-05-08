package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/state"
)

// toFloat converts numeric types to float64. Returns 0 for unsupported types.
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
	}
	return 0
}

// EvaluateThreshold returns true when value satisfies the threshold condition.
func EvaluateThreshold(thr config.ThresholdConfig, value any) bool {
	switch thr.Operator {
	case "lt":
		return toFloat(value) < toFloat(thr.Value)
	case "gt":
		return toFloat(value) > toFloat(thr.Value)
	case "lte":
		return toFloat(value) <= toFloat(thr.Value)
	case "gte":
		return toFloat(value) >= toFloat(thr.Value)
	case "eq":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", thr.Value)
	case "neq":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", thr.Value)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", value), fmt.Sprintf("%v", thr.Value))
	case "matches":
		re, err := regexp.Compile(fmt.Sprintf("%v", thr.Value))
		if err != nil {
			return false
		}
		return re.MatchString(fmt.Sprintf("%v", value))
	}
	return false
}

// ApplyResult evaluates a CheckResult against the threshold and updates alarm
// state in the store. Returns a non-nil *AlarmEvent when a transition occurs.
func ApplyResult(st *state.Store, thr config.ThresholdConfig, r state.CheckResult) *state.AlarmEvent {
	current, _ := st.GetAlarm(r.MonitorName)
	met := EvaluateThreshold(thr, r.Value)
	now := r.Timestamp

	updated := state.AlarmState{
		MonitorName:  r.MonitorName,
		Status:       current.Status,
		FiredAt:      current.FiredAt,
		PendingSince: current.PendingSince,
		LastCheck:    now,
		LastValue:    r.Value,
		Message:      r.Message,
	}

	// Unknown status — clear pending, set unknown, no event.
	if r.Status == state.CheckUnknown {
		updated.Status = state.AlarmUnknown
		updated.PendingSince = nil
		st.SetAlarm(updated)
		return nil
	}

	forDur := thr.For.Duration
	var event *state.AlarmEvent

	if met {
		if updated.PendingSince == nil {
			updated.PendingSince = &now
		}
		if forDur == 0 || now.Sub(*updated.PendingSince) >= forDur {
			if current.Status != state.AlarmFiring {
				updated.Status = state.AlarmFiring
				updated.FiredAt = &now
				event = &state.AlarmEvent{
					MonitorName: r.MonitorName,
					Transition:  state.TransitionFiring,
					Message:     r.Message,
					Value:       r.Value,
					FiredAt:     now,
				}
			} else {
				updated.Status = state.AlarmFiring
			}
		}
	} else {
		updated.PendingSince = nil
		if current.Status == state.AlarmFiring {
			updated.Status = state.AlarmOK
			updated.FiredAt = nil
			event = &state.AlarmEvent{
				MonitorName: r.MonitorName,
				Transition:  state.TransitionResolved,
				Message:     r.Message,
				Value:       r.Value,
				FiredAt:     now,
			}
		} else {
			updated.Status = state.AlarmOK
		}
	}

	st.SetAlarm(updated)

	if event != nil {
		st.AppendHistory(state.HistoryEvent{
			MonitorName: r.MonitorName,
			Transition:  event.Transition,
			At:          now,
			Message:     r.Message,
		})
		return event
	}
	return nil
}
