package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFederationRecentBreakingChangesHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/federation/recent-breaking-changes" {
			t.Errorf("path = %s; want /v1/mcpgateway/federation/recent-breaking-changes", r.URL.Path)
		}
		var req FederationRecentBreakingChangesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationRecentBreakingChangesResponse{
			WorkspaceID: req.WorkspaceID,
			Changes: []FederationBreakingChangeEntry{
				{
					ChangeID:      "ch-1",
					WorkspaceID:   req.WorkspaceID,
					Kind:          "removed_field",
					LoreAuthor:    "alice",
					LoreCommitSHA: "deadbeef1234",
				},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationRecentBreakingChanges(context.Background(), FederationRecentBreakingChangesRequest{WorkspaceID: "ws-1", Limit: 5})
	if err != nil {
		t.Fatalf("FederationRecentBreakingChanges: %v", err)
	}
	if len(resp.Changes) != 1 || resp.Changes[0].ChangeID != "ch-1" {
		t.Errorf("Changes = %+v; want one entry with ChangeID=ch-1", resp.Changes)
	}
	if resp.Changes[0].LoreAuthor != "alice" {
		t.Errorf("LoreAuthor = %q; want alice", resp.Changes[0].LoreAuthor)
	}
}

func TestFederationRecentBreakingChangesEmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationRecentBreakingChangesResponse{
			WorkspaceID: "ws-empty",
			Changes:     []FederationBreakingChangeEntry{},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationRecentBreakingChanges(context.Background(), FederationRecentBreakingChangesRequest{})
	if err != nil {
		t.Fatalf("FederationRecentBreakingChanges: %v", err)
	}
	if len(resp.Changes) != 0 {
		t.Errorf("expected empty Changes; got %+v", resp.Changes)
	}
}

func TestFederationRecentDispatchesHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/federation/recent-dispatches" {
			t.Errorf("path = %s; want /v1/mcpgateway/federation/recent-dispatches", r.URL.Path)
		}
		var req FederationRecentDispatchesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationRecentDispatchesResponse{
			Decisions: []FederationDispatchDecisionEntry{
				{
					ChangeID:        "ch-1",
					Mode:            "Surface",
					DispatchedRepos: []string{"repo-a"},
					AuditID:         "leaf-7",
				},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationRecentDispatches(context.Background(), FederationRecentDispatchesRequest{Limit: 10})
	if err != nil {
		t.Fatalf("FederationRecentDispatches: %v", err)
	}
	if len(resp.Decisions) != 1 || resp.Decisions[0].ChangeID != "ch-1" || resp.Decisions[0].Mode != "Surface" {
		t.Errorf("Decisions = %+v; want one entry with ChangeID=ch-1, Mode=Surface", resp.Decisions)
	}
}

func TestFederationRecentDispatchesEmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationRecentDispatchesResponse{
			Decisions: []FederationDispatchDecisionEntry{},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationRecentDispatches(context.Background(), FederationRecentDispatchesRequest{})
	if err != nil {
		t.Fatalf("FederationRecentDispatches: %v", err)
	}
	if len(resp.Decisions) != 0 {
		t.Errorf("expected empty Decisions; got %+v", resp.Decisions)
	}
}
