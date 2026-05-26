package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHermesProbeHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/hermes/probe" {
			t.Errorf("path = %s, want /v1/hermes/probe", r.URL.Path)
		}
		check := r.URL.Query().Get("check")
		if check != "reachable" {
			t.Errorf("check query = %q, want reachable", check)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HermesProbeResp{Status: "ok", Detail: "daemon responding"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.HermesProbe(context.Background(), "reachable")
	if err != nil {
		t.Fatalf("HermesProbe: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want ok", resp.Status)
	}
	if resp.Detail != "daemon responding" {
		t.Errorf("Detail = %q, want 'daemon responding'", resp.Detail)
	}
}

func TestHermesProbeTransportsCheckName(t *testing.T) {
	t.Parallel()
	var gotCheck string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCheck = r.URL.Query().Get("check")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HermesProbeResp{Status: "warn"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, _ = c.HermesProbe(context.Background(), "plugin-loaded")
	if gotCheck != "plugin-loaded" {
		t.Fatalf("check name = %q, want plugin-loaded", gotCheck)
	}
}

func TestHermesProbeHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"hermes not running"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.HermesProbe(context.Background(), "reachable")
	if err == nil {
		t.Fatal("expected error for 503; got nil")
	}
}
