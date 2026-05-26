package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

func TestCoordinationProbe_Plan9DSubstrate_OK(t *testing.T) {
	tmp := t.TempDir()
	aggDir := filepath.Join(tmp, "internal", "knowledge", "aggregator")
	if err := os.MkdirAll(aggDir, 0o755); err != nil {
		t.Fatalf("mkdir aggregator: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(aggDir, "aggregator.go"),
		[]byte("package aggregator\n"),
		0o644,
	); err != nil {
		t.Fatalf("write aggregator.go: %v", err)
	}
	t.Setenv("ZEN_SWARM_REPO_ROOT", tmp)

	req := httptest.NewRequest(http.MethodGet, "/v1/coordination/probe?check=plan-9-d-substrate", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var resp handlers.CoordinationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q, want ok (detail: %s)", resp.Status, resp.Detail)
	}
}

func TestCoordinationProbe_Plan9DSubstrate_Missing(t *testing.T) {
	t.Setenv("ZEN_SWARM_REPO_ROOT", t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/v1/coordination/probe?check=plan-9-d-substrate", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)
	var resp handlers.CoordinationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "fail" {
		t.Errorf("resp.Status = %q, want fail (artifact missing)", resp.Status)
	}
}

func TestCoordinationProbe_Plan1HPrimeExecuted_Retired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/coordination/probe?check=plan-1-h-prime-executed", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)
	var resp handlers.CoordinationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("retired probe should hit default branch (status=ok); got %q (detail: %s)", resp.Status, resp.Detail)
	}

	if resp.Detail == "" || !contains(resp.Detail, "unknown check name") {
		t.Errorf("retired probe should return unknown-check hint; got detail=%q", resp.Detail)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestCoordinationProbe_UnknownCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/coordination/probe?check=garbage", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)
	var resp handlers.CoordinationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("unknown check should still return ok with hint (got %q)", resp.Status)
	}
	if resp.Detail == "" {
		t.Errorf("unknown check should include a hint in detail")
	}
}

func TestCoordinationProbe_EmptyCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/coordination/probe", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)
	var resp handlers.CoordinationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("empty check should return ok with hint (got %q)", resp.Status)
	}
}

func TestCoordinationProbe_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/coordination/probe", nil)
	rec := httptest.NewRecorder()
	handlers.CoordinationProbeHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
