package monitor

import (
	"context"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func Run(ctx context.Context, m Monitor, interval time.Duration, results chan<- state.CheckResult) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r, err := m.Check(ctx)
			if err != nil {
				r = state.CheckResult{
					MonitorName: m.Name(),
					Status:      state.CheckUnknown,
					Message:     err.Error(),
					Timestamp:   time.Now(),
				}
			}
			if r.MonitorName == "" {
				r.MonitorName = m.Name()
			}
			if r.Timestamp.IsZero() {
				r.Timestamp = time.Now()
			}
			results <- r
		case <-ctx.Done():
			return
		}
	}
}
