package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type auditEmitServer struct {
	mu     sync.Mutex
	events []AuditEventIn
}

func (s *auditEmitServer) AuditEmit(event AuditEventIn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func TestAuditEmit_Single(t *testing.T) {
	srv := &auditEmitServer{}
	h := AuditEmit(srv)
	body := map[string]any{
		"project_id": "internal-platform-x",
		"type":       "sshexec.started",
		"payload":    map[string]string{"host": "vps", "cmd": "alembic upgrade head"},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(srv.events))
	}
	ev := srv.events[0]
	if ev.ProjectID != "internal-platform-x" {
		t.Errorf("project_id: got %q", ev.ProjectID)
	}
	if ev.Type != "sshexec.started" {
		t.Errorf("type: got %q", ev.Type)
	}
	if ev.EmittedAt == 0 {
		t.Error("emitted_at must be set by handler")
	}
}

func TestAuditEmit_MissingType(t *testing.T) {
	srv := &auditEmitServer{}
	h := AuditEmit(srv)
	body := map[string]any{"project_id": "proj", "payload": map[string]string{}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing type, got %d", w.Code)
	}
}

func TestAuditEmit_InvalidJSON(t *testing.T) {
	srv := &auditEmitServer{}
	h := AuditEmit(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit",
		bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestAuditEmit_IDGenerated(t *testing.T) {
	srv := &auditEmitServer{}
	h := AuditEmit(srv)
	body := map[string]any{"project_id": "p", "type": "test.event", "payload": map[string]string{}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	id, _ := resp["id"].(string)
	if id == "" {
		t.Error("response must include id")
	}
}

func TestAuditEmit_EmittedAtIsNow(t *testing.T) {
	srv := &auditEmitServer{}
	h := AuditEmit(srv)
	before := time.Now().Unix()
	body := map[string]any{"project_id": "p", "type": "t", "payload": map[string]string{}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit/emit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	after := time.Now().Unix()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.events) != 1 {
		t.Fatal("no event")
	}
	ev := srv.events[0]
	if ev.EmittedAt < before || ev.EmittedAt > after {
		t.Errorf("emitted_at %d not in [%d, %d]", ev.EmittedAt, before, after)
	}
}
