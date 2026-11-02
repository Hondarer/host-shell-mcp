package clientproxy

import (
	"fmt"
	"log"

	"github.com/hondarer/host-shell-mcp/internal/policy"
)

// Rules is a snapshot of session-local proxy destination rules.
type Rules struct {
	Revision   uint64   `json:"rules_revision"`
	AllowHosts []string `json:"allow_hosts"`
	DenyHosts  []string `json:"deny_hosts"`
}

// UpdateRulesInput replaces both lists, conditional on the current revision.
type UpdateRulesInput struct {
	ExpectedRevision uint64   `json:"expected_revision" jsonschema:"rules_revision from proxy_status; stale revisions are rejected"`
	AllowHosts       []string `json:"allow_hosts" jsonschema:"complete replacement allowlist; empty denies all new destinations"`
	DenyHosts        []string `json:"deny_hosts" jsonschema:"complete replacement denylist; deny takes precedence over allow"`
}

type ruleSet struct {
	revision uint64
	allow    policy.NameRules
	deny     policy.NameRules
}

func parseRules(allow, deny []string) (*ruleSet, error) {
	a, err := policy.ParseNameRules("--proxy-allow-host", allow)
	if err != nil {
		return nil, err
	}
	d, err := policy.ParseNameRules("--proxy-deny-host", deny)
	if err != nil {
		return nil, err
	}
	return &ruleSet{allow: a, deny: d}, nil
}

func (r *ruleSet) snapshot() Rules {
	return Rules{Revision: r.revision, AllowHosts: append([]string{}, r.allow...), DenyHosts: append([]string{}, r.deny...)}
}

// UpdateRules validates before atomically replacing the rules for new streams.
// Existing streams remain open. The bridge decides whether to expose this method over MCP.
func (g *Gateway) UpdateRules(in UpdateRulesInput) (Rules, error) {
	next, err := parseRules(in.AllowHosts, in.DenyHosts)
	if err != nil {
		return Rules{}, err
	}
	current := g.rules.Load()
	if current.revision != in.ExpectedRevision {
		return Rules{}, fmt.Errorf("proxy rules changed: expected revision %d, current revision %d; read proxy_status before retrying", in.ExpectedRevision, current.revision)
	}
	next.revision = current.revision + 1
	if !g.rules.CompareAndSwap(current, next) {
		return Rules{}, fmt.Errorf("proxy rules changed concurrently; read proxy_status before retrying")
	}
	log.Printf("proxy rules updated: revision=%d allow_hosts=%q deny_hosts=%q", next.revision, next.allow, next.deny)
	return next.snapshot(), nil
}
