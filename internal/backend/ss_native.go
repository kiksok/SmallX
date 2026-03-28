package backend

import (
	"context"
	"log/slog"

	"smallx/internal/model"
	"smallx/internal/ss"
)

type SSNative struct {
	logger  *slog.Logger
	service *ss.Service
}

func NewSSNative(logger *slog.Logger) (*SSNative, error) {
	return &SSNative{
		logger:  logger.With(slog.String("runtime", "ss-native")),
		service: ss.NewService(logger),
	}, nil
}

func (s *SSNative) Name() string {
	return "ss-native"
}

func (s *SSNative) Apply(_ context.Context, plan model.RuntimePlan) error {
	cfg, err := ss.Translate(plan.Node, plan.Users)
	if err != nil {
		return err
	}
	return s.service.Apply(cfg)
}

func (s *SSNative) Snapshot(ctx context.Context) (model.RuntimeSnapshot, error) {
	return s.service.Snapshot(ctx)
}

func (s *SSNative) Close() error {
	return s.service.Close()
}
