package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkspaceInitHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/init" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req WorkspaceInitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceInitResponse{
			WorkspaceID:   req.WorkspaceID,
			CreatedAt:     1700000000,
			SchemaVersion: 1,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspaceInit(context.Background(), WorkspaceInitRequest{
		WorkspaceID: "ws-1", OwningProject: "proj-a", PolicyLocked: true,
	})
	if err != nil {
		t.Fatalf("WorkspaceInit: %v", err)
	}
	if resp.WorkspaceID != "ws-1" || resp.SchemaVersion != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestWorkspaceListHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/list" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceListResponse{
			Workspaces: []WorkspaceListEntry{
				{WorkspaceID: "ws-1", OwningProject: "proj-a", PolicyLocked: false, CreatedAt: 1700000000},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspaceList(context.Background(), WorkspaceListRequest{})
	if err != nil {
		t.Fatalf("WorkspaceList: %v", err)
	}
	if len(resp.Workspaces) != 1 || resp.Workspaces[0].WorkspaceID != "ws-1" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestWorkspaceMembersHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WorkspaceMembersRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.WorkspaceID != "ws-1" {
			t.Errorf("WorkspaceID = %q", req.WorkspaceID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceMembersResponse{
			Members: []WorkspaceMemberRow{
				{WorkspaceID: "ws-1", ProjectID: "proj-a", RegisteredAt: 1700000000},
				{WorkspaceID: "ws-1", ProjectID: "proj-b", RegisteredAt: 1700000100},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspaceMembers(context.Background(), WorkspaceMembersRequest{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("WorkspaceMembers: %v", err)
	}
	if len(resp.Members) != 2 {
		t.Errorf("Members count = %d; want 2", len(resp.Members))
	}
}

func TestWorkspaceLinkHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/link" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req WorkspaceLinkRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceLinkResponse{
			WorkspaceID: req.WorkspaceID, ProjectID: req.ProjectID, RegisteredAt: 1700000200,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspaceLink(context.Background(), WorkspaceLinkRequest{WorkspaceID: "ws-1", ProjectID: "proj-c"})
	if err != nil {
		t.Fatalf("WorkspaceLink: %v", err)
	}
	if resp.ProjectID != "proj-c" {
		t.Errorf("ProjectID = %q", resp.ProjectID)
	}
}

func TestWorkspaceRemoveHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/remove" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req WorkspaceRemoveRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceRemoveResponse{
			WorkspaceID: req.WorkspaceID, RowsAffected: 1,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspaceRemove(context.Background(), WorkspaceRemoveRequest{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("WorkspaceRemove: %v", err)
	}
	if resp.RowsAffected != 1 {
		t.Errorf("RowsAffected = %d", resp.RowsAffected)
	}
}

func TestWorkspacePolicyGetHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/policy/get" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspacePolicyGetResponse{
			WorkspaceID: "ws-1", Policy: `{"locked":true}`,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspacePolicyGet(context.Background(), WorkspacePolicyGetRequest{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("WorkspacePolicyGet: %v", err)
	}
	if resp.Policy != `{"locked":true}` {
		t.Errorf("Policy = %q", resp.Policy)
	}
}

func TestWorkspacePolicySetHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/workspace/policy/set" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req WorkspacePolicySetRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.NewPolicy != "locked" {
			t.Errorf("NewPolicy = %q", req.NewPolicy)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspacePolicySetResponse{
			WorkspaceID: req.WorkspaceID, NewPolicy: req.NewPolicy,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.WorkspacePolicySet(context.Background(), WorkspacePolicySetRequest{
		WorkspaceID: "ws-1", NewPolicy: "locked",
	})
	if err != nil {
		t.Fatalf("WorkspacePolicySet: %v", err)
	}
	if resp.NewPolicy != "locked" {
		t.Errorf("NewPolicy = %q", resp.NewPolicy)
	}
}

func TestWorkspaceErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, err := c.WorkspaceList(context.Background(), WorkspaceListRequest{}); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
