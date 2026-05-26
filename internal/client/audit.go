// SPDX-License-Identifier: MIT
// Package client — audit.go (Plan 4 Phase N Task N-5).
//
// Typed wrappers for the audit surface:
//
//	POST /v1/audit/emit       — Phase G; per-event write (operator emit/test only)
//	GET  /v1/audit/events     — Phase N; recent events filtered by type/project
//	GET  /v1/audit/types      — Phase N; distinct types catalog (last 30d)
//
// Family-disjoint reviewer pool (inv-zen-080) and criteria templates are
// surfaced via doctrine state (audit.families, audit.criteria.*); the CLI
// reads them through DoctrineState rather than dedicated audit endpoints.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type AuditEmitReq struct {
	ProjectID string `json:"project_id,omitempty"`
	Type      string `json:"type"`
	Payload   any    `json:"payload"`
}

type AuditEmitResp struct {
	ID        string `json:"id"`
	Accepted  bool   `json:"accepted"`
	EmittedAt int64  `json:"emitted_at"`
}

type AuditEvent struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Type       string `json:"type"`
	Doctrine   string `json:"doctrine,omitempty"`
	PayloadRaw string `json:"payload_json"`
	EmittedAt  int64  `json:"emitted_at"`
}

type AuditType struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type AuditFamily struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Default     bool   `json:"default" yaml:"default"`
}

type AuditCriterion struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Source      string `json:"source" yaml:"source"`
}

func (c *Client) AuditEmit(ctx context.Context, req AuditEmitReq) (*AuditEmitResp, error) {
	var out AuditEmitResp
	if err := c.postJSON(ctx, "/v1/audit/emit", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AuditEvents(ctx context.Context, typePrefix, projectID string, sinceUnix int64, limit int) ([]AuditEvent, error) {
	q := url.Values{}
	if typePrefix != "" {
		q.Set("type", typePrefix)
	}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if sinceUnix > 0 {
		q.Set("since", strconv.FormatInt(sinceUnix, 10))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Items []AuditEvent `json:"items"`
		Count int          `json:"count"`
	}
	path := "/v1/audit/events"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) AuditTypes(ctx context.Context) ([]AuditType, error) {
	var out struct {
		Items []AuditType `json:"items"`
		Count int         `json:"count"`
	}
	if err := c.getJSON(ctx, "/v1/audit/types", &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// familyDescriptions maps a family name to its operator-visible
// description. Used by AuditFamiliesFromPool to enrich the doctrine-
// resolved name list. New families MUST be registered here so they
// surface in `zen audit families show` with a meaningful description.
var familyDescriptions = map[string]string{
	"anthropic":  "Claude (Opus, Sonnet, Haiku) via Anthropic API.",
	"google":     "Gemini (Pro, Flash) via Google AI / Vertex AI.",
	"deepseek":   "DeepSeek (R1, V3) via DeepSeek API or compatible endpoint.",
	"local-qwen": "Local Qwen via Ollama (privacy-locked, no third-party egress).",
	"openai":     "OpenAI (GPT-4, GPT-4o) — reserved for future provider integration.",
}

func AuditFamilies() []AuditFamily {

	defaults := map[string]bool{"anthropic": true, "google": true}
	names := []string{"anthropic", "google", "deepseek", "local-qwen"}
	out := make([]AuditFamily, 0, len(names))
	for _, n := range names {
		out = append(out, AuditFamily{
			Name:        n,
			Description: familyDescriptions[n],
			Default:     defaults[n],
		})
	}
	return out
}

func AuditFamiliesFromPool(pool []string) []AuditFamily {
	out := make([]AuditFamily, 0, len(pool))
	for _, n := range pool {
		desc := familyDescriptions[n]
		if desc == "" {
			desc = fmt.Sprintf("%s reviewer family", n)
		}
		out = append(out, AuditFamily{
			Name:        n,
			Description: desc,

			Default: true,
		})
	}
	return out
}

func (c *Client) AuditFamiliesResolve(ctx context.Context) ([]AuditFamily, error) {
	state, err := c.DoctrineStateCall(ctx)
	if err != nil {

		return AuditFamilies(), nil
	}
	fams, _ := c.AuditFamiliesResolveFromState(state)
	if len(fams) > 0 {
		return fams, nil
	}

	return AuditFamilies(), nil
}

func (c *Client) AuditFamiliesResolveFromState(state DoctrineState) ([]AuditFamily, bool) {
	if pool := extractFamilyPool(state); len(pool) > 0 {
		return AuditFamiliesFromPool(pool), true
	}
	return nil, false
}

func extractFamilyPool(state DoctrineState) []string {

	if pool := poolFromKeys(state, "reviewer", "family_disjoint_pool"); pool != nil {
		return pool
	}

	if pool := poolFromKeys(state, "Reviewer", "FamilyDisjointPool"); pool != nil {
		return pool
	}
	return nil
}

func poolFromKeys(state DoctrineState, outer, inner string) []string {
	v, ok := state[outer]
	if !ok {
		return nil
	}
	sub, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := sub[inner]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

type CheckSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

func (c *Client) AuditDoctorBackup(_ context.Context) ([]CheckSummary, error) {
	return []CheckSummary{}, nil
}

func (c *Client) AuditDoctorChainIntegrity(_ context.Context) ([]CheckSummary, error) {
	return []CheckSummary{}, nil
}

// AuditCriteria returns the canonical criteria-template catalog. Names
// MUST match those registered by the audit MCP (see
// internal/mcp/audit/criteria.go::defaultTemplates: default, security,
// performance, doctrine-violation). The audit MCP's CriteriaRegistry
// rejects names it does not recognise, so operators running e.g.
// `zen audit criteria show <name>` and then dispatching that name to
// the audit MCP MUST see only valid names here (review F-4).
func AuditCriteria() []AuditCriterion {
	return []AuditCriterion{
		{Name: "default", Description: "Comprehensive review (correctness + style + tests).", Source: "builtin"},
		{Name: "security", Description: "Security-grade audit (auth, crypto, input validation, secret leak, RCE).", Source: "builtin"},
		{Name: "performance", Description: "Performance + scalability review (algorithmic complexity, allocations, leaks).", Source: "builtin"},
		{Name: "doctrine-violation", Description: "Doctrine compliance check (max-scope/no-stub/no-defer; inv-zen-* boundary leakage).", Source: "builtin"},
	}
}
