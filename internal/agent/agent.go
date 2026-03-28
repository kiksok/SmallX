package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"smallx/internal/backend"
	"smallx/internal/config"
	"smallx/internal/model"
	"smallx/internal/provider"
)

type Agent struct {
	cfg      *config.Config
	logger   *slog.Logger
	provider provider.Provider
	runtime  backend.Runtime

	lastNode  *model.NodeConfig
	lastUsers []model.UserInfo
	lastRules []model.AuditRule
}

func New(cfg *config.Config, logger *slog.Logger, provider provider.Provider, runtime backend.Runtime) *Agent {
	return &Agent{
		cfg:      cfg,
		logger:   logger.With(slog.String("component", "agent")),
		provider: provider,
		runtime:  runtime,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.syncOnce(ctx); err != nil {
		return err
	}
	if err := a.pushSnapshot(ctx); err != nil {
		a.logger.Warn("initial snapshot push failed", slog.Any("error", err))
	}

	pullTicker := time.NewTicker(a.cfg.Sync.PullEvery())
	defer pullTicker.Stop()

	statusTicker := time.NewTicker(a.cfg.Sync.StatusEvery())
	defer statusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pullTicker.C:
			if err := a.syncOnce(ctx); err != nil {
				a.logger.Warn("sync tick failed", slog.Any("error", err))
			}
		case <-statusTicker.C:
			if err := a.pushSnapshot(ctx); err != nil {
				a.logger.Warn("status tick failed", slog.Any("error", err))
			}
		}
	}
}

func (a *Agent) syncOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Runtime.ApplyTimeoutDuration())
	defer cancel()

	node, nodeChanged, err := a.provider.FetchNode(ctx)
	if err != nil {
		return err
	}

	users, usersChanged, err := a.provider.FetchUsers(ctx)
	if err != nil {
		return err
	}

	rules, rulesChanged, err := a.provider.FetchRules(ctx)
	if err != nil {
		return err
	}

	if a.lastNode == nil {
		nodeChanged = true
	}
	if !nodeChanged && !usersChanged && !rulesChanged {
		a.logger.Debug("no control-plane changes detected")
		return nil
	}

	if nodeChanged {
		a.lastNode = &node
	}
	if usersChanged {
		a.lastUsers = users
	}
	if rulesChanged {
		a.lastRules = rules
	}

	if a.lastNode == nil {
		return errors.New("node config is empty")
	}

	plan := model.RuntimePlan{
		Node:  *a.lastNode,
		Users: append([]model.UserInfo(nil), a.lastUsers...),
		Rules: append([]model.AuditRule(nil), a.lastRules...),
	}

	a.logger.Info("applying control-plane update",
		slog.Bool("node_changed", nodeChanged),
		slog.Bool("users_changed", usersChanged),
		slog.Bool("rules_changed", rulesChanged),
		slog.Int("users", len(plan.Users)),
		slog.Int("rules", len(plan.Rules)),
	)

	return a.runtime.Apply(ctx, plan)
}

func (a *Agent) pushSnapshot(ctx context.Context) error {
	snapshot, err := a.runtime.Snapshot(ctx)
	if err != nil {
		return err
	}

	if err := a.provider.ReportStatus(ctx, snapshot.Status); err != nil {
		return err
	}
	if err := a.provider.ReportTraffic(ctx, snapshot.Traffic); err != nil {
		return err
	}
	if err := a.provider.ReportAliveIPs(ctx, snapshot.AliveIPs); err != nil {
		return err
	}
	if err := a.provider.ReportAudits(ctx, snapshot.Audits); err != nil {
		return err
	}

	return nil
}
