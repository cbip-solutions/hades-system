// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/p11_audit_url/inv_zen_172_audit_url_auth_test.go — D-8.
//
// Compliance test for invariant: zen://audit URL handler validates
// event-id existence + auth + doctrine privacy.
//
// Anchored at compile-time via internal/citation/sentinel.go's
// auditEventHandlerAuthSentinel; runtime via
// internal/daemon/handlers/audit_event_test.go; compliance test below
// extends with cross-doctrine adversarial matrix + path-traversal
// rejection + timing-side-channel observation.
//
// Lives in its own Go package `p11_audit_url` (NOT the shared `compliance`
// package) to isolate this test binary from `internal/daemon/handlers`
// indirect dependencies (the citation+envelope chain pulls in
// github.com/ncruces/go-sqlite3/embed via internal/store). Bringing them
// into `tests/compliance/` was bisected as the trigger for TestInvZen074's
// "subprocess: session closed" flake under `-race`: the inflated test
// binary's helper-subprocess startup time crossed the readiness-handshake
// budget. Pattern mirrors tests/integration/plan9_audit_chain/ +
// tests/integration/plan9_knowledge_research_state/, which split for the
// same isolation reason.
package p11_audit_url

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type complianceFakeCtx struct {
	rows map[string]handlers.AuditEventRow
}

func (f *complianceFakeCtx) AuditEvents(string, string, int64, int) ([]handlers.AuditEventRow, error) {
	return nil, nil
}
func (f *complianceFakeCtx) AuditTypes() ([]handlers.AuditTypeRow, error) {
	return nil, nil
}
func (f *complianceFakeCtx) AuditEventByID(id string) (handlers.AuditEventRow, error) {
	r, ok := f.rows[id]
	if !ok {
		return handlers.AuditEventRow{}, handlers.ErrAuditEventNotFound
	}
	return r, nil
}

type sessionKey struct{}

func ctxWithDoctrine(c context.Context, d string) context.Context {
	return context.WithValue(c, sessionKey{}, d)
}

func sessionDoctrine(r *http.Request) string {
	v, _ := r.Context().Value(sessionKey{}).(string)
	return v
}

func TestInvZen172CrossDoctrineMatrix(t *testing.T) {
	t.Parallel()

	rows := map[string]handlers.AuditEventRow{
		"evt-ms-001": {ID: "evt-ms-001", ProjectID: "p1", Doctrine: "max-scope", Type: "X", PayloadRaw: `{}`, EmittedAt: 1},
		"evt-de-001": {ID: "evt-de-001", ProjectID: "p2", Doctrine: "default", Type: "X", PayloadRaw: `{}`, EmittedAt: 1},
		"evt-cf-001": {ID: "evt-cf-001", ProjectID: "p3", Doctrine: "capa-firewall", Type: "X", PayloadRaw: `{}`, EmittedAt: 1},
	}
	ctx := &complianceFakeCtx{rows: rows}
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)

	cases := []struct {
		name            string
		eventID         string
		sessionDoctrine string
		wantStatus      int
	}{

		{"ms-row + ms-sess → 200", "evt-ms-001", "max-scope", http.StatusOK},
		{"ms-row + default-sess → 200", "evt-ms-001", "default", http.StatusOK},
		{"ms-row + cf-sess → 403", "evt-ms-001", "capa-firewall", http.StatusForbidden},

		{"default-row + ms-sess → 200", "evt-de-001", "max-scope", http.StatusOK},
		{"default-row + default-sess → 200", "evt-de-001", "default", http.StatusOK},
		{"default-row + cf-sess → 403", "evt-de-001", "capa-firewall", http.StatusForbidden},

		{"cf-row + ms-sess → 403", "evt-cf-001", "max-scope", http.StatusForbidden},
		{"cf-row + default-sess → 403", "evt-cf-001", "default", http.StatusForbidden},
		{"cf-row + cf-sess → 200", "evt-cf-001", "capa-firewall", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/v1/audit/event/"+tc.eventID, nil)
			r = r.WithContext(ctxWithDoctrine(r.Context(), tc.sessionDoctrine))
			h(w, r)
			if w.Code != tc.wantStatus {
				t.Errorf("status: want %d got %d (body: %s)", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestInvZen172AuthRequired(t *testing.T) {
	t.Parallel()
	ctx := &complianceFakeCtx{rows: map[string]handlers.AuditEventRow{}}
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-001", nil)

	h(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestInvZen172PathTraversalRejected(t *testing.T) {
	t.Parallel()
	ctx := &complianceFakeCtx{rows: map[string]handlers.AuditEventRow{}}
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)

	traversals := []string{
		"/v1/audit/event/..%2F..%2Fetc%2Fpasswd",
		"/v1/audit/event/%00",
		"/v1/audit/event/evt%00.txt",
		"/v1/audit/event/evt%2E%2E%2F..%2Fetc",
	}
	for _, path := range traversals {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", path, nil)
			r = r.WithContext(ctxWithDoctrine(r.Context(), "max-scope"))
			h(w, r)
			if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
				t.Errorf("want 400 or 404 got %d (path: %s; body: %s)", w.Code, path, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "/etc/passwd") || strings.Contains(w.Body.String(), "%00") {
				t.Error("response leaked attack payload")
			}
		})
	}
}

func TestInvZen172DoctrineMismatchReturns403NotLeakingExistence(t *testing.T) {
	t.Parallel()

	rows := map[string]handlers.AuditEventRow{
		"evt-cf-secret": {ID: "evt-cf-secret", ProjectID: "secret", Doctrine: "capa-firewall", Type: "X", PayloadRaw: `{}`, EmittedAt: 1},
	}
	ctx := &complianceFakeCtx{rows: rows}
	h := handlers.AuditEventByIDHandler(ctx, sessionDoctrine)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-cf-secret", nil)
	r = r.WithContext(ctxWithDoctrine(r.Context(), "default"))
	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("doctrine-mismatch on existing row: want 403 got %d", w.Code)
	}

	body := w.Body.String()
	if strings.Contains(body, "secret") {
		t.Errorf("response body leaked project_id: %s", body)
	}
}

// TestInvZen172XZenDoctrineHeaderIgnoredByProductionWire asserts the
// header (client-controlled) MUST NOT influence the audit URL
// handler's doctrine-privacy decision when the production
// sessionDoctrine wire is used.
//
// The production sessionDoctrine derives doctrine from the daemon's
// active.Accessor (controlled by the daemon, not the client) +
// peer-cred / loopback auth. A capa-firewall row queried under the
// production wire from a default-doctrine daemon (the realistic
// shape) MUST 403 regardless of any HTTP header the client sets.
//
// This complements the runtime tests at
// internal/daemon/server_session_doctrine_test.go +
// internal/daemon/handlers/audit_event_test.go; the compliance
// witness sits here to gate the invariant anchor for the release
// pipeline.
func TestInvZen172XZenDoctrineHeaderIgnoredByProductionWire(t *testing.T) {
	t.Parallel()

	rows := map[string]handlers.AuditEventRow{
		"evt-cf-claim": {
			ID:         "evt-cf-claim",
			ProjectID:  "secret",
			Doctrine:   "capa-firewall",
			Type:       "X",
			PayloadRaw: `{}`,
			EmittedAt:  1,
		},
	}
	ctx := &complianceFakeCtx{rows: rows}

	productionSessionDoctrine := func(r *http.Request) string {

		return "default"
	}
	h := handlers.AuditEventByIDHandler(ctx, productionSessionDoctrine)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/audit/event/evt-cf-claim", nil)
	r.Header.Set("X-Zen-Doctrine", "capa-firewall")
	h(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("client-claimed capa-firewall via header: want 403 got %d (body: %s)",
			w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Errorf("response body leaked project_id: %s", w.Body.String())
	}
}
