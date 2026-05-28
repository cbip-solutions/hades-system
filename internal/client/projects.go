// SPDX-License-Identifier: MIT
// Package client — projects.go.
//
// Three typed wrappers for the daemon's project-aggregate doctor surface
// that declares and RunFullProbe orchestrator consumes:
//
// - GET /v1/projects → ProjectsListAll
// - GET /v1/projects/{alias}/doctor → ProjectDoctorReport
// - GET /v1/meta/snapshot → MetaSnapshot
//
// Method-name reconciliation note (drift from spec): the spec named the
// per-alias doctor wrapper `ProjectDoctor`. That symbol already exists in
// project.go with a different signature (POST /v1/projects/doctor with
// alias+cwd+rebind from task). Renaming the existing method
// would break contract; instead we use `ProjectDoctorReport`
// for the GET-based aggregate-doctor wrapper introduced here. Same
// rationale for `ProjectsListAll` (avoids confusion with handler-side
// ProjectsList) and the new typed Project struct (mirrors the
// daemon wire shape, distinct from the response struct).
//
// The wire shapes here MUST stay in sync with the daemon
// handlers (`internal/daemon/handlers/projects.go` GET routes, currently
// scaffolded as `notImplemented` returning 501 — the orchestrator
// surfaces a Warn probe in that interim state and the full-shape
// implementation will populate the response).
package client

import (
	"context"
	"time"
)

type Project struct {
	ID              string    `json:"id"`
	Alias           string    `json:"alias"`
	Path            string    `json:"path"`
	LastActivatedAt time.Time `json:"last_activated_at,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	AutonomousState string    `json:"autonomous_state,omitempty"`
}

func (p Project) IsArchived() bool {
	return p.AutonomousState == "complete"
}

func (c *Client) ProjectsListAll(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.getJSON(ctx, "/v1/projects", &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

type ProjectDoctorItem struct {
	Aspect  string `json:"aspect"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint"`
}

type ProjectDoctorReportResp struct {
	Items []ProjectDoctorItem `json:"items"`
}

func (c *Client) ProjectDoctorReport(ctx context.Context, alias string) (*ProjectDoctorReportResp, error) {
	var out ProjectDoctorReportResp
	if err := c.getJSON(ctx, "/v1/projects/"+alias+"/doctor", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type MetaSnapshot struct {
	PanicsLast24h      int `json:"panics_last_24h"`
	CostUtilizationPct int `json:"cost_utilization_pct"`
}

func (c *Client) MetaSnapshotGet(ctx context.Context) (*MetaSnapshot, error) {
	var out MetaSnapshot
	if err := c.getJSON(ctx, "/v1/meta/snapshot", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
