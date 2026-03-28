package xboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"smallx/internal/config"
	"smallx/internal/model"
)

type Client struct {
	cfg    config.PanelConfig
	http   *http.Client
	logger *slog.Logger

	mu    sync.Mutex
	etags map[string]string
}

func New(cfg config.PanelConfig, logger *slog.Logger) *Client {
	return &Client{
		cfg:    cfg,
		http:   &http.Client{Timeout: cfg.TimeoutDuration()},
		logger: logger.With(slog.String("provider", "xboard")),
		etags:  make(map[string]string),
	}
}

func (c *Client) Name() string {
	return "xboard"
}

func (c *Client) FetchNode(ctx context.Context) (model.NodeConfig, bool, error) {
	var node model.NodeConfig
	changed, err := c.getJSON(ctx, "/api/v1/server/UniProxy/config", "node", &node)
	return node, changed, err
}

func (c *Client) FetchUsers(ctx context.Context) ([]model.UserInfo, bool, error) {
	var payload struct {
		Users []model.UserInfo `json:"users"`
	}
	changed, err := c.getJSON(ctx, "/api/v1/server/UniProxy/user", "users", &payload)
	return payload.Users, changed, err
}

func (c *Client) FetchRules(ctx context.Context) ([]model.AuditRule, bool, error) {
	var rules []model.AuditRule
	changed, err := c.getJSON(ctx, "/api/v1/server/UniProxy/rules", "rules", &rules)
	if err != nil {
		if isNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return rules, changed, nil
}

func (c *Client) ReportTraffic(ctx context.Context, traffic []model.TrafficReport) error {
	if len(traffic) == 0 {
		return nil
	}
	data := make(map[int][]int64, len(traffic))
	for _, item := range traffic {
		data[item.ID] = []int64{item.U, item.D}
	}
	return c.postJSON(ctx, "/api/v1/server/UniProxy/push", data)
}

func (c *Client) ReportAliveIPs(ctx context.Context, alive []model.AliveIP) error {
	if len(alive) == 0 {
		return nil
	}
	data := make(map[int][]string, len(alive))
	for _, item := range alive {
		data[item.ID] = item.IPs
	}
	return c.postJSON(ctx, "/api/v1/server/UniProxy/alive", data)
}

func (c *Client) ReportStatus(ctx context.Context, status model.StatusReport) error {
	return c.postJSON(ctx, "/api/v1/server/UniProxy/status", status)
}

func (c *Client) ReportAudits(ctx context.Context, audits []model.AuditLog) error {
	if len(audits) == 0 {
		return nil
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path, cacheKey string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.makeURL(path), nil)
	if err != nil {
		return false, err
	}
	c.addQuery(req)
	if etag := c.getETag(cacheKey); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return false, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("not found: %s", path)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("GET %s failed: %s", path, strings.TrimSpace(string(body)))
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		c.setETag(cacheKey, etag)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, fmt.Errorf("decode %s: %w", path, err)
	}

	return true, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.makeURL(path), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.addQuery(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s failed: %s", path, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) makeURL(path string) string {
	return c.cfg.BaseURL + path
}

func (c *Client) addQuery(req *http.Request) {
	q := req.URL.Query()
	q.Set("token", c.cfg.Token)
	q.Set("node_id", fmt.Sprintf("%d", c.cfg.NodeID))
	q.Set("node_type", c.cfg.NodeType)
	req.URL.RawQuery = q.Encode()
}

func (c *Client) getETag(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.etags[key]
}

func (c *Client) setETag(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.etags[key] = value
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

var _ interface {
	Name() string
} = (*Client)(nil)

func init() {
	_ = url.Values{}
}
