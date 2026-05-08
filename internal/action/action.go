package action

import (
	"context"

	"github.com/mfeldheim/klyra/internal/state"
)

type Action interface {
	Name() string
	Fire(ctx context.Context, event state.AlarmEvent) error
}

type Factory func(name string, cfg map[string]any) (Action, error)
