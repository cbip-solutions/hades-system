package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFederationHealthHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/federation/health" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req FederationHealthRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationHealthResponse{
			WorkspaceID:        req.WorkspaceID,
			Reachable:          true,
			GateLatencyP95Ms:   1.2,
			ContractLinksCount: 5,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationHealth(context.Background(), FederationHealthRequest{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("FederationHealth: %v", err)
	}
	if !resp.Reachable {
		t.Errorf("Reachable = false; want true")
	}
	if resp.GateLatencyP95Ms != 1.2 {
		t.Errorf("GateLatencyP95Ms = %f", resp.GateLatencyP95Ms)
	}
}

func TestFederationHealthDaemonWide(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req FederationHealthRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.WorkspaceID != "" {
			t.Errorf("WorkspaceID = %q; want empty (daemon-wide)", req.WorkspaceID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FederationHealthResponse{Reachable: true})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.FederationHealth(context.Background(), FederationHealthRequest{})
	if err != nil {
		t.Fatalf("FederationHealth: %v", err)
	}
	if !resp.Reachable {
		t.Errorf("Reachable = false")
	}
}

func TestAPIImpactHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/api-impact" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req APIImpactRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.DiffRef != "HEAD~3..HEAD" {
			t.Errorf("DiffRef = %q", req.DiffRef)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIImpactResponse{
			DiffRef:       req.DiffRef,
			WorkspaceID:   req.WorkspaceID,
			AffectedCount: 2,
			Consumers: []APIImpactConsumer{
				{Repo: "repo-b", CallID: "call-1", Severity: "BREAKING"},
				{Repo: "repo-c", CallID: "call-2", Severity: "DANGEROUS"},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.APIImpact(context.Background(), APIImpactRequest{
		DiffRef: "HEAD~3..HEAD", WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("APIImpact: %v", err)
	}
	if resp.AffectedCount != 2 || len(resp.Consumers) != 2 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestFederationErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, err := c.FederationHealth(context.Background(), FederationHealthRequest{}); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
