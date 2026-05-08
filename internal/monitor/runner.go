package monitor

import (
	"context"
	"time"

	"github.com/mfeldheim/klyra/internal/state"
)

func Run(ctx context.Context, m Monitor, interval time.Duration, results chan<- state.CheckResult) {
	send := func() {
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
		select {
		case results <- r:
		case <-ctx.Done():
		}
	}

	// Run immediately on startup
	send()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			send()
		case <-ctx.Done():
			return
		}
	}
}
