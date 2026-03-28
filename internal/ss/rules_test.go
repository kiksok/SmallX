package ss

import (
	"testing"

	"smallx/internal/model"
)

func TestTargetRulesBlockByXboardRoute(t *testing.T) {
	rules, err := compileTargetRules(RuntimeConfig{
		Server: ServerConfig{
			Routes: []model.RouteRule{
				{ID: 1, Action: "block", Match: []string{"example.com"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("compileTargetRules returned error: %v", err)
	}
	if err := rules.Validate("sub.example.com:443"); err == nil {
		t.Fatalf("expected target to be blocked")
	}
}

func TestTargetRulesAllowlist(t *testing.T) {
	rules, err := compileTargetRules(RuntimeConfig{
		Server: ServerConfig{
			AllowTargets: []string{"allowed.example.com"},
		},
	})
	if err != nil {
		t.Fatalf("compileTargetRules returned error: %v", err)
	}
	if err := rules.Validate("denied.example.com:443"); err == nil {
		t.Fatalf("expected allowlist rejection")
	}
	if err := rules.Validate("allowed.example.com:443"); err != nil {
		t.Fatalf("expected allowlist match, got: %v", err)
	}
}
