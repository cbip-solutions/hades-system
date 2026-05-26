package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCaronteProbeCtx struct {
	bgeAvailable bool
}

func (f fakeCaronteProbeCtx) BGEAvailable() bool { return f.bgeAvailable }

func TestCaronteProbeHandler_RerankAvailable_True(t *testing.T) {
	ctx := fakeCaronteProbeCtx{bgeAvailable: true}
	h := CaronteProbeHandler(ctx)
	req := httptest.NewRequest(http.MethodGet, "/v1/caronte/probe?check=rerank.available", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp CaronteProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if !strings.Contains(resp.Detail, "BGE reranker active") {
		t.Errorf("detail = %q, want to contain 'BGE reranker active'", resp.Detail)
	}
}

func TestCaronteProbeHandler_RerankAvailable_False(t *testing.T) {
	ctx := fakeCaronteProbeCtx{bgeAvailable: false}
	h := CaronteProbeHandler(ctx)
	req := httptest.NewRequest(http.MethodGet, "/v1/caronte/probe?check=rerank.available", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp CaronteProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "warn" {
		t.Errorf("status = %q, want warn (degraded-default contract)", resp.Status)
	}
	if !strings.Contains(resp.Detail, "scripts/download-bge-model.sh") {
		t.Errorf("detail = %q, want to reference scripts/download-bge-model.sh", resp.Detail)
	}
}

func TestCaronteProbeHandler_KnownPlan19Probes(t *testing.T) {
	ctx := fakeCaronteProbeCtx{bgeAvailable: false}
	h := CaronteProbeHandler(ctx)
	for _, check := range []string{"engine.healthy", "index.freshness", "language.coverage", "project-db.status"} {
		t.Run(check, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/caronte/probe?check="+check, nil)
			rec := httptest.NewRecorder()
			h(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var resp CaronteProbeResp
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Status != "ok" {
				t.Errorf("status = %q, want ok", resp.Status)
			}
		})
	}
}

func TestCaronteProbeHandler_UnknownCheck(t *testing.T) {
	ctx := fakeCaronteProbeCtx{}
	h := CaronteProbeHandler(ctx)
	req := httptest.NewRequest(http.MethodGet, "/v1/caronte/probe?check=does-not-exist", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp CaronteProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Detail, "unknown check name") {
		t.Errorf("detail = %q, want to contain 'unknown check name'", resp.Detail)
	}
}

func TestCaronteProbeHandler_EmptyCheck(t *testing.T) {
	ctx := fakeCaronteProbeCtx{}
	h := CaronteProbeHandler(ctx)
	req := httptest.NewRequest(http.MethodGet, "/v1/caronte/probe", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp CaronteProbeResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Detail, "no check specified") {
		t.Errorf("detail = %q, want to contain 'no check specified'", resp.Detail)
	}
}

func TestCaronteProbeHandler_NonGET(t *testing.T) {
	ctx := fakeCaronteProbeCtx{}
	h := CaronteProbeHandler(ctx)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/v1/caronte/probe?check=rerank.available", nil)
			rec := httptest.NewRecorder()
			h(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s status = %d, want 405", method, rec.Code)
			}
		})
	}
}
