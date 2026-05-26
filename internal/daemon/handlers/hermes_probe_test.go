package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeHermesCtx struct {
	sessions int
	orch     any
}

func (f *fakeHermesCtx) HermesActiveSessions() int { return f.sessions }
func (f *fakeHermesCtx) Orchestrator() any         { return f.orch }

func TestHermesProbePluginInstalled(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=plugin_installed", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "install-mcps") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeSessionActiveNoSessions(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{sessions: 0})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=session_active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "warn" {
		t.Errorf("Status = %q, want warn", resp.Status)
	}
	if !strings.Contains(resp.Detail, "no active Hermes session") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeSessionActiveOne(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{sessions: 1})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=session_active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "1 active") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeSessionActiveMany(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{sessions: 4})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=session_active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "multiple") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeTransportReachableWired(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{orch: struct{}{}})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=transport_reachable", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
}

func TestHermesProbeTransportReachableNotWired(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{orch: nil})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=transport_reachable", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "warn" {
		t.Errorf("Status = %q, want warn", resp.Status)
	}
}

func TestHermesProbeUnknownCheck(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe?check=mystery", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "unknown") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeEmptyCheck(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{})
	r := httptest.NewRequest(http.MethodGet, "/v1/hermes/probe", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.HermesProbeResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "ok" {
		t.Errorf("Status = %q", resp.Status)
	}
	if !strings.Contains(resp.Detail, "no check specified") {
		t.Errorf("Detail = %q", resp.Detail)
	}
}

func TestHermesProbeMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.HermesProbeHandler(&fakeHermesCtx{})
	r := httptest.NewRequest(http.MethodPost, "/v1/hermes/probe", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
