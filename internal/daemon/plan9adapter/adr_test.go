// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/adr"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestADRAdapterShowListGraphAndRegenerate(t *testing.T) {
	dir := seedADRDir(t)
	writeADR(t, dir, "0001-first.md", "ADR-0001", "First", adr.StatusAccepted, "plan-9", "high", "", []string{"ADR-0002"})
	writeADR(t, dir, "0002-second.md", "ADR-0002", "Second", adr.StatusAccepted, "plan-9", "", "", nil)

	a := newTestADRAdapter(t, dir, nil)

	doc, err := a.Show(context.Background(), "ADR-0001")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if doc.ID != "ADR-0001" || doc.Topic != "First" || !strings.Contains(doc.Body, "First body") {
		t.Fatalf("Show doc = %+v", doc)
	}

	rows, err := a.List(context.Background(), handlers.ADRListFilter{
		Status:    "accepted",
		Plan:      "plan-9",
		RiskLevel: "high",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "ADR-0001" {
		t.Fatalf("List rows = %+v", rows)
	}

	g, err := a.Graph(context.Background(), "ADR-0001", 1)
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[0].From != "ADR-0001" || g.Edges[0].To != "ADR-0002" {
		t.Fatalf("Graph = %+v", g)
	}

	manifest, err := a.RegenerateIndex(context.Background(), true)
	if err != nil {
		t.Fatalf("RegenerateIndex dry-run: %v", err)
	}
	if manifest.ADRCount != 2 || !strings.Contains(manifest.Manifest, `"ADR-0001"`) || !strings.Contains(manifest.Graph, `"ADR-0002"`) {
		t.Fatalf("dry-run manifest = %+v", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, "_index.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote _index.json; stat err=%v", err)
	}

	if _, err := a.RegenerateIndex(context.Background(), false); err != nil {
		t.Fatalf("RegenerateIndex write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_index.json")); err != nil {
		t.Fatalf("_index.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_graph.json")); err != nil {
		t.Fatalf("_graph.json not written: %v", err)
	}
}

func TestADRAdapterProposeTransitionAndHistoryUseAuditEventsRaw(t *testing.T) {
	dir := seedADRDir(t)
	st := openMigratedPlan9Store(t)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	a := newTestADRAdapter(t, dir, st)
	a.now = func() time.Time { return now }
	a.clock = func() string { return now.Format(time.RFC3339) }

	doc, err := a.Propose(context.Background(), "Production ADR facade", "plan-9")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if doc.ID != "ADR-0001" || doc.Status != string(adr.StatusProposed) || doc.Plan != "plan-9" {
		t.Fatalf("Propose doc = %+v", doc)
	}

	if err := a.Accept(context.Background(), "ADR-0001", "operator accepted the facade"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	history, err := a.History(context.Background(), "ADR-0001")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("History len = %d, rows=%+v", len(history), history)
	}
	if history[0].Status != string(adr.StatusProposed) || history[1].Status != string(adr.StatusAccepted) {
		t.Fatalf("History statuses = %+v", history)
	}
	if history[1].Reason != "operator accepted the facade" {
		t.Fatalf("History accepted reason = %q", history[1].Reason)
	}
}

func TestADRAdapterTransitionRequiresAuditSinkBeforeFileMutation(t *testing.T) {
	dir := seedADRDir(t)
	writeADR(t, dir, "0001-draft.md", "ADR-0001", "Draft", adr.StatusProposed, "plan-9", "", "", nil)
	a := newTestADRAdapter(t, dir, nil)

	err := a.Accept(context.Background(), "ADR-0001", "operator accepted the draft")
	if err == nil {
		t.Fatal("Accept without audit sink returned nil")
	}

	doc, showErr := a.Show(context.Background(), "ADR-0001")
	if showErr != nil {
		t.Fatalf("Show after failed Accept: %v", showErr)
	}
	if doc.Status != string(adr.StatusProposed) {
		t.Fatalf("status after failed Accept = %q, want proposed", doc.Status)
	}
}

func TestADRAdapterProposeRejectsInvalidPlanBeforeFileMutation(t *testing.T) {
	dir := seedADRDir(t)
	st := openMigratedPlan9Store(t)
	a := newTestADRAdapter(t, dir, st)

	_, err := a.Propose(context.Background(), "Invalid plan tag", "not a plan")
	if err == nil {
		t.Fatal("Propose accepted an invalid plan tag")
	}

	files, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	for _, file := range files {
		if isADRMarkdownFile(file) {
			t.Fatalf("invalid proposal wrote ADR file %s", file.Name())
		}
	}
	var count int
	if scanErr := st.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_events_raw WHERE type = ?`,
		string(adr.EvtADRProposed),
	).Scan(&count); scanErr != nil {
		t.Fatalf("count ADR proposed events: %v", scanErr)
	}
	if count != 0 {
		t.Fatalf("invalid proposal emitted %d ADR proposed events", count)
	}
}

func TestADRAdapterTransitionRollsBackWhenAuditInsertFails(t *testing.T) {
	dir := seedADRDir(t)
	writeADR(t, dir, "0001-draft.md", "ADR-0001", "Draft", adr.StatusProposed, "plan-9", "", "", nil)
	st := openMigratedPlan9Store(t)
	a := newTestADRAdapter(t, dir, st)
	if err := st.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	err := a.Accept(context.Background(), "ADR-0001", "operator accepted the draft")
	if err == nil {
		t.Fatal("Accept returned nil after audit store close")
	}

	doc, showErr := a.Show(context.Background(), "ADR-0001")
	if showErr != nil {
		t.Fatalf("Show after failed Accept: %v", showErr)
	}
	if doc.Status != string(adr.StatusProposed) {
		t.Fatalf("status after failed Accept = %q, want proposed", doc.Status)
	}
}

func TestADRAdapterProposeScansQueuedADRDirectories(t *testing.T) {
	dir := seedADRDir(t)
	st := openMigratedPlan9Store(t)
	a := newTestADRAdapter(t, dir, st)
	if err := os.Mkdir(filepath.Join(dir, "proposed"), 0o755); err != nil {
		t.Fatalf("mkdir proposed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "rejected"), 0o755); err != nil {
		t.Fatalf("mkdir rejected: %v", err)
	}
	writeADR(t, filepath.Join(dir, "proposed"), "0001-proposed.md", "ADR-0001", "Queued Proposed", adr.StatusProposed, "plan-99", "", "", nil)
	writeADR(t, filepath.Join(dir, "rejected"), "0002-rejected.md", "ADR-0002", "Queued Rejected", adr.StatusRejected, "plan-99", "", "", nil)

	doc, err := a.Propose(context.Background(), "Next queued ADR", "plan-99")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if doc.ID != "ADR-0003" {
		t.Fatalf("Propose ID = %s, want ADR-0003", doc.ID)
	}
}

func TestADRAdapterProposeConcurrentIDsAreUnique(t *testing.T) {
	dir := seedADRDir(t)
	st := openMigratedPlan9Store(t)
	a := newTestADRAdapter(t, dir, st)
	const n = 12
	var wg sync.WaitGroup
	results := make(chan handlers.ADRDoc, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc, err := a.Propose(context.Background(), "Concurrent ADR "+strconv.Itoa(i), "plan-99")
			if err != nil {
				errs <- err
				return
			}
			results <- doc
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("Propose concurrent: %v", err)
	}
	seen := map[string]bool{}
	for doc := range results {
		if seen[doc.ID] {
			t.Fatalf("duplicate concurrent ADR ID %s", doc.ID)
		}
		seen[doc.ID] = true
	}
	if len(seen) != n {
		t.Fatalf("concurrent ADR count = %d, want %d", len(seen), n)
	}
}

func seedADRDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	schemaBody, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "decisions", "_schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema.json"), schemaBody, 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return dir
}

func writeADR(t *testing.T, dir, name, id, title string, status adr.Status, plan, risk, supersededBy string, relates []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + id + "\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("status: " + string(status) + "\n")
	b.WriteString("date: \"2026-05-26\"\n")
	b.WriteString("plan: " + plan + "\n")
	b.WriteString("tags: []\n")
	if risk != "" {
		b.WriteString("risk-level: " + risk + "\n")
	}
	if supersededBy != "" {
		b.WriteString("superseded-by: " + supersededBy + "\n")
	}
	if len(relates) > 0 {
		b.WriteString("relates-to:\n")
		for _, ref := range relates {
			b.WriteString("  - " + ref + "\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString(title + " body.\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write ADR: %v", err)
	}
}

func newTestADRAdapter(t *testing.T, dir string, st *store.Store) *ADRAdapter {
	t.Helper()
	a, err := NewADRAdapter(ADRAdapterDeps{
		Dir:         dir,
		SchemaPath:  filepath.Join(dir, "_schema.json"),
		Store:       st,
		DefaultPlan: "plan-9-followup",
		Clock:       func() string { return "2026-05-26T12:00:00Z" },
		Now:         func() time.Time { return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewADRAdapter: %v", err)
	}
	return a
}

func openMigratedPlan9Store(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}
