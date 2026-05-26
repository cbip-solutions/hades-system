// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/daemon/handlers/audit_event_test.go — Plan 11 Phase D Task D-5.
//
// Tests for the zen://audit URL handler: auth-required, doctrine privacy
// filter, path validation, 401/403/404/200 paths.
package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeAuditCtx struct {
	rows map[string]handlers.AuditEventRow
}

func (f *fakeAuditCtx) AuditEvents(typePrefix, projectID string, sinceUnix int64, limit int) ([]handlers.AuditEventRow, error) {
	return nil, nil
}

func (f *fakeAuditCtx) AuditTypes() ([]handlers.AuditTypeRow, error) {
	return nil, nil
}

func (f *fakeAuditCtx) AuditEventByID(id string) (handlers.AuditEventRow, error) {
	row, ok := f.rows[id]
	if !ok {
		return handlers.AuditEventRow{}, handlers.ErrAuditEventNotFound
	}
	return row, nil
}

func newFakeAuditCtx(rows ...handlers.AuditEventRow) *fakeAuditCtx {
	m := make(map[string]handlers.AuditEventRow)
	for _, r := range rows {
		m[r.ID] = r
	}
	return &fakeAuditCtx{rows: m}
}

type sessionKey struct{}

func ctxWithSession(ctx context.Context, doctrine string) context.Context {
	return context.WithValue(ctx, sessionKey{}, doctrine)
}

func sessionDoctrine(r *http.Request) string {
	if v := r.Context().Value(sessionKey{}); v != nil {
		return v.(string)
	}
	return ""
}

func TestAuditEventHandlerHappyPath(t *testing.T) {
	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID:         "evt-0001",
		ProjectID:  "internal-platform-x",
		Type:       "AugmentationCompleted",
		Doctrine:   "max-scope",
		PayloadRaw: `{"tokens":1024}`,
		EmittedAt:  1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-0001", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: want 200 got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Envelope citation.Envelope      `json:"envelope"`
		Row      handlers.AuditEventRow `json:"row"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Row.ID != "evt-0001" {
		t.Errorf("row.ID: want evt-0001 got %s", resp.Row.ID)
	}
	if resp.Envelope.AuditEventID != "evt-0001" {
		t.Errorf("envelope.AuditEventID: want evt-0001 got %s", resp.Envelope.AuditEventID)
	}
}

func TestAuditEventHandler401NoSession(t *testing.T) {
	ctx := newFakeAuditCtx()
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-0001", nil)

	h(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401 got %d", w.Code)
	}
}

func TestAuditEventHandler404NotFound(t *testing.T) {
	ctx := newFakeAuditCtx()
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-missing", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: want 404 got %d", w.Code)
	}
}

func TestAuditEventHandler403DoctrineMismatch(t *testing.T) {

	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID:         "evt-cf-0001",
		ProjectID:  "secret-proj",
		Type:       "AugmentationCompleted",
		Doctrine:   "capa-firewall",
		PayloadRaw: `{}`,
		EmittedAt:  1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-cf-0001", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "default"))

	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status: want 403 got %d (body: %s)", w.Code, w.Body.String())
	}

	if strings.Contains(w.Body.String(), "secret-proj") {
		t.Errorf("response body leaked project_id on 403: %s", w.Body.String())
	}
}

func TestAuditEventHandlerCapaFirewallAllowsCapaFirewall(t *testing.T) {
	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-cf-0002", ProjectID: "p", Type: "X", Doctrine: "capa-firewall",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-cf-0002", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "capa-firewall"))

	h(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: want 200 got %d", w.Code)
	}
}

func TestAuditEventHandlerMaxScopeVisibility(t *testing.T) {
	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-ms-0001", ProjectID: "p", Type: "X", Doctrine: "max-scope",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-ms-0001", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "default"))

	h(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("max-scope event from default session: want 200 got %d", w.Code)
	}
}

func TestAuditEventHandlerInvalidPath(t *testing.T) {
	ctx := newFakeAuditCtx()
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 got %d", w.Code)
	}
}

func TestAuditEventHandlerTrailingSlash(t *testing.T) {
	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-001", ProjectID: "p", Type: "X", Doctrine: "max-scope",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-001/", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("trailing slash should resolve: want 200 got %d", w.Code)
	}
}

func TestAuditEventHandlerInvalidIDChars(t *testing.T) {
	ctx := newFakeAuditCtx()
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)

	traversals := []string{
		"/v1/audit/event/..%2F..%2Fetc%2Fpasswd",
		"/v1/audit/event/evt$invalid",
		"/v1/audit/event/evt%20with%20spaces",
	}
	for _, p := range traversals {
		t.Run(p, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", p, nil)
			r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))
			h(w, r)

			if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
				t.Errorf("status: want 400 or 404 got %d (path-traversal attempt should be rejected)", w.Code)
			}
			body := w.Body.String()
			if strings.Contains(body, "/etc/passwd") {
				t.Error("response leaked path-traversal payload")
			}
		})
	}
}

func TestAuditEventHandlerInternalDBError(t *testing.T) {

	failingCtx := &failingAuditCtx{err: errors.New("disk full")}
	h := handlers.AuditEventByIDHandler(failingCtx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-001", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500 got %d", w.Code)
	}
}

type failingAuditCtx struct {
	err error
}

func (f *failingAuditCtx) AuditEvents(_, _ string, _ int64, _ int) ([]handlers.AuditEventRow, error) {
	return nil, nil
}
func (f *failingAuditCtx) AuditTypes() ([]handlers.AuditTypeRow, error) {
	return nil, nil
}
func (f *failingAuditCtx) AuditEventByID(_ string) (handlers.AuditEventRow, error) {
	return handlers.AuditEventRow{}, f.err
}

func TestAuditEventHandlerDefaultRowVisibleToMaxScope(t *testing.T) {
	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-def-0001", ProjectID: "p", Type: "X", Doctrine: "default",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-def-0001", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("default row from max-scope session: want 200 got %d", w.Code)
	}
}

func TestAuditEventHandlerEmptyProjectIDFailsEnvelopeConstruction(t *testing.T) {

	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-noproj", ProjectID: "", Type: "X", Doctrine: "max-scope",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-noproj", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "max-scope"))

	h(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("empty project_id should fail envelope construction: want 500 got %d", w.Code)
	}
}

func TestAuditEventHandlerCapaFirewallSessionRejectsDefaultRow(t *testing.T) {

	ctx := newFakeAuditCtx(handlers.AuditEventRow{
		ID: "evt-def-0002", ProjectID: "p", Type: "X", Doctrine: "default",
		PayloadRaw: `{}`, EmittedAt: 1715299200,
	})
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-def-0002", nil)
	r = r.WithContext(ctxWithSession(r.Context(), "capa-firewall"))

	h(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("capa-firewall session viewing default row: want 403 got %d", w.Code)
	}
}
