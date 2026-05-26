// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/citationadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestCitationRegistryWireUpEmitsCitationRendered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srv := daemon.New(st, daemon.Config{})

	bridge := citationadapter.New(srv)
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(bridge))
	srv.SetCitationRegistry(reg)

	if srv.Citations() != reg {
		t.Fatal("Citations() != reg after SetCitationRegistry")
	}

	env := &citation.Envelope{
		ID:           "c-wire01",
		Type:         citation.CitationTypeFileSlice,
		Source:       citation.SourceManualOverride,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-wire-001",
		Confidence:   0.9,
		RRFScore:     0.5,
		RRFRank:      1,
		ProjectID:    "p-wire",
		Payload:      "wire-up smoke payload",
	}
	sess := citation.SessionContext{
		Doctrine: "max-scope",
		Platform: "markdown",
		Now:      time.Unix(1715299200, 0),
	}
	if _, err := reg.Dispatch(env, sess); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var (
		gotType      string
		gotProjectID string
		gotPayload   string
	)
	row := st.DB().QueryRow(
		`SELECT type, project_id, payload_json
		 FROM audit_events_raw
		 WHERE type = 'CitationRendered'
		 ORDER BY emitted_at DESC LIMIT 1`,
	)
	if err := row.Scan(&gotType, &gotProjectID, &gotPayload); err != nil {
		t.Fatalf("scan audit_events_raw: %v", err)
	}
	if gotType != "CitationRendered" {
		t.Errorf("type: want CitationRendered got %q", gotType)
	}
	if gotProjectID != "p-wire" {
		t.Errorf("project_id: want p-wire got %q", gotProjectID)
	}

	for _, want := range []string{
		`"citation_id":"c-wire01"`,
		`"platform":"markdown"`,
		`"audit_event_link":"zen://audit/evt-wire-001"`,
	} {
		if !strings.Contains(gotPayload, want) {
			t.Errorf("payload_json missing %q in: %s", want, gotPayload)
		}
	}
}

func TestCitationRenderedEventReadableByMaxScopeSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srv := daemon.New(st, daemon.Config{})

	const rowID = "audit-row-m3-fixture"
	bridge := citationadapter.New(srv).WithIDGenerator(func() string { return rowID })
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(bridge))
	srv.SetCitationRegistry(reg)

	env := &citation.Envelope{
		ID:           "c-msrd01",
		Type:         citation.CitationTypeFileSlice,
		Source:       citation.SourceManualOverride,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-msrd-001",
		Confidence:   0.9,
		RRFScore:     0.5,
		RRFRank:      1,
		ProjectID:    "p-msrd",
		Payload:      "max-scope-readable payload",
	}
	maxScopeSess := citation.SessionContext{
		Doctrine: "max-scope",
		Platform: "markdown",
		Now:      time.Unix(1715299200, 0),
	}
	if _, err := reg.Dispatch(env, maxScopeSess); err != nil {
		t.Fatalf("Dispatch (max-scope): %v", err)
	}

	row, err := srv.AuditEventByID(rowID)
	if err != nil {
		t.Fatalf("AuditEventByID(%q): %v", rowID, err)
	}
	if row.Doctrine != "max-scope" {
		t.Fatalf("row.Doctrine: want max-scope got %q (m-3 regression: adapter "+
			"did not stamp doctrine into audit payload — readable by "+
			"capa-firewall sessions only)", row.Doctrine)
	}

	if !handlers.DoctrineVisible(row.Doctrine, "max-scope") {
		t.Errorf("DoctrineVisible(row=%q, session=max-scope) = false; "+
			"the originating session must always see its own row", row.Doctrine)
	}

	// 3. Cross-doctrine read: a capa-firewall session MUST be DENIED
	//    against a max-scope row. Documents the inv-zen-172 invariant
	//    at the level of the rendered-event payload.
	if handlers.DoctrineVisible(row.Doctrine, "capa-firewall") {
		t.Errorf("DoctrineVisible(row=%q, session=capa-firewall) = true; "+
			"capa-firewall session must NOT see max-scope rows", row.Doctrine)
	}
}

func TestCitationRenderedEventStampsDefaultDoctrine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srv := daemon.New(st, daemon.Config{})

	const rowID = "audit-row-m3-default"
	bridge := citationadapter.New(srv).WithIDGenerator(func() string { return rowID })
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(bridge))
	srv.SetCitationRegistry(reg)

	env := &citation.Envelope{
		ID:           "c-defrd1",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-defrd-001",
		Confidence:   0.5,
		RRFScore:     0.1,
		RRFRank:      3,
		ProjectID:    "p-defrd",
		Payload:      "default-doctrine payload",
	}
	defaultSess := citation.SessionContext{
		Doctrine: "default",
		Platform: "markdown",
		Now:      time.Unix(1715299200, 0),
	}
	if _, err := reg.Dispatch(env, defaultSess); err != nil {
		t.Fatalf("Dispatch (default): %v", err)
	}

	row, err := srv.AuditEventByID(rowID)
	if err != nil {
		t.Fatalf("AuditEventByID(%q): %v", rowID, err)
	}
	if row.Doctrine != "default" {
		t.Errorf("row.Doctrine: want default got %q (m-3 regression)", row.Doctrine)
	}
	if !handlers.DoctrineVisible(row.Doctrine, "default") {
		t.Error("default session must see its own default row")
	}
	if !handlers.DoctrineVisible(row.Doctrine, "max-scope") {
		t.Error("max-scope session must see default rows (spec §3.4)")
	}
	if handlers.DoctrineVisible(row.Doctrine, "capa-firewall") {
		t.Error("capa-firewall session must NOT see default rows")
	}
}

func TestCitationRenderedEventCapaFirewallSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srv := daemon.New(st, daemon.Config{})

	const rowID = "audit-row-m3-cf"
	bridge := citationadapter.New(srv).WithIDGenerator(func() string { return rowID })
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(bridge))
	srv.SetCitationRegistry(reg)

	env := &citation.Envelope{
		ID:           "c-cfrd01",
		Type:         citation.CitationTypeFileSlice,
		Source:       citation.SourceManualOverride,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-cfrd-001",
		Confidence:   0.9,
		RRFScore:     0.5,
		RRFRank:      1,
		ProjectID:    "p-cfrd",
		Payload:      "capa-firewall payload",
	}
	cfSess := citation.SessionContext{
		Doctrine: "capa-firewall",
		Platform: "markdown",
		Now:      time.Unix(1715299200, 0),
	}
	if _, err := reg.Dispatch(env, cfSess); err != nil {
		t.Fatalf("Dispatch (capa-firewall): %v", err)
	}

	row, err := srv.AuditEventByID(rowID)
	if err != nil {
		t.Fatalf("AuditEventByID(%q): %v", rowID, err)
	}
	if row.Doctrine != "capa-firewall" {
		t.Errorf("row.Doctrine: want capa-firewall got %q (m-3 regression)", row.Doctrine)
	}
	if !handlers.DoctrineVisible(row.Doctrine, "capa-firewall") {
		t.Error("capa-firewall session must see its own row")
	}
	if handlers.DoctrineVisible(row.Doctrine, "max-scope") {
		t.Error("max-scope session must NOT see capa-firewall rows (one-way isolation)")
	}
	if handlers.DoctrineVisible(row.Doctrine, "default") {
		t.Error("default session must NOT see capa-firewall rows (one-way isolation)")
	}
}
