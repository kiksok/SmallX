package ss

import (
	"testing"

	"smallx/internal/model"
)

func TestTranslateAEADNode(t *testing.T) {
	cfg, err := Translate(model.NodeConfig{
		Protocol:   "shadowsocks",
		ListenIP:   "0.0.0.0",
		ServerPort: 26139,
		Cipher:     "aes-256-gcm",
	}, []model.UserInfo{
		{ID: 1, UUID: "abc-uuid-1"},
	})
	if err != nil {
		t.Fatalf("Translate returned error: %v", err)
	}

	if cfg.Server.Cipher != "aes-256-gcm" {
		t.Fatalf("unexpected cipher: %s", cfg.Server.Cipher)
	}
	if !cfg.Server.EnableTCP || !cfg.Server.EnableUDP {
		t.Fatalf("expected tcp+udp enabled")
	}
	if got := cfg.Users[0].Password; got != "abc-uuid-1" {
		t.Fatalf("unexpected user password: %s", got)
	}
}

func TestTranslate2022Node(t *testing.T) {
	cfg, err := Translate(model.NodeConfig{
		Protocol:   "shadowsocks",
		ListenIP:   "0.0.0.0",
		ServerPort: 10000,
		Cipher:     "2022-blake3-aes-128-gcm",
		ServerKey:  "c2VydmVyLWtleS0xMjM0NQ==",
	}, []model.UserInfo{
		{ID: 1, UUID: "1234567890abcdef-hello-world"},
	})
	if err != nil {
		t.Fatalf("Translate returned error: %v", err)
	}

	if got := cfg.Users[0].Password; got != "MTIzNDU2Nzg5MGFiY2RlZg==" {
		t.Fatalf("unexpected 2022 user password: %s", got)
	}
}

func TestParseObfsHTTP(t *testing.T) {
	cfg, err := Translate(model.NodeConfig{
		Protocol:   "shadowsocks",
		ListenIP:   "0.0.0.0",
		ServerPort: 10000,
		Cipher:     "aes-128-gcm",
		Plugin:     "obfs",
		PluginOpts: "obfs=http;obfs-host=cdn.example.com;path=/video",
	}, nil)
	if err != nil {
		t.Fatalf("Translate returned error: %v", err)
	}

	if !cfg.Server.Obfs.Enabled {
		t.Fatalf("expected obfs enabled")
	}
	if cfg.Server.Obfs.Mode != "http" {
		t.Fatalf("unexpected obfs mode: %s", cfg.Server.Obfs.Mode)
	}
	if cfg.Server.Obfs.Host != "cdn.example.com" {
		t.Fatalf("unexpected obfs host: %s", cfg.Server.Obfs.Host)
	}
	if cfg.Server.Obfs.Path != "/video" {
		t.Fatalf("unexpected obfs path: %s", cfg.Server.Obfs.Path)
	}
}
