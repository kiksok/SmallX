package backend

import (
	"context"

	"smallx/internal/model"
)

type Runtime interface {
	Name() string
	Apply(ctx context.Context, plan model.RuntimePlan) error
	Snapshot(ctx context.Context) (model.RuntimeSnapshot, error)
	Close() error
}
