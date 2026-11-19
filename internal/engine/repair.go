package engine

import (
	"log"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

// RepairStaleFiring clears fire state left on alarms that are no longer firing
// and records the missing RESOLVED transition. Older versions could drop an
// alarm from FIRING to OK without a RESOLVED event, leaving firedAt and
// incidentId behind; the history then ended on FIRING and the UI kept showing
// the monitor as firing. The real recovery time is unknown, so the entry is
// stamped with now. It returns the names of repaired monitors.
func RepairStaleFiring(st *state.Store, now time.Time) []string {
	var repaired []string
	for name, a := range st.Alarms() {
		if a.Status == state.AlarmFiring || (a.FiredAt == nil && a.IncidentID == nil) {
			continue
		}
		var incID string
		if a.IncidentID != nil {
			incID = *a.IncidentID
		}
		st.AppendHistory(state.HistoryEvent{
			MonitorName: name,
			Transition:  state.TransitionResolved,
			At:          now,
			Message:     "state repair: alarm was " + string(a.Status) + " with stale fire state; actual recovery time unknown",
			IncidentID:  incID,
		})
		a.FiredAt = nil
		a.IncidentID = nil
		a.RecoverySince = nil
		st.SetAlarm(a)
		repaired = append(repaired, name)
		log.Printf("state repair: cleared stale fire state of %q (incident %q)", name, incID)
	}
	return repaired
}
