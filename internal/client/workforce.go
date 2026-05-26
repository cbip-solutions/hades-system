// SPDX-License-Identifier: MIT
// Package client — workforce.go (Plan 4 Phase N Task N-2).
//
// Typed wrappers for /v1/workforce/* endpoints exposed by the daemon
// (Phase G). The CLI surface in internal/cli/workforce.go uses these.
package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type WorkforceSpec struct {
	ID           string   `json:"id"`
	Variant      string   `json:"variant"`
	TaskTier     string   `json:"task_tier"`
	ModelClass   string   `json:"model_class"`
	DoctrineName string   `json:"doctrine_name"`
	ProjectID    string   `json:"project_id"`
	Tools        []string `json:"tools"`
	CreatedAt    int64    `json:"created_at"`
}

type WorkforceWorker struct {
	ID        string `json:"id"`
	SpecID    string `json:"spec_id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id"`
	ThreadID  string `json:"thread_id"`
	StartedAt int64  `json:"started_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type WorkforceCheckpoint struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	ThreadID  string `json:"thread_id"`
	StateJSON string `json:"state_json"`
	CreatedAt int64  `json:"created_at"`
}

type WorkforceFixPrompt struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	FromLayer string `json:"from_layer"`
	Prompt    string `json:"prompt"`
	Consumed  bool   `json:"consumed"`
	CreatedAt int64  `json:"created_at"`
}

type GateStateResp struct {
	State     string `json:"state"`
	CanPause  bool   `json:"can_pause"`
	CanResume bool   `json:"can_resume"`
}

type GatePauseReq struct {
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type GatePauseResp struct {
	State  string `json:"state"`
	Paused bool   `json:"paused"`
}

type GateResumeResp struct {
	State   string `json:"state"`
	Running bool   `json:"running"`
}

type WorkforceStatusSnapshot struct {
	GateState         string `json:"gate_state"`
	CanPause          bool   `json:"can_pause"`
	CanResume         bool   `json:"can_resume"`
	WorkersTotal      int    `json:"workers_total"`
	WorkersPending    int    `json:"workers_pending"`
	WorkersInProgress int    `json:"workers_in_progress"`
	WorkersReview     int    `json:"workers_review"`
	WorkersDone       int    `json:"workers_done"`
	WorkersFailed     int    `json:"workers_failed"`
	SpecsLoaded       int    `json:"specs_loaded"`
	CheckpointsDepth  int    `json:"checkpoints_depth"`
	FixPromptsDepth   int    `json:"fix_prompts_depth"`
}

func (c *Client) WorkforceSpecs(ctx context.Context, variant string, limit, offset int) ([]WorkforceSpec, error) {
	q := url.Values{}
	if variant != "" {
		q.Set("variant", variant)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out struct {
		Items []WorkforceSpec `json:"items"`
		Count int             `json:"count"`
	}
	path := "/v1/workforce/specs"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) WorkforceWorkers(ctx context.Context, status string, limit, offset int) ([]WorkforceWorker, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out struct {
		Items []WorkforceWorker `json:"items"`
		Count int               `json:"count"`
	}
	path := "/v1/workforce/workers"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) WorkforceCheckpoints(ctx context.Context, taskID string, limit, offset int) ([]WorkforceCheckpoint, error) {
	q := url.Values{}
	if taskID != "" {
		q.Set("task_id", taskID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	var out struct {
		Items []WorkforceCheckpoint `json:"items"`
		Count int                   `json:"count"`
	}
	path := "/v1/workforce/checkpoints"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) WorkforceFixPrompts(ctx context.Context, taskID string, limit, offset int) ([]WorkforceFixPrompt, error) {
	q := url.Values{}
	if taskID != "" {
		q.Set("task_id", taskID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Items []WorkforceFixPrompt `json:"items"`
	}
	path := "/v1/workforce/fix_prompts"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) GateState(ctx context.Context) (*GateStateResp, error) {
	var out GateStateResp
	if err := c.getJSON(ctx, "/v1/workforce/gate/state", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GatePause(ctx context.Context, req GatePauseReq) (*GatePauseResp, error) {
	var out GatePauseResp
	if err := c.postJSON(ctx, "/v1/workforce/gate/pause", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GateResume(ctx context.Context) (*GateResumeResp, error) {
	var out GateResumeResp
	if err := c.postJSON(ctx, "/v1/workforce/gate/resume", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) WorkforceStatus(ctx context.Context) (*WorkforceStatusSnapshot, error) {
	snap := &WorkforceStatusSnapshot{}
	gate, err := c.GateState(ctx)
	if err != nil {
		return nil, fmt.Errorf("gate state: %w", err)
	}
	snap.GateState = gate.State
	snap.CanPause = gate.CanPause
	snap.CanResume = gate.CanResume

	workers, err := c.WorkforceWorkers(ctx, "", 500, 0)
	if err != nil {
		return nil, fmt.Errorf("workers: %w", err)
	}
	snap.WorkersTotal = len(workers)
	for _, w := range workers {
		switch w.Status {
		case "pending":
			snap.WorkersPending++
		case "in_progress":
			snap.WorkersInProgress++
		case "review":
			snap.WorkersReview++
		case "done":
			snap.WorkersDone++
		case "failed":
			snap.WorkersFailed++
		}
	}

	specs, err := c.WorkforceSpecs(ctx, "", 500, 0)
	if err != nil {
		return nil, fmt.Errorf("specs: %w", err)
	}
	snap.SpecsLoaded = len(specs)

	cps, err := c.WorkforceCheckpoints(ctx, "", 500, 0)
	if err != nil {
		return nil, fmt.Errorf("checkpoints: %w", err)
	}
	snap.CheckpointsDepth = len(cps)

	fps, err := c.WorkforceFixPrompts(ctx, "", 500, 0)
	if err != nil {
		return nil, fmt.Errorf("fix_prompts: %w", err)
	}
	snap.FixPromptsDepth = len(fps)

	return snap, nil
}

func FormatUnix(sec int64) string {
	if sec == 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
