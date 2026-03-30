package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Panel   PanelConfig   `yaml:"panel"`
	Sync    SyncConfig    `yaml:"sync"`
	Runtime RuntimeConfig `yaml:"runtime"`
	PassX   PassXConfig   `yaml:"passx"`
	Log     LogConfig     `yaml:"log"`
}

type PanelConfig struct {
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	Token    string `yaml:"token"`
	NodeID   int    `yaml:"node_id"`
	NodeType string `yaml:"node_type"`
	Timeout  string `yaml:"timeout"`
}

type SyncConfig struct {
	PullInterval   string `yaml:"pull_interval"`
	StatusInterval string `yaml:"status_interval"`
}

type RuntimeConfig struct {
	Adapter             string   `yaml:"adapter"`
	WorkDir             string   `yaml:"work_dir"`
	ApplyTimeout        string   `yaml:"apply_timeout"`
	DefaultTCPConnLimit int      `yaml:"default_tcp_conn_limit"`
	EnforceDeviceLimit  *bool    `yaml:"enforce_device_limit"`
	AllowTargets        []string `yaml:"allow_targets"`
	BlockTargets        []string `yaml:"block_targets"`
}

type PassXConfig struct {
	Enabled      bool     `yaml:"enabled"`
	TrustedCIDRs []string `yaml:"trusted_cidrs"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	cfg.Panel.Provider = strings.TrimSpace(strings.ToLower(cfg.Panel.Provider))
	cfg.Panel.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Panel.BaseURL), "/")
	cfg.Panel.NodeType = normalizeNodeType(cfg.Panel.NodeType)
	cfg.Panel.Timeout = defaultIfEmpty(cfg.Panel.Timeout, "10s")

	cfg.Sync.PullInterval = defaultIfEmpty(cfg.Sync.PullInterval, "60s")
	cfg.Sync.StatusInterval = defaultIfEmpty(cfg.Sync.StatusInterval, "60s")

	cfg.Runtime.Adapter = strings.TrimSpace(strings.ToLower(cfg.Runtime.Adapter))
	cfg.Runtime.WorkDir = defaultIfEmpty(cfg.Runtime.WorkDir, "./run")
	cfg.Runtime.ApplyTimeout = defaultIfEmpty(cfg.Runtime.ApplyTimeout, "15s")
	if cfg.Runtime.EnforceDeviceLimit == nil {
		value := true
		cfg.Runtime.EnforceDeviceLimit = &value
	}

	cfg.Log.Level = strings.TrimSpace(strings.ToLower(defaultIfEmpty(cfg.Log.Level, "info")))
}

func validate(cfg *Config) error {
	switch {
	case cfg.Panel.Provider == "":
		return errors.New("panel.provider is required")
	case cfg.Panel.BaseURL == "":
		return errors.New("panel.base_url is required")
	case cfg.Panel.Token == "":
		return errors.New("panel.token is required")
	case cfg.Panel.NodeID <= 0:
		return errors.New("panel.node_id must be > 0")
	case cfg.Panel.NodeType == "":
		return errors.New("panel.node_type is required")
	case cfg.Runtime.Adapter == "":
		return errors.New("runtime.adapter is required")
	}

	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "panel.timeout", value: cfg.Panel.Timeout},
		{name: "sync.pull_interval", value: cfg.Sync.PullInterval},
		{name: "sync.status_interval", value: cfg.Sync.StatusInterval},
		{name: "runtime.apply_timeout", value: cfg.Runtime.ApplyTimeout},
	} {
		if _, err := time.ParseDuration(item.value); err != nil {
			return fmt.Errorf("%s is invalid: %w", item.name, err)
		}
	}

	return nil
}

func (c PanelConfig) TimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.Timeout)
	return d
}

func (c SyncConfig) PullEvery() time.Duration {
	d, _ := time.ParseDuration(c.PullInterval)
	return d
}

func (c SyncConfig) StatusEvery() time.Duration {
	d, _ := time.ParseDuration(c.StatusInterval)
	return d
}

func (c RuntimeConfig) ApplyTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.ApplyTimeout)
	return d
}

func (c RuntimeConfig) DeviceLimitEnabled() bool {
	return c.EnforceDeviceLimit != nil && *c.EnforceDeviceLimit
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeNodeType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "v2ray":
		return "vmess"
	case "hysteria2":
		return "hysteria"
	default:
		return value
	}
}

func ErrUnsupportedProvider(name string) error {
	return fmt.Errorf("unsupported provider: %s", name)
}

func ErrUnsupportedRuntime(name string) error {
	return fmt.Errorf("unsupported runtime adapter: %s", name)
}
