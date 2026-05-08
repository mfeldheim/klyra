package monitor

import (
	"context"

	"github.com/mfeldheim/klyra/internal/state"
)

type Monitor interface {
	Name() string
	Check(ctx context.Context) (state.CheckResult, error)
}

type Factory func(name string, cfg map[string]any) (Monitor, error)
