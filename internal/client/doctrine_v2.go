// SPDX-License-Identifier: MIT
// Package client — doctrine_v2.go (Plan 8 Phase I).
//
// Typed structs for the 10 daemon HTTP API routes consumed by Phase I CLI:
//
//	GET  /v1/doctrine/active        → DoctrineV2ActiveResp
//	GET  /v1/doctrine/list?source=X → DoctrineV2ListResp
//	GET  /v1/doctrine/show?name=X   → DoctrineV2ShowResp
//	POST /v1/doctrine/validate      → DoctrineV2ValidateResp
//	POST /v1/doctrine/reload        → DoctrineV2ReloadResp
//	GET  /v1/doctrine/status        → DoctrineV2StatusResp
//	GET  /v1/doctrine/history       → DoctrineV2HistoryResp
//	GET  /v1/doctrine/diff?a=X&b=Y  → DoctrineV2DiffResp
//	POST /v1/doctrine/migrate       → DoctrineV2MigrateResp
//	POST /v1/doctrine/reinforce     → DoctrineV2ReinforceResp
//
// JSON tags follow daemon-handler convention (lower_snake_case).
// inv-zen-031 boundary preserved: this file imports zero non-stdlib (only
// "context" — pure DTOs + 4 thin Client wrappers added in Phase N for
// the doctor.doctrine 11-check matrix).
package client

import "context"

type DoctrineV2ActiveResp struct {
	Name            string `json:"name"`
	SchemaVersion   string `json:"schema_version"`
	DoctrineVersion string `json:"doctrine_version"`
	Source          string `json:"source"`
}

type DoctrineV2ListItem struct {
	Name            string `json:"name"`
	Source          string `json:"source"`
	SchemaVersion   string `json:"schema_version"`
	DoctrineVersion string `json:"doctrine_version"`
}

type DoctrineV2ListResp struct {
	Items []DoctrineV2ListItem `json:"items"`
}

type DoctrineV2ShowResp struct {
	Name    string `json:"name"`
	Format  string `json:"format"`
	Section string `json:"section,omitempty"`
	Body    string `json:"body"`
}

type DoctrineV2ValidateReq struct {
	AgainstBaseline string `json:"against_baseline,omitempty"`
	TOMLContent     string `json:"toml_content"`
}

type DoctrineV2ValidateResp struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type DoctrineV2ReloadReq struct {
	Path string `json:"path,omitempty"`
}

type DoctrineV2ReloadResp struct {
	Reloaded bool                 `json:"reloaded"`
	State    DoctrineV2ActiveResp `json:"state,omitempty"`
	Error    string               `json:"error,omitempty"`
	Errors   []string             `json:"errors,omitempty"`
}

type DoctrineV2StatusResp struct {
	Active         DoctrineV2ActiveResp `json:"active"`
	LastReloadAt   string               `json:"last_reload_at"`
	LastReloadOk   bool                 `json:"last_reload_ok"`
	WatcherHealthy bool                 `json:"watcher_healthy"`
	PendingChanges []string             `json:"pending_changes"`
}

type DoctrineV2HistoryEvent struct {
	Type    string         `json:"type"`
	AtUnix  int64          `json:"at_unix"`
	Payload map[string]any `json:"payload,omitempty"`
}

type DoctrineV2HistoryResp struct {
	Events []DoctrineV2HistoryEvent `json:"events"`
}

type DoctrineV2DiffEntry struct {
	Path   string `json:"path"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}

type DoctrineV2DiffResp struct {
	From  string                `json:"from"`
	To    string                `json:"to"`
	Diffs []DoctrineV2DiffEntry `json:"diffs"`
}

type DoctrineV2MigrateReq struct {
	TOMLContent       string `json:"toml_content"`
	FromSchemaVersion string `json:"from_schema_version"`
}

type DoctrineV2MigrateResp struct {
	ToSchemaVersion string   `json:"to_schema_version"`
	TOMLContent     string   `json:"toml_content"`
	Warnings        []string `json:"warnings,omitempty"`
}

type DoctrineV2ReinforceReq struct {
	TaskKind     string `json:"task_kind"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Stage        string `json:"stage,omitempty"`
	Phase        string `json:"phase,omitempty"`
	PlanID       string `json:"plan_id,omitempty"`
}

type DoctrineV2ReinforceResp struct {
	Rendered string `json:"rendered"`
}

func (c *Client) DoctrineV2ActiveCall(ctx context.Context) (*DoctrineV2ActiveResp, error) {
	var out DoctrineV2ActiveResp
	if err := c.getJSON(ctx, "/v1/doctrine/active", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineV2ListCall(ctx context.Context, source string) (*DoctrineV2ListResp, error) {
	path := "/v1/doctrine/list"
	if source != "" {
		path += "?source=" + source
	}
	var out DoctrineV2ListResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineV2StatusCall(ctx context.Context, project string) (*DoctrineV2StatusResp, error) {
	path := "/v1/doctrine/status"
	if project != "" {
		path += "?project=" + project
	}
	var out DoctrineV2StatusResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineV2HistoryCall(ctx context.Context, since string, limit int) (*DoctrineV2HistoryResp, error) {
	path := "/v1/doctrine/history"
	first := true
	if since != "" {
		path += "?since=" + since
		first = false
	}
	if limit > 0 {
		sep := "?"
		if !first {
			sep = "&"
		}
		path += sep + "limit=" + itoa(limit)
	}
	var out DoctrineV2HistoryResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
