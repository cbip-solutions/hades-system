package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeCitationProbeCtx struct{ has bool }

func (f *fakeCitationProbeCtx) HasAuditEventRoute() bool { return f.has }

func TestCitationProbe_AuditHandlerFunctional_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/citation/probe?check=audit-handler-functional", nil)
	rec := httptest.NewRecorder()
	handlers.CitationProbeHandler(&fakeCitationProbeCtx{has: true}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var resp handlers.CitationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("resp.Status = %q, want ok", resp.Status)
	}
}

func TestCitationProbe_AuditHandlerFunctional_Fail(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/citation/probe?check=audit-handler-functional", nil)
	rec := httptest.NewRecorder()
	handlers.CitationProbeHandler(&fakeCitationProbeCtx{has: false}).ServeHTTP(rec, req)
	var resp handlers.CitationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "fail" {
		t.Errorf("resp.Status = %q, want fail (audit not wired)", resp.Status)
	}
}

func TestCitationProbe_UnknownCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/citation/probe?check=garbage", nil)
	rec := httptest.NewRecorder()
	handlers.CitationProbeHandler(&fakeCitationProbeCtx{has: true}).ServeHTTP(rec, req)
	var resp handlers.CitationProbeResp
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

func TestCitationProbe_EmptyCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/citation/probe", nil)
	rec := httptest.NewRecorder()
	handlers.CitationProbeHandler(&fakeCitationProbeCtx{has: true}).ServeHTTP(rec, req)
	var resp handlers.CitationProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("empty check should return ok with hint (got %q)", resp.Status)
	}
}

func TestCitationProbe_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/citation/probe", nil)
	rec := httptest.NewRecorder()
	handlers.CitationProbeHandler(&fakeCitationProbeCtx{has: true}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
