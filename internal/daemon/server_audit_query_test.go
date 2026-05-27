// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/daemon/server_audit_query_test.go — fix-cycle
// for Critical-1 (extractDoctrineFromPayload fails OPEN) + Critical-2
// (Server.AuditEventByID + extractDoctrineFromPayload 0% covered).
//
// Tests in this file exercise the daemon-side production wiring of
// invariant doctrine privacy filtering through real SQL inserts into
// audit_events_raw + the canonical extractor.
//
// Fail-closed contract (Option A, post-review): when payload_json is
// malformed JSON, or is missing the "doctrine" field, or carries an
// unrecognised doctrine value, extractDoctrineFromPayload returns
// "capa-firewall" — the most-restrictive setting — so the visibility
// predicate denies cross-doctrine reads against legacy / corrupted
// rows. invariant corollary: any ambiguity at the payload-parse
// layer is resolved on the side of secrecy.
package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func newAuditTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "audit_query_test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func seedAuditEvent(t *testing.T, s *store.Store, id, projectID, evtType, payload string, emittedAt int64) {
	t.Helper()
	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, projectID, evtType, payload, emittedAt,
	)
	if err != nil {
		t.Fatalf("seed audit_events_raw: %v", err)
	}
}

func TestExtractDoctrineFromPayloadFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		want    string
	}{

		{"max-scope", `{"doctrine":"max-scope"}`, "max-scope"},
		{"default", `{"doctrine":"default"}`, "default"},
		{"capa-firewall", `{"doctrine":"capa-firewall"}`, "capa-firewall"},
		{"valid doctrine with extra fields",
			`{"doctrine":"max-scope","tokens":1024,"x":"y"}`, "max-scope"},

		{"empty string", ``, "capa-firewall"},
		{"empty JSON object", `{}`, "capa-firewall"},
		{"null payload", `null`, "capa-firewall"},
		{"explicit null doctrine", `{"doctrine":null}`, "capa-firewall"},
		{"explicit empty doctrine", `{"doctrine":""}`, "capa-firewall"},
		{"malformed JSON (truncated)", `{"doctrine":"max-scope"`, "capa-firewall"},
		{"malformed JSON (random bytes)", `not json at all`, "capa-firewall"},
		{"malformed JSON (object close only)", `}`, "capa-firewall"},
		{"wrong type (number)", `{"doctrine":42}`, "capa-firewall"},
		{"wrong type (array)", `{"doctrine":["max-scope"]}`, "capa-firewall"},
		{"unrecognised doctrine", `{"doctrine":"super-secret-mode"}`, "capa-firewall"},
		{"case mismatch (capital)", `{"doctrine":"Max-Scope"}`, "capa-firewall"},
		{"whitespace padded", `{"doctrine":" max-scope "}`, "capa-firewall"},
		{"different field (typo)", `{"doctrina":"max-scope"}`, "capa-firewall"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDoctrineFromPayload(tc.payload)
			if got != tc.want {
				t.Errorf("extractDoctrineFromPayload(%q): want %q, got %q",
					tc.payload, tc.want, got)
			}
		})
	}
}

func TestServerAuditEventByIDHappyPath(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	seedAuditEvent(t, st, "evt-200", "internal-platform-x", "AugmentationCompleted",
		`{"doctrine":"max-scope","tokens":1024}`, time.Now().Unix())

	row, err := s.AuditEventByID("evt-200")
	if err != nil {
		t.Fatalf("AuditEventByID: %v", err)
	}
	if row.ID != "evt-200" {
		t.Errorf("ID: want evt-200 got %s", row.ID)
	}
	if row.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectID: want internal-platform-x got %s", row.ProjectID)
	}
	if row.Type != "AugmentationCompleted" {
		t.Errorf("Type: want AugmentationCompleted got %s", row.Type)
	}
	if row.Doctrine != "max-scope" {
		t.Errorf("Doctrine: want max-scope got %s", row.Doctrine)
	}
}

func TestServerAuditEventByIDNotFound(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	_, err := s.AuditEventByID("evt-does-not-exist")
	if err != handlers.ErrAuditEventNotFound {
		t.Errorf("missing row: want ErrAuditEventNotFound got %v", err)
	}
}

func TestServerAuditEventByIDNilStore(t *testing.T) {
	t.Parallel()
	s := &Server{store: nil}
	_, err := s.AuditEventByID("evt-any")
	if err != handlers.ErrAuditEventNotFound {
		t.Errorf("nil store: want ErrAuditEventNotFound got %v", err)
	}
}

func TestServerAuditEventByIDExtractsDoctrineFromRealPayload(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	cases := []struct {
		name    string
		id      string
		payload string
		want    string
	}{
		{"valid max-scope", "evt-ms", `{"doctrine":"max-scope"}`, "max-scope"},
		{"valid default", "evt-de", `{"doctrine":"default"}`, "default"},
		{"valid capa-firewall", "evt-cf", `{"doctrine":"capa-firewall"}`, "capa-firewall"},
		{"missing field → fail closed",
			"evt-bare", `{"tokens":1024}`, "capa-firewall"},
		{"malformed → fail closed",
			"evt-bad", `not json`, "capa-firewall"},
		{"unknown doctrine → fail closed",
			"evt-unk", `{"doctrine":"future-doctrine"}`, "capa-firewall"},
	}
	for _, tc := range cases {
		seedAuditEvent(t, st, tc.id, "p", "X", tc.payload, time.Now().Unix())
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, err := s.AuditEventByID(tc.id)
			if err != nil {
				t.Fatalf("AuditEventByID(%s): %v", tc.id, err)
			}
			if row.Doctrine != tc.want {
				t.Errorf("Doctrine: want %q got %q (payload=%q)",
					tc.want, row.Doctrine, tc.payload)
			}
		})
	}
}

func TestServerAuditEventsPopulatesDoctrineFromRealPayload(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	now := time.Now().Unix()
	seedAuditEvent(t, st, "evt-list-1", "p", "X",
		`{"doctrine":"max-scope"}`, now)
	seedAuditEvent(t, st, "evt-list-2", "p", "X",
		`{"doctrine":"default"}`, now+1)
	seedAuditEvent(t, st, "evt-list-3", "p", "X",
		`{"doctrine":"capa-firewall"}`, now+2)
	seedAuditEvent(t, st, "evt-list-4", "p", "X", `not json`, now+3)

	rows, err := s.AuditEvents("", "p", 0, 100)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows: want 4 got %d", len(rows))
	}

	want := map[string]string{
		"evt-list-1": "max-scope",
		"evt-list-2": "default",
		"evt-list-3": "capa-firewall",
		"evt-list-4": "capa-firewall",
	}
	for _, r := range rows {
		if got, ok := want[r.ID]; !ok || r.Doctrine != got {
			t.Errorf("row %s: Doctrine want %q got %q", r.ID, got, r.Doctrine)
		}
	}
}

func TestServerAuditEventsNilStore(t *testing.T) {
	t.Parallel()
	s := &Server{store: nil}
	rows, err := s.AuditEvents("", "", 0, 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want empty rows got %d", len(rows))
	}
}

// TestServerAuditEventsQueryError closes the store mid-test to trigger
// the Query error branch of AuditEvents (line 73 — `if err != nil
// return nil, err`). Defends the invariant read-path: a DB error
// MUST surface to the handler as a 5xx, not be swallowed silently.
func TestServerAuditEventsQueryError(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	if err := st.DB().Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	_, err := s.AuditEvents("", "", 0, 10)
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

func TestServerAuditEventByIDQueryError(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}
	seedAuditEvent(t, st, "evt-x", "p", "X", `{"doctrine":"max-scope"}`, time.Now().Unix())
	if err := st.DB().Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	_, err := s.AuditEventByID("evt-x")
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
	if err == handlers.ErrAuditEventNotFound {
		t.Fatalf("want non-ErrAuditEventNotFound error from closed DB, got %v", err)
	}
}

func TestServerAuditEventsFilteredByPrefix(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	now := time.Now().Unix()
	seedAuditEvent(t, st, "evt-a", "p", "Augment.Started", `{"doctrine":"max-scope"}`, now)
	seedAuditEvent(t, st, "evt-b", "p", "Augment.Completed", `{"doctrine":"max-scope"}`, now+1)
	seedAuditEvent(t, st, "evt-c", "p", "Doctrine.Reloaded", `{"doctrine":"max-scope"}`, now+2)

	rows, err := s.AuditEvents("Augment.", "", 0, 100)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("type prefix filter: want 2 got %d", len(rows))
	}
}

func TestServerAuditEventsSinceUnixFilter(t *testing.T) {
	t.Parallel()
	st := newAuditTestStore(t)
	s := &Server{store: st}

	now := time.Now().Unix()
	seedAuditEvent(t, st, "evt-old", "p", "X", `{"doctrine":"max-scope"}`, now-100)
	seedAuditEvent(t, st, "evt-new", "p", "X", `{"doctrine":"max-scope"}`, now+100)

	rows, err := s.AuditEvents("", "", now, 100)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "evt-new" {
		t.Errorf("since filter: want 1 row evt-new got %v", rows)
	}
}

func TestDoctrineVisibleFailsClosedOnLegacyRow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		payload    string
		sessionDoc string

		canView bool
	}{

		{"legacy + max-scope session → deny", `{}`, "max-scope", false},
		{"legacy + default session → deny", `{}`, "default", false},
		{"legacy + capa-firewall session → allow", `{}`, "capa-firewall", true},

		{"malformed + max-scope → deny", `garbage`, "max-scope", false},
		{"malformed + default → deny", `garbage`, "default", false},
		{"malformed + capa-firewall → allow", `garbage`, "capa-firewall", true},

		{"unknown + default → deny", `{"doctrine":"super-secret"}`, "default", false},
		{"unknown + capa-firewall → allow", `{"doctrine":"super-secret"}`, "capa-firewall", true},

		{"max-scope row + default session → allow",
			`{"doctrine":"max-scope"}`, "default", true},
		{"default row + max-scope session → allow",
			`{"doctrine":"default"}`, "max-scope", true},
		{"capa-firewall row + default session → deny",
			`{"doctrine":"capa-firewall"}`, "default", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rowDoc := extractDoctrineFromPayload(tc.payload)
			got := handlers.DoctrineVisible(rowDoc, tc.sessionDoc)
			if got != tc.canView {
				t.Errorf("canView: payload=%q session=%q rowDoc=%q want %v got %v",
					tc.payload, tc.sessionDoc, rowDoc, tc.canView, got)
			}
		})
	}
}

func TestDoctrineVisibleWhitelistsKnownSessionDoctrines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		rowDoctrine     string
		sessionDoctrine string
		want            bool
	}{
		// Empty session doctrine → fail closed regardless of row.
		// Production callers map empty to 401 upstream, but the
		// predicate itself MUST also deny so a regression in the
		// handler can't accidentally call DoctrineVisible with "".
		{"empty session + max-scope row → deny", "max-scope", "", false},
		{"empty session + default row → deny", "default", "", false},
		{"empty session + capa-firewall row → deny", "capa-firewall", "", false},

		// "active" fallback label (server_doctrine.go:140) MUST not
		// authorise reads against any row. M-5 closure piggybacks on
		// this case.
		{"active session + max-scope row → deny", "max-scope", "active", false},
		{"active session + default row → deny", "default", "active", false},
		{"active session + capa-firewall row → deny", "capa-firewall", "active", false},

		// Unknown / future doctrine names → fail closed. This is the
		// load-bearing future-proofing: a (or later) doctrine
		// added to the registry but not yet wired through the
		// visibility matrix MUST not silently inherit max-scope/default
		// authorisation. Operator must explicitly extend this matrix.
		{"unknown session + max-scope row → deny", "max-scope", "experimental", false},
		{"unknown session + default row → deny", "default", "future-doctrine", false},
		{"unknown session + capa-firewall row → deny", "capa-firewall", "super-secret", false},

		{"capital case session → deny", "max-scope", "Max-Scope", false},
		{"whitespace-padded session → deny", "default", " default ", false},
		{"typo session → deny", "max-scope", "maxscope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := handlers.DoctrineVisible(tc.rowDoctrine, tc.sessionDoctrine)
			if got != tc.want {
				t.Errorf("DoctrineVisible(%q, %q) = %v; want %v",
					tc.rowDoctrine, tc.sessionDoctrine, got, tc.want)
			}
		})
	}
}

func TestDoctrineVisibleAllowsRecognisedSessionDoctrines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		rowDoctrine     string
		sessionDoctrine string
		want            bool
	}{

		{"max-scope sees max-scope", "max-scope", "max-scope", true},
		{"max-scope sees default", "default", "max-scope", true},
		{"max-scope rejects capa-firewall", "capa-firewall", "max-scope", false},

		{"default sees max-scope", "max-scope", "default", true},
		{"default sees default", "default", "default", true},
		{"default rejects capa-firewall", "capa-firewall", "default", false},

		{"capa-firewall sees capa-firewall", "capa-firewall", "capa-firewall", true},
		{"capa-firewall rejects max-scope", "max-scope", "capa-firewall", false},
		{"capa-firewall rejects default", "default", "capa-firewall", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := handlers.DoctrineVisible(tc.rowDoctrine, tc.sessionDoctrine)
			if got != tc.want {
				t.Errorf("DoctrineVisible(%q, %q) = %v; want %v",
					tc.rowDoctrine, tc.sessionDoctrine, got, tc.want)
			}
		})
	}
}
