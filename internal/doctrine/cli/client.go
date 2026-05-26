// SPDX-License-Identifier: MIT
// Package cli — client.go (Plan 8 Phase I Task I-1).
//
// Typed daemon HTTP client wrapper for /v1/doctrine/* routes consumed by
// Phase I + Phase K CLI subcommands. Wraps net/http with descriptive
// error messages in español.
//
// The wrapper intentionally lives under internal/doctrine/cli/ (NOT under
// internal/client/) so Phase I owns its full request/response surface
// without fighting Plan 4 v1's existing internal/client/doctrine.go
// (which serves Plan 4 v1 routes — different shape). Phase N will collapse
// the v1+v2 client surfaces; until then they coexist.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type Client struct {
	baseURL string
	hc      *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, hc: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) withUDS(socketPath string) *Client {
	c.hc = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	return c
}

func (c *Client) Active(ctx context.Context) (client.DoctrineV2ActiveResp, error) {
	var out client.DoctrineV2ActiveResp
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/active", nil, nil, &out)
	return out, err
}

func (c *Client) List(ctx context.Context, source string) (client.DoctrineV2ListResp, error) {
	var out client.DoctrineV2ListResp
	q := url.Values{}
	if source != "" {
		q.Set("source", source)
	}
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/list", q, nil, &out)
	return out, err
}

func (c *Client) Show(ctx context.Context, name, format, section string) (client.DoctrineV2ShowResp, error) {
	var out client.DoctrineV2ShowResp
	q := url.Values{}
	q.Set("name", name)
	if format != "" {
		q.Set("format", format)
	}
	if section != "" {
		q.Set("section", section)
	}
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/show", q, nil, &out)
	return out, err
}

func (c *Client) Status(ctx context.Context, projectAlias string) (client.DoctrineV2StatusResp, error) {
	var out client.DoctrineV2StatusResp
	q := url.Values{}
	if projectAlias != "" {
		q.Set("project", projectAlias)
	}
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/status", q, nil, &out)
	return out, err
}

type HistoryReq struct {
	Since  string
	Filter string
	Limit  int
}

func (c *Client) History(ctx context.Context, req HistoryReq) (client.DoctrineV2HistoryResp, error) {
	var out client.DoctrineV2HistoryResp
	q := url.Values{}
	if req.Since != "" {
		q.Set("since", req.Since)
	}
	if req.Filter != "" {
		q.Set("filter", req.Filter)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/history", q, nil, &out)
	return out, err
}

func (c *Client) Diff(ctx context.Context, a, b, section string) (client.DoctrineV2DiffResp, error) {
	var out client.DoctrineV2DiffResp
	q := url.Values{}
	q.Set("a", a)
	q.Set("b", b)
	if section != "" {
		q.Set("section", section)
	}
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/diff", q, nil, &out)
	return out, err
}

func (c *Client) Validate(ctx context.Context, againstBaseline, tomlBody string) (client.DoctrineV2ValidateResp, error) {
	var out client.DoctrineV2ValidateResp
	body := client.DoctrineV2ValidateReq{
		AgainstBaseline: againstBaseline,
		TOMLContent:     tomlBody,
	}
	err := c.do(ctx, http.MethodPost, "/v1/doctrine/validate", nil, body, &out)
	return out, err
}

// AmendmentProposal mirrors the wire format Plan 5 emits at
// /v1/doctrine/propose-list (defined in internal/client.DoctrineProposal).
// Phase K declares its own typed wrapper here to keep the Phase I+K cli
// package self-contained — the field tags MUST match Plan 5's wire bytes
// exactly. Drift surfaces as Unmarshal failures on integration tests.
type AmendmentProposal struct {
	ID                    string `json:"id"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	ProposedAt            int64  `json:"proposed_at"`
	BodyMarkdown          string `json:"body_markdown,omitempty"`
	AppliedAt             int64  `json:"applied_at,omitempty"`
	RevertedAt            int64  `json:"reverted_at,omitempty"`
	OperatorReason        string `json:"operator_reason,omitempty"`
	CooldownRemainSeconds int64  `json:"cooldown_remain_seconds,omitempty"`
}

type AmendmentProposalList struct {
	Proposals []AmendmentProposal `json:"proposals"`
}

type AmendmentDecision struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type AmendmentProposeRequest struct {
	RulePath         string `json:"rule_path"`
	NewValue         string `json:"new_value"`
	Justification    string `json:"justification"`
	Category         string `json:"category"`
	CooldownOverride bool   `json:"cooldown_override,omitempty"`
}

type AmendmentProposeResponse struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	RulePath        string `json:"rule_path"`
	NewValue        string `json:"new_value"`
	Category        string `json:"category"`
	ProposedAt      int64  `json:"proposed_at"`
	Proposer        string `json:"proposer,omitempty"`
	AdrMarkdownPath string `json:"adr_markdown_path,omitempty"`
}

func (c *Client) AmendmentProposeList(ctx context.Context) (AmendmentProposalList, error) {
	var out AmendmentProposalList
	err := c.do(ctx, http.MethodGet, "/v1/doctrine/propose-list", nil, nil, &out)
	return out, err
}

func (c *Client) AmendmentAck(ctx context.Context, req AmendmentDecision) error {
	return c.do(ctx, http.MethodPost, "/v1/doctrine/ack", nil, req, nil)
}

func (c *Client) AmendmentDeny(ctx context.Context, req AmendmentDecision) error {
	return c.do(ctx, http.MethodPost, "/v1/doctrine/deny", nil, req, nil)
}

func (c *Client) AmendmentRevert(ctx context.Context, req AmendmentDecision) error {
	return c.do(ctx, http.MethodPost, "/v1/doctrine/revert", nil, req, nil)
}

func (c *Client) AmendmentPropose(ctx context.Context, req AmendmentProposeRequest) (AmendmentProposeResponse, error) {
	var out AmendmentProposeResponse
	err := c.do(ctx, http.MethodPost, "/v1/doctrine/propose", nil, req, &out)
	return out, err
}

func (c *Client) Reload(ctx context.Context, path string) (client.DoctrineV2ReloadResp, error) {
	var out client.DoctrineV2ReloadResp
	body := client.DoctrineV2ReloadReq{Path: path}
	err := c.do(ctx, http.MethodPost, "/v1/doctrine/reload", nil, body, &out)
	return out, err
}

type MigrateReq struct {
	TOMLContent       string
	FromSchemaVersion string
}

func (c *Client) Migrate(ctx context.Context, req MigrateReq) (client.DoctrineV2MigrateResp, error) {
	var out client.DoctrineV2MigrateResp
	body := client.DoctrineV2MigrateReq{
		TOMLContent:       req.TOMLContent,
		FromSchemaVersion: req.FromSchemaVersion,
	}
	err := c.do(ctx, http.MethodPost, "/v1/doctrine/migrate", nil, body, &out)
	return out, err
}

type ReinforceReq struct {
	TaskKind     string
	ProjectAlias string
	Stage        string
	Phase        string
	PlanID       string
}

func (c *Client) Reinforce(ctx context.Context, req ReinforceReq) (client.DoctrineV2ReinforceResp, error) {
	var out client.DoctrineV2ReinforceResp
	body := client.DoctrineV2ReinforceReq{
		TaskKind:     req.TaskKind,
		ProjectAlias: req.ProjectAlias,
		Stage:        req.Stage,
		Phase:        req.Phase,
		PlanID:       req.PlanID,
	}
	q := url.Values{}
	q.Set("task_kind", req.TaskKind)
	if req.ProjectAlias != "" {
		q.Set("project", req.ProjectAlias)
	}
	if req.Stage != "" {
		q.Set("stage", req.Stage)
	}
	if req.Phase != "" {
		q.Set("phase", req.Phase)
	}
	if req.PlanID != "" {
		q.Set("plan_id", req.PlanID)
	}
	err := c.do(ctx, http.MethodPost, "/v1/doctrine/reinforce", q, body, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	u := c.baseURL + path
	if q != nil && len(q) > 0 {
		u += "?" + q.Encode()
	}
	var bodyR io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("doctrine cli: codificación JSON falló: %w", err)
		}
		bodyR = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyR)
	if err != nil {
		return fmt.Errorf("doctrine cli: construcción de petición falló: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("doctrine cli: petición HTTP falló: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("doctrine cli: daemon devolvió status %d: %s", resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("doctrine cli: decodificación JSON falló: %w", err)
		}
	}
	return nil
}
