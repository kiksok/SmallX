package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type NodeConfig struct {
	Protocol        string          `json:"protocol"`
	ListenIP        string          `json:"listen_ip,omitempty"`
	ServerPort      int             `json:"server_port"`
	Network         string          `json:"network,omitempty"`
	NetworkSettings json.RawMessage `json:"networkSettings,omitempty"`
	TLS             int             `json:"tls,omitempty"`
	TLSSettings     json.RawMessage `json:"tls_settings,omitempty"`
	Multiplex       json.RawMessage `json:"multiplex,omitempty"`
	Flow            string          `json:"flow,omitempty"`
	Host            string          `json:"host,omitempty"`
	ServerName      string          `json:"server_name,omitempty"`
	Cipher          string          `json:"cipher,omitempty"`
	ServerKey       string          `json:"server_key,omitempty"`
	Plugin          string          `json:"plugin,omitempty"`
	PluginOpts      string          `json:"plugin_opts,omitempty"`
	Routes          []RouteRule     `json:"routes,omitempty"`
}

type RouteRule struct {
	ID          int      `json:"id"`
	Match       []string `json:"match"`
	Action      string   `json:"action"`
	ActionValue string   `json:"action_value"`
}

type UserInfo struct {
	ID          int    `json:"id"`
	UUID        string `json:"uuid,omitempty"`
	Password    string `json:"password,omitempty"`
	SpeedLimit  int    `json:"speed_limit,omitempty"`
	DeviceLimit int    `json:"device_limit,omitempty"`
	TCPLimit    int    `json:"tcp_limit,omitempty"`
}

type AuditRule struct {
	ID   int    `json:"id"`
	Rule string `json:"rule"`
}

type TrafficReport struct {
	ID int   `json:"id"`
	U  int64 `json:"u"`
	D  int64 `json:"d"`
}

type AliveIP struct {
	ID  int      `json:"id"`
	IPs []string `json:"ips"`
}

type AuditLog struct {
	UserID  int `json:"user_id"`
	AuditID int `json:"audit_id"`
}

type StatusReport struct {
	CPU    float64       `json:"cpu"`
	Mem    ResourceUsage `json:"mem"`
	Swap   ResourceUsage `json:"swap"`
	Disk   ResourceUsage `json:"disk"`
	Uptime int64         `json:"uptime"`
}

type ResourceUsage struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
}

type RuntimePlan struct {
	Node  NodeConfig
	Users []UserInfo
	Rules []AuditRule
}

type RuntimeSnapshot struct {
	Status   StatusReport
	Traffic  []TrafficReport
	AliveIPs []AliveIP
	Audits   []AuditLog
}

func (n NodeConfig) Summary() string {
	var parts []string
	if n.Protocol != "" {
		parts = append(parts, strings.ToLower(n.Protocol))
	}
	if n.Network != "" {
		parts = append(parts, n.Network)
	}
	if n.ServerPort > 0 {
		parts = append(parts, fmt.Sprintf(":%d", n.ServerPort))
	}
	if n.TLS > 0 {
		parts = append(parts, fmt.Sprintf("tls=%d", n.TLS))
	}
	return strings.Join(parts, " ")
}
