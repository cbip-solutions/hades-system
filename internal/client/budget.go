// SPDX-License-Identifier: MIT
// Package client — budget.go.
//
// Typed wrappers for the release budget HTTP surface ( handlers in
// internal/daemon/handlers/budget_plan4.go). The CLI in
// internal/cli/budget.go uses these to expose:
//
// rollup, caps {show, set}, anomalies, pause, resume,
// events, axes, pause-modes
//
// read-only spend-rollup over CostCounters; release retains the call site
// because zen day + zen doctor still consume it.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// BudgetSummaryRollup calls GET /v1/budget?range=<window>. The window
// MUST be one of CostCounters' registered windows ("24h" or "30d"); the
// daemon returns 400 on any other value. Empty rng defaults to "24h" —
// the operator-facing default surfaces "what happened recently" without
// requiring a flag.
//
// reused that name for the morning-brief summary endpoint
// (release_summary.go), so this release read-rollup has been renamed to
// `BudgetSummaryRollup` to keep both surfaces compilable. The wire
// path (`GET /v1/budget?range=`) is unchanged. Callers: zen doctor
// orchestrator checks.
func (c *Client) BudgetSummaryRollup(ctx context.Context, rng string) (*BudgetSummaryResp, error) {
	path := "/v1/budget"
	if rng != "" {
		v := url.Values{}
		v.Set("range", rng)
		path += "?" + v.Encode()
	}
	var out BudgetSummaryResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type BudgetCapStatus struct {
	RemainingUSD float64 `json:"remaining_usd"`
	Blocked      bool    `json:"blocked"`
	BlockedScope string  `json:"blocked_scope,omitempty"`
}

type BudgetRecordReq struct {
	CostID      string            `json:"cost_id"`
	AmountUSD   float64           `json:"amount_usd"`
	AxisTags    map[string]string `json:"axis_tags"`
	OperationID string            `json:"operation_id,omitempty"`
	WorkerID    string            `json:"worker_id,omitempty"`
}

type BudgetAxisTag struct {
	AxisName  string `json:"axis_name"`
	AxisValue string `json:"axis_value"`
}

type BudgetAnomaly struct {
	ZScore  float64 `json:"z_score"`
	Mean    float64 `json:"mean"`
	StdDev  float64 `json:"std_dev"`
	Samples int64   `json:"samples"`
}

type BudgetEvent struct {
	ID         string  `json:"id"`
	Scope      string  `json:"scope"`
	Value      string  `json:"value"`
	EventType  string  `json:"event_type"`
	AmountUSD  float64 `json:"amount_usd,omitempty"`
	OccurredAt int64   `json:"occurred_at"`
}

type BudgetPauseReq struct {
	Scope  string `json:"scope"`
	Value  string `json:"value"`
	Reason string `json:"reason,omitempty"`
}

type BudgetResumeReq struct {
	Scope string `json:"scope"`
	Value string `json:"value"`
}

type BudgetPauseResp struct {
	State string `json:"state"`
	Scope string `json:"scope"`
	Value string `json:"value"`
}

type PauseMode struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Default     bool   `json:"default" yaml:"default"`
}

func (c *Client) BudgetCapStatusCall(ctx context.Context, axis, value string) (*BudgetCapStatus, error) {
	if axis == "" || value == "" {
		return nil, fmt.Errorf("axis and value required")
	}
	q := url.Values{"axis": []string{axis}, "value": []string{value}}
	var out BudgetCapStatus
	if err := c.getJSON(ctx, "/v1/budget/cap_status?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BudgetRecord(ctx context.Context, req BudgetRecordReq) error {
	return c.postJSON(ctx, "/v1/budget/record", req, nil)
}

func (c *Client) BudgetAxes(ctx context.Context, costID string) ([]BudgetAxisTag, error) {
	if costID == "" {
		return nil, fmt.Errorf("cost_id required")
	}
	q := url.Values{"cost_id": []string{costID}}
	var out []BudgetAxisTag
	if err := c.getJSON(ctx, "/v1/budget/axes?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) BudgetAnomalyCall(ctx context.Context, scope, value string, windowSec int64) (*BudgetAnomaly, error) {
	if scope == "" || value == "" {
		return nil, fmt.Errorf("scope and value required")
	}
	q := url.Values{"scope": []string{scope}, "value": []string{value}}
	if windowSec > 0 {
		q.Set("window", strconv.FormatInt(windowSec, 10))
	}
	var out BudgetAnomaly
	if err := c.getJSON(ctx, "/v1/budget/anomaly?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BudgetEvents(ctx context.Context, sinceUnix int64, limit int) ([]BudgetEvent, error) {
	q := url.Values{}
	if sinceUnix > 0 {
		q.Set("since", strconv.FormatInt(sinceUnix, 10))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Events []BudgetEvent `json:"events"`
		Count  int           `json:"count"`
	}
	path := "/v1/budget/events"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

func (c *Client) BudgetPauseCall(ctx context.Context, req BudgetPauseReq) (*BudgetPauseResp, error) {
	var out BudgetPauseResp
	if err := c.postJSON(ctx, "/v1/budget/pause", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BudgetResumeCall(ctx context.Context, req BudgetResumeReq) (*BudgetPauseResp, error) {
	var out BudgetPauseResp
	if err := c.postJSON(ctx, "/v1/budget/resume", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PauseModes returns the canonical 3-mode catalog. The names match the
// `pause_mode` field accepted by the doctrine schema (see
// internal/doctrine/builtin.go: max-scope=descriptive, default=quiet,
// capa-firewall=fail_loud). Operators copy these names into
// `[budget].pause_mode` in zenswarm.toml; the doctrine validator only
// accepts these three canonical strings, so the CLI surface MUST match.
//
// `Default: true` is set on `descriptive` because that is the
// max-scope-builtin choice (the OOTB default per spec §0.2).
//
// Plan I may surface a daemon route if the doctrine schema gains
// additional modes; until then this CLI-side list is the source of truth
// for `zen budget pause-modes` output.
func PauseModes() []PauseMode {
	return []PauseMode{
		{Name: "descriptive", Description: "Visible pause; emits notification (max-scope OOTB).", Default: true},
		{Name: "quiet", Description: "Silent pause; no notification (default doctrine; automated test windows)."},
		{Name: "fail_loud", Description: "Fail loudly on cap hit; no silent overruns (capa-firewall doctrine)."},
	}
}
