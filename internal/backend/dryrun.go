package backend

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"liteone/internal/model"
)

type DryRun struct {
	logger *slog.Logger

	mu      sync.RWMutex
	plan    model.RuntimePlan
	started time.Time
}

func NewDryRun(logger *slog.Logger) (*DryRun, error) {
	return &DryRun{
		logger:  logger.With(slog.String("runtime", "dry-run")),
		started: time.Now(),
	}, nil
}

func (d *DryRun) Name() string {
	return "dry-run"
}

func (d *DryRun) Apply(_ context.Context, plan model.RuntimePlan) error {
	d.mu.Lock()
	d.plan = plan
	d.mu.Unlock()

	d.logger.Info("applied runtime plan",
		slog.String("node", plan.Node.Summary()),
		slog.Int("users", len(plan.Users)),
		slog.Int("rules", len(plan.Rules)),
	)
	return nil
}

func (d *DryRun) Snapshot(_ context.Context) (model.RuntimeSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return model.RuntimeSnapshot{
		Status: model.StatusReport{
			CPU: 0,
			Mem: model.ResourceUsage{
				Total: 0,
				Used:  0,
			},
			Swap: model.ResourceUsage{
				Total: 0,
				Used:  0,
			},
			Disk: model.ResourceUsage{
				Total: 0,
				Used:  0,
			},
			Uptime: int64(time.Since(d.started).Seconds()),
		},
	}, nil
}

func (d *DryRun) Close() error {
	return nil
}
