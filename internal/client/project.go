// SPDX-License-Identifier: MIT
// Package client — project.go (Plan 7 Phase A Task A-8).
//
// Three POST methods for the daemon's /v1/projects/* lifecycle surface
// (doctor / archive / rm). Wire types mirror the daemon-side response
// shape declared in internal/daemon/handlers/projects_p7.go.
package client

import (
	"context"
)

type ProjectDoctorResponse struct {
	Healthy       bool                      `json:"healthy"`
	Alias         string                    `json:"alias"`
	IDSha256      string                    `json:"id_sha256"`
	CanonicalPath string                    `json:"canonical_path"`
	PathHistory   []ProjectPathHistoryEntry `json:"path_history"`
	MvDetected    *ProjectMvDetection       `json:"mv_detected,omitempty"`
	Hint          string                    `json:"hint,omitempty"`
}

type ProjectPathHistoryEntry struct {
	Path      string `json:"path"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
}

type ProjectMvDetection struct {
	OldPath    string `json:"old_path"`
	NewPath    string `json:"new_path"`
	OldIDShort string `json:"old_id_short"`
	NewIDShort string `json:"new_id_short"`
}

func (c *Client) ProjectDoctor(ctx context.Context, alias, cwd string, rebind bool) (*ProjectDoctorResponse, error) {
	body := map[string]any{"alias": alias, "cwd": cwd, "rebind": rebind}
	var resp ProjectDoctorResponse
	if err := c.postJSON(ctx, "/v1/projects/doctor", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ProjectArchive(ctx context.Context, alias string) error {
	body := map[string]any{"alias": alias}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, "/v1/projects/archive", body, &resp)
}

func (c *Client) ProjectRemove(ctx context.Context, alias string) error {
	body := map[string]any{"alias": alias}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, "/v1/projects/rm", body, &resp)
}
