package ss

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"smallx/internal/model"
)

type targetRules struct {
	allows []compiledRule
	blocks []compiledRule
}

type compiledRule struct {
	id      int
	source  string
	pattern *regexp.Regexp
}

func compileTargetRules(cfg RuntimeConfig) (*targetRules, error) {
	rules := &targetRules{}

	for _, item := range cfg.Server.AllowTargets {
		compiled, err := compileRule(-1, "local-allow", item)
		if err != nil {
			return nil, err
		}
		rules.allows = append(rules.allows, compiled)
	}
	for _, item := range cfg.Server.BlockTargets {
		compiled, err := compileRule(-1, "local-block", item)
		if err != nil {
			return nil, err
		}
		rules.blocks = append(rules.blocks, compiled)
	}

	for _, route := range cfg.Server.Routes {
		switch strings.ToLower(strings.TrimSpace(route.Action)) {
		case "block":
			for _, match := range route.Match {
				compiled, err := compileRule(route.ID, "xboard-block", match)
				if err != nil {
					return nil, err
				}
				rules.blocks = append(rules.blocks, compiled)
			}
		}
	}

	return rules, nil
}

func (r *targetRules) Validate(target string) error {
	if r == nil {
		return nil
	}
	candidates := targetCandidates(target)

	for _, rule := range r.blocks {
		if rule.matchesAny(candidates) {
			return fmt.Errorf("target blocked by %s rule %d", rule.source, rule.id)
		}
	}

	if len(r.allows) > 0 {
		for _, rule := range r.allows {
			if rule.matchesAny(candidates) {
				return nil
			}
		}
		return fmt.Errorf("target is not in allowlist")
	}

	return nil
}

func (r compiledRule) matchesAny(values []string) bool {
	for _, value := range values {
		if r.pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func compileRule(id int, source, raw string) (compiledRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return compiledRule{}, fmt.Errorf("empty rule pattern")
	}

	patternText := raw
	if !looksRegex(raw) {
		if strings.Contains(raw, ":") || net.ParseIP(raw) != nil {
			patternText = "^" + regexp.QuoteMeta(raw) + "$"
		} else {
			patternText = `(^|\.)` + regexp.QuoteMeta(raw) + `$`
		}
	}

	pattern, err := regexp.Compile(patternText)
	if err != nil {
		return compiledRule{}, fmt.Errorf("compile rule %q: %w", raw, err)
	}

	return compiledRule{
		id:      id,
		source:  source,
		pattern: pattern,
	}, nil
}

func targetCandidates(target string) []string {
	values := []string{target}
	host, _, err := net.SplitHostPort(target)
	if err == nil {
		values = append(values, host)
	}
	return values
}

func looksRegex(value string) bool {
	return strings.ContainsAny(value, `\^$*+?()[]{}|`)
}

func routeIDs(routes []model.RouteRule) []int {
	out := make([]int, 0, len(routes))
	for _, item := range routes {
		out = append(out, item.ID)
	}
	return out
}
