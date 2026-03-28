package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"smallx/internal/agent"
	"smallx/internal/backend"
	"smallx/internal/buildinfo"
	"smallx/internal/config"
	"smallx/internal/provider"
	"smallx/internal/provider/xboard"
)

func main() {
	configPath := flag.String("config", "./config.example.yaml", "path to config file")
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *printVersion {
		if buildinfo.Commit != "" {
			println(buildinfo.Version + " (" + buildinfo.Commit + ")")
			return
		}
		println(buildinfo.Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		panic(err)
	}

	logger := newLogger(cfg.Log.Level)
	logger.Info("starting smallx",
		slog.String("version", buildinfo.Version),
		slog.String("commit", buildinfo.Commit),
		slog.String("provider", cfg.Panel.Provider),
		slog.String("runtime", cfg.Runtime.Adapter),
		slog.Int("node_id", cfg.Panel.NodeID),
		slog.String("node_type", cfg.Panel.NodeType),
	)

	p, err := buildProvider(cfg, logger)
	if err != nil {
		panic(err)
	}

	r, err := buildRuntime(cfg, logger)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = r.Close()
	}()

	a := agent.New(cfg, logger, p, r)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil {
		logger.Error("agent stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func buildProvider(cfg *config.Config, logger *slog.Logger) (provider.Provider, error) {
	switch cfg.Panel.Provider {
	case "xboard":
		return xboard.New(cfg.Panel, logger), nil
	default:
		return nil, config.ErrUnsupportedProvider(cfg.Panel.Provider)
	}
}

func buildRuntime(cfg *config.Config, logger *slog.Logger) (backend.Runtime, error) {
	switch cfg.Runtime.Adapter {
	case "dry-run":
		return backend.NewDryRun(logger)
	case "ss-native":
		return backend.NewSSNative(cfg.Runtime, logger)
	case "ss-prototype":
		return backend.NewSSPrototype(logger)
	default:
		return nil, config.ErrUnsupportedRuntime(cfg.Runtime.Adapter)
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
