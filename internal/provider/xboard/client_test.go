package xboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
	"smallx/internal/config"
)

const usersAcceptHeader = "application/msgpack, application/x-msgpack, application/json"

func TestFetchNodeParsesRoutesFromConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/UniProxy/config" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token"); got != "panel-token" {
			t.Fatalf("unexpected token query: %s", got)
		}
		if got := r.URL.Query().Get("node_id"); got != "1" {
			t.Fatalf("unexpected node_id query: %s", got)
		}
		if got := r.URL.Query().Get("node_type"); got != "shadowsocks" {
			t.Fatalf("unexpected node_type query: %s", got)
		}

		w.Header().Set("ETag", `"config-v1"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"protocol":    "shadowsocks",
			"listen_ip":   "0.0.0.0",
			"server_port": 26139,
			"cipher":      "aes-256-gcm",
			"routes": []map[string]any{
				{
					"id":     1,
					"match":  []string{"ads.example.com"},
					"action": "block",
				},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	node, changed, err := client.FetchNode(context.Background())
	if err != nil {
		t.Fatalf("FetchNode returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected config fetch to report changed")
	}
	if node.Protocol != "shadowsocks" {
		t.Fatalf("unexpected protocol: %s", node.Protocol)
	}
	if len(node.Routes) != 1 {
		t.Fatalf("expected one route from config payload, got %d", len(node.Routes))
	}
	if node.Routes[0].Action != "block" {
		t.Fatalf("unexpected route action: %s", node.Routes[0].Action)
	}
}

func TestFetchUsersMsgpack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertUsersRequest(t, r)
		w.Header().Set("Content-Type", "application/msgpack")
		w.Header().Set("ETag", `"users-v1"`)

		var buf bytes.Buffer
		encoder := msgpack.NewEncoder(&buf)
		encoder.SetCustomStructTag("json")
		if err := encoder.Encode(map[string]any{
			"users": []map[string]any{
				{
					"id":           1,
					"uuid":         "user-uuid-1",
					"speed_limit":  10,
					"device_limit": 2,
				},
			},
		}); err != nil {
			t.Fatalf("msgpack encode failed: %v", err)
		}

		if _, err := w.Write(buf.Bytes()); err != nil {
			t.Fatalf("response write failed: %v", err)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	users, changed, err := client.FetchUsers(context.Background())
	if err != nil {
		t.Fatalf("FetchUsers returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected users fetch to report changed")
	}
	if len(users) != 1 || users[0].UUID != "user-uuid-1" {
		t.Fatalf("unexpected users payload: %+v", users)
	}
}

func TestFetchUsersJSONFallback(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "json", contentType: "application/json; charset=utf-8"},
		{name: "empty", contentType: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertUsersRequest(t, r)
				if tt.contentType != "" {
					w.Header().Set("Content-Type", tt.contentType)
				} else {
					w.Header()["Content-Type"] = []string{""}
				}
				payload, err := json.Marshal(map[string]any{
					"users": []map[string]any{
						{
							"id":           1,
							"uuid":         "json-user",
							"speed_limit":  20,
							"device_limit": 3,
						},
					},
				})
				if err != nil {
					t.Fatalf("json marshal failed: %v", err)
				}
				if _, err := w.Write(payload); err != nil {
					t.Fatalf("response write failed: %v", err)
				}
			}))
			defer server.Close()

			client := newTestClient(server.URL)

			users, changed, err := client.FetchUsers(context.Background())
			if err != nil {
				t.Fatalf("FetchUsers returned error: %v", err)
			}
			if !changed {
				t.Fatalf("expected users fetch to report changed")
			}
			if len(users) != 1 || users[0].UUID != "json-user" {
				t.Fatalf("unexpected users payload: %+v", users)
			}
		})
	}
}

func TestFetchUsersRejectsUnknownContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertUsersRequest(t, r)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain-text"))
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	_, _, err := client.FetchUsers(context.Background())
	if err == nil {
		t.Fatalf("expected unknown content-type to fail")
	}
	if !strings.Contains(err.Error(), "unsupported content-type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchUsersNotModifiedSupportsJSONAndMsgpack(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		writer      func(t *testing.T, w http.ResponseWriter)
	}{
		{
			name:        "json",
			contentType: "application/json",
			writer: func(t *testing.T, w http.ResponseWriter) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"users": []map[string]any{
						{"id": 1, "uuid": "json-user"},
					},
				})
			},
		},
		{
			name:        "msgpack",
			contentType: "application/x-msgpack; charset=binary",
			writer: func(t *testing.T, w http.ResponseWriter) {
				var buf bytes.Buffer
				encoder := msgpack.NewEncoder(&buf)
				encoder.SetCustomStructTag("json")
				if err := encoder.Encode(map[string]any{
					"users": []map[string]any{
						{"id": 1, "uuid": "msgpack-user"},
					},
				}); err != nil {
					t.Fatalf("msgpack encode failed: %v", err)
				}
				if _, err := w.Write(buf.Bytes()); err != nil {
					t.Fatalf("response write failed: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			requests := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertUsersRequest(t, r)

				mu.Lock()
				requests++
				currentRequest := requests
				mu.Unlock()

				if currentRequest == 1 {
					w.Header().Set("Content-Type", tt.contentType)
					w.Header().Set("ETag", `"users-v1"`)
					tt.writer(t, w)
					return
				}

				if got := r.Header.Get("If-None-Match"); got != `"users-v1"` {
					t.Fatalf("unexpected If-None-Match header: %s", got)
				}
				w.WriteHeader(http.StatusNotModified)
			}))
			defer server.Close()

			client := newTestClient(server.URL)

			users, changed, err := client.FetchUsers(context.Background())
			if err != nil {
				t.Fatalf("first FetchUsers returned error: %v", err)
			}
			if !changed || len(users) != 1 {
				t.Fatalf("unexpected first fetch result: changed=%v users=%+v", changed, users)
			}

			users, changed, err = client.FetchUsers(context.Background())
			if err != nil {
				t.Fatalf("second FetchUsers returned error: %v", err)
			}
			if changed {
				t.Fatalf("expected second fetch to report unchanged")
			}
			if len(users) != 0 {
				t.Fatalf("expected no users on not modified response, got %+v", users)
			}

			mu.Lock()
			defer mu.Unlock()
			if requests != 2 {
				t.Fatalf("expected exactly 2 requests, got %d", requests)
			}
		})
	}
}

func TestFetchRulesIsNoOpForOfficialXboard(t *testing.T) {
	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server.URL)

	rules, changed, err := client.FetchRules(context.Background())
	if err != nil {
		t.Fatalf("FetchRules returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected no-op rules fetch to report unchanged")
	}
	if len(rules) != 0 {
		t.Fatalf("expected no rules from no-op fetch, got %d", len(rules))
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 0 {
		t.Fatalf("expected FetchRules no-op to avoid network calls, got %d request(s)", requests)
	}
}

func newTestClient(baseURL string) *Client {
	return New(config.PanelConfig{
		BaseURL:  baseURL,
		Token:    "panel-token",
		NodeID:   1,
		NodeType: "shadowsocks",
		Timeout:  "5s",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func assertUsersRequest(t *testing.T, r *http.Request) {
	t.Helper()

	if r.URL.Path != "/api/v1/server/UniProxy/user" {
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}
	if got := r.URL.Query().Get("token"); got != "panel-token" {
		t.Fatalf("unexpected token query: %s", got)
	}
	if got := r.URL.Query().Get("node_id"); got != "1" {
		t.Fatalf("unexpected node_id query: %s", got)
	}
	if got := r.URL.Query().Get("node_type"); got != "shadowsocks" {
		t.Fatalf("unexpected node_type query: %s", got)
	}
	if got := r.Header.Get("Accept"); got != usersAcceptHeader {
		t.Fatalf("unexpected Accept header: %s", got)
	}
}
