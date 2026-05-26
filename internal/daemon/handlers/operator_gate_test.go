package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type operatorGateServer struct {
	state string
}

func (g *operatorGateServer) OperatorGateState() (string, error) {
	if g.state == "" {
		g.state = "running"
	}
	return g.state, nil
}

func (g *operatorGateServer) OperatorGatePause(mode, reason string) (string, error) {

	if g.state != "running" {
		return g.state, nil
	}
	if mode == "" {
		mode = "paused_descriptive"
	}
	g.state = mode
	return g.state, nil
}

func (g *operatorGateServer) OperatorGateResume() (string, error) {
	g.state = "running"
	return g.state, nil
}

func TestOperatorGateState_Running(t *testing.T) {
	srv := &operatorGateServer{state: "running"}
	h := OperatorGateState(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/gate/state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["state"] != "running" {
		t.Errorf("state: got %v", resp["state"])
	}
}

func TestOperatorGatePause_FromRunning(t *testing.T) {
	srv := &operatorGateServer{state: "running"}
	h := OperatorGatePause(srv)
	body := map[string]string{"mode": "paused_descriptive", "reason": "operator test"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.state != "paused_descriptive" {
		t.Errorf("state not updated: %q", srv.state)
	}
}

func TestOperatorGatePause_Idempotent(t *testing.T) {

	srv := &operatorGateServer{state: "paused_quiet"}
	h := OperatorGatePause(srv)
	body := map[string]string{"mode": "paused_descriptive", "reason": "second call"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (idempotent), got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["state"] != "paused_quiet" {
		t.Errorf("idempotent: state should remain paused_quiet, got %v", resp["state"])
	}
}

func TestOperatorGatePause_DefaultMode(t *testing.T) {

	srv := &operatorGateServer{state: "running"}
	h := OperatorGatePause(srv)
	body := map[string]string{"reason": "no mode supplied"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOperatorGateResume_OK(t *testing.T) {
	srv := &operatorGateServer{state: "paused_quiet"}
	h := OperatorGateResume(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/resume", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.state != "running" {
		t.Errorf("state not running after resume: %q", srv.state)
	}
}

func TestOperatorGatePause_MalformedJSON(t *testing.T) {
	srv := &operatorGateServer{state: "running"}
	h := OperatorGatePause(srv)
	body := `{"mode":`
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/pause", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("MalformedJSON: got status %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOperatorGateResume_AlreadyRunning(t *testing.T) {

	srv := &operatorGateServer{state: "running"}
	h := OperatorGateResume(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/workforce/gate/resume", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}
