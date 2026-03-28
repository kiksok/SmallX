package ss

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"liteone/internal/model"
)

type RuntimeConfig struct {
	Server ServerConfig
	Users  []UserConfig
}

type ServerConfig struct {
	ListenIP   string
	ServerPort int
	Cipher     string
	ServerKey  string
	EnableTCP  bool
	EnableUDP  bool
	Obfs       ObfsConfig
}

type UserConfig struct {
	ID       int
	UUID     string
	Method   string
	Password string
}

type ObfsConfig struct {
	Enabled bool
	Mode    string
	Host    string
	Path    string
}

type CipherSpec struct {
	Name        string
	Is2022      bool
	UserKeySize int
}

var supportedCiphers = map[string]CipherSpec{
	"chacha20-ietf-poly1305":   {Name: "chacha20-ietf-poly1305"},
	"aes-128-gcm":              {Name: "aes-128-gcm"},
	"aes-192-gcm":              {Name: "aes-192-gcm"},
	"aes-256-gcm":              {Name: "aes-256-gcm"},
	"2022-blake3-aes-128-gcm":  {Name: "2022-blake3-aes-128-gcm", Is2022: true, UserKeySize: 16},
	"2022-blake3-aes-256-gcm":  {Name: "2022-blake3-aes-256-gcm", Is2022: true, UserKeySize: 32},
}

func Translate(node model.NodeConfig, users []model.UserInfo) (RuntimeConfig, error) {
	if strings.ToLower(node.Protocol) != "shadowsocks" {
		return RuntimeConfig{}, fmt.Errorf("unsupported protocol for ss runtime: %s", node.Protocol)
	}

	cipher := strings.ToLower(strings.TrimSpace(node.Cipher))
	spec, ok := supportedCiphers[cipher]
	if !ok {
		return RuntimeConfig{}, fmt.Errorf("unsupported shadowsocks cipher: %s", node.Cipher)
	}
	if node.ServerPort <= 0 {
		return RuntimeConfig{}, errors.New("server port must be greater than 0")
	}
	if spec.Is2022 && strings.TrimSpace(node.ServerKey) == "" {
		return RuntimeConfig{}, errors.New("shadowsocks 2022 requires server_key")
	}

	obfs, err := parseObfs(node.Plugin, node.PluginOpts)
	if err != nil {
		return RuntimeConfig{}, err
	}

	cfg := RuntimeConfig{
		Server: ServerConfig{
			ListenIP:   defaultListenIP(node.ListenIP),
			ServerPort: node.ServerPort,
			Cipher:     spec.Name,
			ServerKey:  node.ServerKey,
			EnableTCP:  true,
			EnableUDP:  true,
			Obfs:       obfs,
		},
		Users: make([]UserConfig, 0, len(users)),
	}

	for _, user := range users {
		pass, err := derivePassword(spec, user.UUID)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("derive password for user %d: %w", user.ID, err)
		}
		cfg.Users = append(cfg.Users, UserConfig{
			ID:       user.ID,
			UUID:     user.UUID,
			Method:   spec.Name,
			Password: pass,
		})
	}

	return cfg, nil
}

func SupportedCiphers() []string {
	return []string{
		"chacha20-ietf-poly1305",
		"aes-128-gcm",
		"aes-192-gcm",
		"aes-256-gcm",
		"2022-blake3-aes-128-gcm",
		"2022-blake3-aes-256-gcm",
	}
}

func derivePassword(spec CipherSpec, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty user secret")
	}

	if !spec.Is2022 {
		return raw, nil
	}

	// Future-proofing:
	// if a panel ever returns "serverKey:userKey", use the user-key part directly.
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	if len(raw) < spec.UserKeySize {
		return "", fmt.Errorf("user secret too short for %s", spec.Name)
	}

	return base64.StdEncoding.EncodeToString([]byte(raw[:spec.UserKeySize])), nil
}

func parseObfs(plugin, pluginOpts string) (ObfsConfig, error) {
	plugin = strings.ToLower(strings.TrimSpace(plugin))
	pluginOpts = strings.TrimSpace(pluginOpts)
	if plugin == "" || pluginOpts == "" {
		return ObfsConfig{}, nil
	}

	if plugin != "obfs" && plugin != "obfs-local" {
		return ObfsConfig{}, fmt.Errorf("unsupported ss plugin: %s", plugin)
	}

	kv := parsePluginOpts(pluginOpts)
	mode := strings.ToLower(strings.TrimSpace(kv["obfs"]))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(kv["mode"]))
	}
	if mode == "" {
		return ObfsConfig{}, errors.New("obfs plugin is missing mode")
	}
	if mode != "http" {
		return ObfsConfig{}, fmt.Errorf("unsupported obfs mode for mvp: %s", mode)
	}

	path := kv["path"]
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return ObfsConfig{
		Enabled: true,
		Mode:    mode,
		Host:    kv["obfs-host"],
		Path:    path,
	}, nil
}

func parsePluginOpts(raw string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			out[part] = ""
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func defaultListenIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0.0.0.0"
	}
	return value
}
