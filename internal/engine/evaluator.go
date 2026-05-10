package engine

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/state"
)

// regexCache caches compiled regular expressions to avoid recompilation on
// every EvaluateThreshold call (Bug 4).
var regexCache sync.Map // map[string]*regexp.Regexp

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
		// Bug 4: cache compiled patterns instead of recompiling every call.
		pattern := fmt.Sprintf("%v", thr.Value)
		var re *regexp.Regexp
		if cached, ok := regexCache.Load(pattern); ok {
			re = cached.(*regexp.Regexp)
		} else {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return false
			}
			regexCache.Store(pattern, compiled)
			re = compiled
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
		MonitorName:   r.MonitorName,
		Status:        current.Status,
		FiredAt:       current.FiredAt,
		PendingSince:  current.PendingSince,
		RecoverySince: current.RecoverySince,
		LastCheck:     now,
		LastValue:     r.Value,
		Message:       r.Message,
		Icon:          current.Icon,
		Group:         current.Group,
	}

	// Bug 3: CheckError (failed to run) and CheckUnknown both skip threshold
	// evaluation and produce AlarmUnknown with no event.
	if r.Status == state.CheckUnknown || r.Status == state.CheckError {
		updated.Status = state.AlarmUnknown
		updated.PendingSince = nil
		st.SetAlarm(updated)
		return nil
	}

	forDur := thr.For.Duration
	var event *state.AlarmEvent

	if met {
		updated.RecoverySince = nil
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
		} else {
			// Bug 1: condition met but for-duration not yet elapsed (pending).
			// Persist a valid status rather than leaving it as the zero value.
			updated.Status = state.AlarmOK
		}
	} else {
		updated.PendingSince = nil
		if current.Status == state.AlarmFiring {
			recoveryDur := thr.RecoveryFor.Duration
			if recoveryDur == 0 {
				// No recovery_for configured — resolve immediately (existing behavior)
				updated.Status = state.AlarmOK
				updated.FiredAt = nil
				updated.RecoverySince = nil
				var origFiredAt time.Time
				if current.FiredAt != nil {
					origFiredAt = *current.FiredAt
				}
				event = &state.AlarmEvent{
					MonitorName: r.MonitorName,
					Transition:  state.TransitionResolved,
					Message:     r.Message,
					Value:       r.Value,
					FiredAt:     origFiredAt,
				}
			} else {
				// Recovery hysteresis: require recovery_for duration of good checks
				if updated.RecoverySince == nil {
					updated.RecoverySince = &now
				}
				if now.Sub(*updated.RecoverySince) >= recoveryDur {
					// Recovery sustained long enough — resolve
					updated.Status = state.AlarmOK
					updated.FiredAt = nil
					updated.RecoverySince = nil
					var origFiredAt time.Time
					if current.FiredAt != nil {
						origFiredAt = *current.FiredAt
					}
					event = &state.AlarmEvent{
						MonitorName: r.MonitorName,
						Transition:  state.TransitionResolved,
						Message:     r.Message,
						Value:       r.Value,
						FiredAt:     origFiredAt,
					}
				} else {
					// Still in recovery window — stay FIRING
					updated.Status = state.AlarmFiring
				}
			}
		} else {
			updated.RecoverySince = nil
			updated.Status = state.AlarmOK
		}
	}

	st.SetAlarm(updated)

	return event
}
