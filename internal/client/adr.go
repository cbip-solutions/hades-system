// SPDX-License-Identifier: MIT
// Package client — adr.go.
//
// 9 typed wrappers for the ADR endpoints declared in
// internal/daemon/handlers/adr.go. Wire types mirror the handler declarations;
// duplication is intentional (client compiles standalone without importing
// internal/daemon — N convention).
//
// POST /v1/adr/propose — ADRPropose
// GET /v1/adr/show — ADRShow
// GET /v1/adr/list — ADRList
// GET /v1/adr/graph — ADRGraph
// GET /v1/adr/history — ADRHistory
// POST /v1/adr/accept — ADRAccept
// POST /v1/adr/reject — ADRReject
// POST /v1/adr/supersede — ADRSupersede
// POST /v1/adr/index — ADRIndex
//
// invariant: this file imports stdlib only (context, net/url, strconv).
// No internal/daemon, internal/store, or internal/adr imports.
package client

import (
	"context"
	"net/url"
	"strconv"
)

type ADR struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Topic       string            `json:"topic"`
	Plan        string            `json:"plan"`
	RiskLevel   string            `json:"risk_level,omitempty"`
	Frontmatter map[string]string `json:"frontmatter"`
	Body        string            `json:"body,omitempty"`
	CreatedAt   int64             `json:"created_at_unix"`
	UpdatedAt   int64             `json:"updated_at_unix"`
}

type ADRListClientFilter struct {
	Status    string
	Plan      string
	RiskLevel string
	Limit     int
}

type ADRGraphNode struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type ADREdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type ADRGraph struct {
	Nodes []ADRGraphNode `json:"nodes"`
	Edges []ADREdge      `json:"edges"`
}

type ADRTransition struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	At     int64  `json:"at_unix"`
	Reason string `json:"reason,omitempty"`
}

type ADRManifest struct {
	GeneratedAt int64  `json:"generated_at_unix"`
	ADRCount    int    `json:"adr_count"`
	Manifest    string `json:"manifest_json"`
	Graph       string `json:"graph_json"`
}

func (c *Client) ADRPropose(ctx context.Context, topic string) (ADR, error) {
	return c.ADRProposeWithPlan(ctx, topic, "")
}

func (c *Client) ADRProposeWithPlan(ctx context.Context, topic, planRange string) (ADR, error) {
	body := map[string]any{"topic": topic}
	if planRange != "" {
		body["plan_range"] = planRange
	}
	var out ADR
	if err := c.postJSON(ctx, "/v1/adr/propose", body, &out); err != nil {
		return ADR{}, err
	}
	return out, nil
}

func (c *Client) ADRShow(ctx context.Context, id string) (ADR, error) {
	q := url.Values{"id": []string{id}}
	var out ADR
	if err := c.getJSON(ctx, "/v1/adr/show?"+q.Encode(), &out); err != nil {
		return ADR{}, err
	}
	return out, nil
}

func (c *Client) ADRList(ctx context.Context, filter ADRListClientFilter) ([]ADR, error) {
	q := url.Values{}
	if filter.Status != "" {
		q.Set("status", filter.Status)
	}
	if filter.Plan != "" {
		q.Set("plan", filter.Plan)
	}
	if filter.RiskLevel != "" {
		q.Set("risk_level", filter.RiskLevel)
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	path := "/v1/adr/list"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []ADR `json:"items"`
		Count int   `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []ADR{}
	}
	return out.Items, nil
}

func (c *Client) ADRGraph(ctx context.Context, from string, depth int) (ADRGraph, error) {
	if depth <= 0 {
		depth = 1
	}
	q := url.Values{
		"from":  []string{from},
		"depth": []string{strconv.Itoa(depth)},
	}
	var out ADRGraph
	if err := c.getJSON(ctx, "/v1/adr/graph?"+q.Encode(), &out); err != nil {
		return ADRGraph{}, err
	}
	return out, nil
}

func (c *Client) ADRHistory(ctx context.Context, id string) ([]ADRTransition, error) {
	q := url.Values{"id": []string{id}}
	var out struct {
		Items []ADRTransition `json:"items"`
		Count int             `json:"count"`
	}
	if err := c.getJSON(ctx, "/v1/adr/history?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []ADRTransition{}
	}
	return out.Items, nil
}

func (c *Client) ADRAccept(ctx context.Context, id, reason string) error {
	body := map[string]any{"id": id, "reason": reason}
	return c.postJSON(ctx, "/v1/adr/accept", body, nil)
}

func (c *Client) ADRReject(ctx context.Context, id, reason string) error {
	body := map[string]any{"id": id, "reason": reason}
	return c.postJSON(ctx, "/v1/adr/reject", body, nil)
}

func (c *Client) ADRSupersede(ctx context.Context, oldID, newID, reason string) error {
	body := map[string]any{"old_id": oldID, "new_id": newID, "reason": reason}
	return c.postJSON(ctx, "/v1/adr/supersede", body, nil)
}

func (c *Client) ADRIndex(ctx context.Context, check bool) (ADRManifest, error) {
	body := map[string]any{"check": check}
	var out ADRManifest
	if err := c.postJSON(ctx, "/v1/adr/index", body, &out); err != nil {
		return ADRManifest{}, err
	}
	return out, nil
}
