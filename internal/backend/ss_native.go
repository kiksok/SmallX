package backend

import (
	"context"
	"log/slog"

	"smallx/internal/config"
	"smallx/internal/model"
	"smallx/internal/ss"
)

type SSNative struct {
	logger  *slog.Logger
	service *ss.Service
	runtime config.RuntimeConfig
	passx   config.PassXConfig
}

func NewSSNative(runtime config.RuntimeConfig, passx config.PassXConfig, logger *slog.Logger) (*SSNative, error) {
	return &SSNative{
		logger:  logger.With(slog.String("runtime", "ss-native")),
		service: ss.NewService(logger),
		runtime: runtime,
		passx:   passx,
	}, nil
}

func (s *SSNative) Name() string {
	return "ss-native"
}

func (s *SSNative) Apply(_ context.Context, plan model.RuntimePlan) error {
	cfg, err := ss.Translate(plan.Node, plan.Users, ss.Options{
		DefaultTCPConnLimit: s.runtime.DefaultTCPConnLimit,
		EnforceDeviceLimit:  s.runtime.DeviceLimitEnabled(),
		AllowTargets:        append([]string(nil), s.runtime.AllowTargets...),
		BlockTargets:        append([]string(nil), s.runtime.BlockTargets...),
		PassX: ss.PassXConfig{
			Enabled:      s.passx.Enabled,
			TrustedCIDRs: append([]string(nil), s.passx.TrustedCIDRs...),
		},
	})
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
