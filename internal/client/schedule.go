// SPDX-License-Identifier: MIT
// Package client — schedule.go (Plan 7 Phase D Task D-13).
//
// Six methods + supporting wire types for the daemon's
// /v1/schedules/* surface backing the operator-facing
// `zen schedule {routine, task, loop, history, queue}` CLI:
//
//	ScheduleCreate   POST /v1/schedules                — create routine
//	ScheduleList     GET  /v1/schedules                — list (filter by alias / all)
//	ScheduleDelete   POST /v1/schedules/{id}/delete    — soft-delete (Disabled then DELETE)
//	ScheduleRun      POST /v1/schedules/{id}/run       — manual trigger (Fire)
//	ScheduleHistory  GET  /v1/schedules/{id}/history   — fire-history rows in window
//	ScheduleQueue    GET  /v1/schedules/queue          — next-24h fire queue
//
// Field names + JSON tags align with the daemon-side handler in
// internal/daemon/handlers/schedule_p7.go. Times use RFC3339 over the
// wire (Go's encoding/json default for time.Time). Tier / Outcome are
// surfaced as their canonical scheduler.* enum string forms ("routine",
// "task", "loop", "success", "failed", "skipped", "rate-limited") so the
// CLI can render them without a translation table.
//
// Phase I gap: until the daemon mounts the /v1/schedules/* routes in
// Phase I, every call returns 503. The CLI surfaces 503 as exit 2
// (infra-issue, not operator-typo). Mirrors the Plan 2 /v1/messages
// graceful-degradation pattern: client method shipped early, daemon
// route ships in a follow-up phase.
package client

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type CreateRoutineRequest struct {
	ProjectAlias  string        `json:"project_alias"`
	Action        string        `json:"action"`
	Trigger       string        `json:"trigger"`
	CronExpr      string        `json:"cron_expr,omitempty"`
	RepoURL       string        `json:"repo_url,omitempty"`
	Branch        string        `json:"branch,omitempty"`
	MissPolicyStr string        `json:"miss_policy"`
	MissLookback  time.Duration `json:"miss_lookback_ns,omitempty"`
}

type CreateRoutineResponse struct {
	ID             string    `json:"id"`
	Tier           string    `json:"tier"`
	NextRunAt      time.Time `json:"next_run_at"`
	RawBearerToken string    `json:"raw_bearer_token,omitempty"`
}

type CreateTaskRequest struct {
	ProjectAlias string        `json:"project_alias"`
	Action       string        `json:"action"`
	In           time.Duration `json:"in_ns"`
}

type CreateTaskResponse struct {
	ID        string    `json:"id"`
	Tier      string    `json:"tier"`
	NextRunAt time.Time `json:"next_run_at"`
}

type CreateLoopRequest struct {
	ProjectAlias string        `json:"project_alias"`
	Action       string        `json:"action"`
	Interval     time.Duration `json:"interval_ns"`
}

type CreateLoopResponse struct {
	ID        string `json:"id"`
	Tier      string `json:"tier"`
	SessionID string `json:"session_id"`
}

type RoutineRow struct {
	ID           string    `json:"id"`
	ProjectAlias string    `json:"project_alias"`
	Action       string    `json:"action"`
	Tier         string    `json:"tier"`
	Status       string    `json:"status"`
	NextRunAt    time.Time `json:"next_run_at,omitempty"`
}

type listSchedulesResponse struct {
	Schedules []RoutineRow `json:"schedules"`
}

type RunRoutineResponse struct {
	Outcome    string  `json:"outcome"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	DurationMs int64   `json:"duration_ms"`
	Reason     string  `json:"reason,omitempty"`
}

type HistoryRow struct {
	ScheduleID string    `json:"schedule_id"`
	FiredAt    time.Time `json:"fired_at"`
	Outcome    int       `json:"outcome"`
	Reason     string    `json:"reason,omitempty"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

type historyResponse struct {
	Rows []HistoryRow `json:"rows"`
}

type QueueRow struct {
	ID           string    `json:"id"`
	ProjectAlias string    `json:"project_alias"`
	Action       string    `json:"action"`
	NextRunAt    time.Time `json:"next_run_at"`
}

type queueResponse struct {
	Rows []QueueRow `json:"rows"`
}

func (c *Client) ScheduleCreate(ctx context.Context, req CreateRoutineRequest) (*CreateRoutineResponse, error) {
	var resp CreateRoutineResponse
	if err := c.postJSON(ctx, "/v1/schedules", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ScheduleCreateTask(ctx context.Context, req CreateTaskRequest) (*CreateTaskResponse, error) {
	body := map[string]any{
		"kind":          "task",
		"project_alias": req.ProjectAlias,
		"action":        req.Action,
		"in_ns":         req.In,
	}
	var resp CreateTaskResponse
	if err := c.postJSON(ctx, "/v1/schedules", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ScheduleCreateLoop(ctx context.Context, req CreateLoopRequest) (*CreateLoopResponse, error) {
	body := map[string]any{
		"kind":          "loop",
		"project_alias": req.ProjectAlias,
		"action":        req.Action,
		"interval_ns":   req.Interval,
	}
	var resp CreateLoopResponse
	if err := c.postJSON(ctx, "/v1/schedules", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ScheduleList(ctx context.Context, alias string) ([]RoutineRow, error) {
	path := "/v1/schedules"
	if alias != "" {
		path += "?alias=" + url.QueryEscape(alias)
	}
	var resp listSchedulesResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	if resp.Schedules == nil {
		return []RoutineRow{}, nil
	}
	return resp.Schedules, nil
}

func (c *Client) ScheduleDelete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("ScheduleDelete: id is empty")
	}
	path := "/v1/schedules/" + url.PathEscape(id) + "/delete"
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, path, nil, &resp)
}

func (c *Client) ScheduleRun(ctx context.Context, id string) (*RunRoutineResponse, error) {
	if id == "" {
		return nil, fmt.Errorf("ScheduleRun: id is empty")
	}
	path := "/v1/schedules/" + url.PathEscape(id) + "/run"
	var resp RunRoutineResponse
	if err := c.postJSON(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ScheduleHistory(ctx context.Context, id string, from, to time.Time) ([]HistoryRow, error) {
	if id == "" {
		return nil, fmt.Errorf("ScheduleHistory: id is empty")
	}
	q := url.Values{}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	path := "/v1/schedules/" + url.PathEscape(id) + "/history?" + q.Encode()
	var resp historyResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []HistoryRow{}, nil
	}
	return resp.Rows, nil
}

func (c *Client) ScheduleQueue(ctx context.Context) ([]QueueRow, error) {
	var resp queueResponse
	if err := c.getJSON(ctx, "/v1/schedules/queue", &resp); err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []QueueRow{}, nil
	}
	return resp.Rows, nil
}
