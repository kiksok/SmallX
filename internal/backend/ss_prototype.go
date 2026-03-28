package backend

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"liteone/internal/model"
	"liteone/internal/ss"
)

type SSPrototype struct {
	logger *slog.Logger

	mu      sync.RWMutex
	cfg     ss.RuntimeConfig
	started time.Time
}

func NewSSPrototype(logger *slog.Logger) (*SSPrototype, error) {
	return &SSPrototype{
		logger:  logger.With(slog.String("runtime", "ss-prototype")),
		started: time.Now(),
	}, nil
}

func (s *SSPrototype) Name() string {
	return "ss-prototype"
}

func (s *SSPrototype) Apply(_ context.Context, plan model.RuntimePlan) error {
	cfg, err := ss.Translate(plan.Node, plan.Users)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()

	s.logger.Info("translated ss runtime config",
		slog.String("cipher", cfg.Server.Cipher),
		slog.Int("port", cfg.Server.ServerPort),
		slog.Bool("tcp", cfg.Server.EnableTCP),
		slog.Bool("udp", cfg.Server.EnableUDP),
		slog.Bool("obfs_http", cfg.Server.Obfs.Enabled),
		slog.Int("users", len(cfg.Users)),
	)
	return nil
}

func (s *SSPrototype) Snapshot(_ context.Context) (model.RuntimeSnapshot, error) {
	return model.RuntimeSnapshot{
		Status: model.StatusReport{
			Uptime: int64(time.Since(s.started).Seconds()),
		},
	}, nil
}

func (s *SSPrototype) Close() error {
	return nil
}
