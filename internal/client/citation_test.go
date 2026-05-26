package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditEventResolveHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/audit/event/evt-abc123" {
			t.Errorf("path = %s, want /v1/audit/event/evt-abc123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AuditEventResolveResponse{
			ID:            "evt-abc123",
			Type:          "AugmentationCompleted",
			TessLeaf:      "deadbeef",
			ProjectAlias:  "zen-swarm",
			TimestampUnix: 1746000000,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AuditEventResolve(context.Background(), "evt-abc123")
	if err != nil {
		t.Fatalf("AuditEventResolve: %v", err)
	}
	if resp.ID != "evt-abc123" {
		t.Fatalf("ID = %q, want evt-abc123", resp.ID)
	}
	if resp.Type != "AugmentationCompleted" {
		t.Fatalf("Type = %q, want AugmentationCompleted", resp.Type)
	}
	if resp.TessLeaf != "deadbeef" {
		t.Fatalf("TessLeaf = %q, want deadbeef", resp.TessLeaf)
	}
}

func TestAuditEventResolveHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"event not found"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.AuditEventResolve(context.Background(), "no-such-event")
	if err == nil {
		t.Fatal("expected error for 404; got nil")
	}
}

func TestCitationProbeHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/citation/probe" {
			t.Errorf("path = %s, want /v1/citation/probe", r.URL.Path)
		}
		check := r.URL.Query().Get("check")
		if check != "renderer-loaded" {
			t.Errorf("check = %q, want renderer-loaded", check)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CitationProbeResp{Status: "ok", Detail: "markdown_fallback active"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CitationProbe(context.Background(), "renderer-loaded")
	if err != nil {
		t.Fatalf("CitationProbe: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want ok", resp.Status)
	}
	if resp.Detail != "markdown_fallback active" {
		t.Errorf("Detail = %q, want 'markdown_fallback active'", resp.Detail)
	}
}
