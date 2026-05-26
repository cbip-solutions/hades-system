package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKnowledgePromoteHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/promote" {
			t.Errorf("path = %s, want /v1/knowledge/promote", r.URL.Path)
		}
		var req KnowledgePromoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.ID != "doc-xyz" {
			t.Errorf("ID = %q, want doc-xyz", req.ID)
		}
		if !req.GlobalScope {
			t.Errorf("GlobalScope = false, want true")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgePromoteResponse{
			ID:     req.ID,
			Status: "promoted",
			Scope:  "global",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.KnowledgePromote(context.Background(), KnowledgePromoteRequest{
		ID:          "doc-xyz",
		GlobalScope: true,
	})
	if err != nil {
		t.Fatalf("KnowledgePromote: %v", err)
	}
	if resp.ID != "doc-xyz" {
		t.Fatalf("ID = %q, want doc-xyz", resp.ID)
	}
	if resp.Status != "promoted" {
		t.Fatalf("Status = %q, want promoted", resp.Status)
	}
	if resp.Scope != "global" {
		t.Fatalf("Scope = %q, want global", resp.Scope)
	}
}

func TestKnowledgePromoteHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"invalid id"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgePromote(context.Background(), KnowledgePromoteRequest{ID: ""})
	if err == nil {
		t.Fatal("expected error for 422; got nil")
	}
}

func TestKnowledgeSyncHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/sync" {
			t.Errorf("path = %s, want /v1/knowledge/sync", r.URL.Path)
		}
		var req KnowledgeSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.ProjectAlias != "internal-platform-x" {
			t.Errorf("ProjectAlias = %q, want internal-platform-x", req.ProjectAlias)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeSyncResponse{
			RowsIndexed: 200,
			DurationMs:  450,
			VerifyDelta: 3,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.KnowledgeSync(context.Background(), KnowledgeSyncRequest{
		ProjectAlias: "internal-platform-x",
		Verify:       true,
	})
	if err != nil {
		t.Fatalf("KnowledgeSync: %v", err)
	}
	if resp.RowsIndexed != 200 {
		t.Fatalf("RowsIndexed = %d, want 200", resp.RowsIndexed)
	}
	if resp.DurationMs != 450 {
		t.Fatalf("DurationMs = %d, want 450", resp.DurationMs)
	}
	if resp.VerifyDelta != 3 {
		t.Fatalf("VerifyDelta = %d, want 3", resp.VerifyDelta)
	}
}

func TestKnowledgeSyncHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"daemon syncing"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeSync(context.Background(), KnowledgeSyncRequest{})
	if err == nil {
		t.Fatal("expected error for 503; got nil")
	}
}

func TestKnowledgeRestoreHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/restore" {
			t.Errorf("path = %s, want /v1/knowledge/restore", r.URL.Path)
		}
		var req KnowledgeRestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.ProjectAlias != "zen-swarm" {
			t.Errorf("ProjectAlias = %q, want zen-swarm", req.ProjectAlias)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeRestoreResponse{
			ProjectAlias: req.ProjectAlias,
			SnapshotID:   "snap-2026-05-01T08:00:00Z",
			RowsRestored: 5000,
			DurationMs:   1200,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.KnowledgeRestore(context.Background(), KnowledgeRestoreRequest{
		ProjectAlias: "zen-swarm",
	})
	if err != nil {
		t.Fatalf("KnowledgeRestore: %v", err)
	}
	if resp.ProjectAlias != "zen-swarm" {
		t.Fatalf("ProjectAlias = %q, want zen-swarm", resp.ProjectAlias)
	}
	if resp.SnapshotID != "snap-2026-05-01T08:00:00Z" {
		t.Fatalf("SnapshotID = %q", resp.SnapshotID)
	}
	if resp.RowsRestored != 5000 {
		t.Fatalf("RowsRestored = %d, want 5000", resp.RowsRestored)
	}
}

func TestKnowledgeRestoreHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"snapshot not found"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeRestore(context.Background(), KnowledgeRestoreRequest{
		ProjectAlias: "no-such-project",
		Timestamp:    "2020-01-01T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error for 404; got nil")
	}
}
