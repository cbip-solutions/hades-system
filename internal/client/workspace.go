// SPDX-License-Identifier: MIT
// Package client — workspace.go.
//
// Thin pass-throughs for the daemon's workspace REST sub-routes:
// /v1/mcpgateway/workspace/{init,list,members,link,remove,policy/get,policy/set}.
// 7 methods total — the operator-facing federation lifecycle surface.
//
// invariant single-egress preserved: every round-trip proxies through the
// daemon. invariant enforced: this file uses ONLY c.postJSON — never
// net/http directly.
package client

import "context"

type WorkspaceInitRequest struct {
	WorkspaceID   string   `json:"workspace_id"`
	OwningProject string   `json:"owning_project"`
	Members       []string `json:"members,omitempty"`
	PolicyLocked  bool     `json:"policy_locked"`
}

type WorkspaceInitResponse struct {
	WorkspaceID   string `json:"workspace_id"`
	CreatedAt     int64  `json:"created_at"`
	SchemaVersion int    `json:"schema_version"`
}

func (c *Client) WorkspaceInit(ctx context.Context, req WorkspaceInitRequest) (*WorkspaceInitResponse, error) {
	var resp WorkspaceInitResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/init", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceListRequest struct{}

type WorkspaceListEntry struct {
	WorkspaceID   string `json:"workspace_id"`
	OwningProject string `json:"owning_project"`
	PolicyLocked  bool   `json:"policy_locked"`
	CreatedAt     int64  `json:"created_at"`
	SchemaVersion int    `json:"schema_version"`
}

type WorkspaceListResponse struct {
	Workspaces []WorkspaceListEntry `json:"workspaces"`
}

func (c *Client) WorkspaceList(ctx context.Context, req WorkspaceListRequest) (*WorkspaceListResponse, error) {
	var resp WorkspaceListResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/list", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceMembersRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceMemberRow struct {
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	RegisteredAt int64  `json:"registered_at"`
}

type WorkspaceMembersResponse struct {
	Members []WorkspaceMemberRow `json:"members"`
}

func (c *Client) WorkspaceMembers(ctx context.Context, req WorkspaceMembersRequest) (*WorkspaceMembersResponse, error) {
	var resp WorkspaceMembersResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/members", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceLinkRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
}

type WorkspaceLinkResponse struct {
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	RegisteredAt int64  `json:"registered_at"`
}

func (c *Client) WorkspaceLink(ctx context.Context, req WorkspaceLinkRequest) (*WorkspaceLinkResponse, error) {
	var resp WorkspaceLinkResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/link", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceRemoveRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceRemoveResponse struct {
	WorkspaceID  string `json:"workspace_id"`
	RowsAffected int64  `json:"rows_affected"`
}

func (c *Client) WorkspaceRemove(ctx context.Context, req WorkspaceRemoveRequest) (*WorkspaceRemoveResponse, error) {
	var resp WorkspaceRemoveResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/remove", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspacePolicyGetRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type WorkspacePolicyGetResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Policy      string `json:"policy"`
}

func (c *Client) WorkspacePolicyGet(ctx context.Context, req WorkspacePolicyGetRequest) (*WorkspacePolicyGetResponse, error) {
	var resp WorkspacePolicyGetResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/policy/get", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspacePolicySetRequest struct {
	WorkspaceID string `json:"workspace_id"`
	NewPolicy   string `json:"new_policy"`
}

type WorkspacePolicySetResponse struct {
	WorkspaceID string `json:"workspace_id"`
	NewPolicy   string `json:"new_policy"`
}

func (c *Client) WorkspacePolicySet(ctx context.Context, req WorkspacePolicySetRequest) (*WorkspacePolicySetResponse, error) {
	var resp WorkspacePolicySetResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/workspace/policy/set", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
