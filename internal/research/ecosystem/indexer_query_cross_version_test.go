//go:build cgo
// +build cgo

// Package ecosystem — indexer_query_cross_version_test.go
//
// Tests for IsCrossVersionQuery (package-level regex helper) +
// Indexer.QueryCrossVersion (SQL pivot over ecosystem_changes), shipped
// by Plan 14 Phase E Task E-6.
//
// Coverage discipline (CLAUDE.md hard rule 5): the cross-version query
// path is consumed by Phase D dispatcher.go at intent-routing step 1;
// it is security/correctness-critical (drives whether a query hits the
// VersionRAG change-node pivot vs the normal vector+BM25 retrieval). The
// suite aims for ≥90% per-fn coverage on both new symbols, including the
// defense-in-depth branches (nil DB, ctx cancellation, empty result set,
// regex negative cases).
//
// Drift reconciliation (Stage 0 reality-check 2026-05-18):
//   1. NewIndexer returns (*Indexer, error) — 2-value return. Tests use
//      the constructor with a fakeAuditEmitter (declared in indexer_test.go
//      same package) instead of the plan-file's single-return shape.
//   2. QueryCrossVersion uses idx.opts.DB (Phase C convention, mirrors
//      BinaryTop200 / FTS5Top200 / HydrateChunks) — NOT db-as-parameter
//      from the plan-file. Aligns with master §3.13 IndexerQueryAdapter
//      pattern.
//   3. Regex refined: the second alternation gains a `(?:\w+\s+)?` group
//      before the second version capture so 3-part SemVer with a word
//      prefix ("go 1.22.0 to go 1.23.0 breaking") matches consistently
//      with the first alternation's existing pre-version-word optional
//      group. Documented in indexer.go::crossVersionQueryRe.
//
// Test infrastructure: reuses setupQueryTestDB + fakeAuditEmitter from
// indexer_query_test.go + indexer_test.go (same package). Inserts
// ecosystem_changes rows directly via raw SQL (the indexer's WriteChunks
// happy-path tests already cover the upsert pipeline; we want a
// minimal-surface read test, not a write integration test).
//
// Build tag `cgo`: depends on sqlite3 driver registration; mirrors the
// indexer_query_test.go convention.

package ecosystem

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestIsCrossVersionQuery(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		want    bool
		from    string
		to      string
		comment string
	}{

		{
			name:    "between_X_and_Y",
			query:   "what changed between go 1.22 and go 1.23",
			want:    true,
			from:    "1.22",
			to:      "1.23",
			comment: "first alternation: 'changed between' + 'and'",
		},
		{
			name:    "between_bare_versions",
			query:   "changed between 1.21 and 1.22",
			want:    true,
			from:    "1.21",
			to:      "1.22",
			comment: "first alternation w/o ecosystem prefix words",
		},
		{
			name:    "X_to_Y_breaking",
			query:   "go 1.22 to 1.23 breaking changes",
			want:    true,
			from:    "1.22",
			to:      "1.23",
			comment: "second alternation: 'X to Y break|chang|...'",
		},
		{
			name:    "single_version_usage_negative",
			query:   "how to use crypto/sha256 in go 1.22",
			want:    false,
			comment: "single version, no transition — negative",
		},
		{
			name:    "single_version_feature_docs_negative",
			query:   "python 3.12 match statement docs",
			want:    false,
			comment: "single version, no transition — negative",
		},
		{
			name:    "whats_new_in_form",
			query:   "what's new in python 3.11 to 3.12",
			want:    true,
			from:    "3.11",
			to:      "3.12",
			comment: "first alternation: 'what.{0,20}new\\s+in' (apostrophe handled via .)",
		},

		{
			name:    "3part_semver_with_word_prefix",
			query:   "go 1.22.0 to go 1.23.0 breaking",
			want:    true,
			from:    "1.22.0",
			to:      "1.23.0",
			comment: "second alternation w/ \\w+\\s+ pre-version group (Drift 4 refinement)",
		},
		{
			name:    "arrow_unicode",
			query:   "1.21 → 1.22 migration",
			want:    true,
			from:    "1.21",
			to:      "1.22",
			comment: "second alternation w/ unicode arrow + 'migrat'",
		},
		{
			name:    "ascii_arrow",
			query:   "1.21 -> 1.22 changelog",
			want:    false,
			comment: "ascii arrow but trailing 'changelog' does not match break|chang|new|releas|migrat... wait — 'chang' IS a prefix in 'changelog'",
		},
		{
			name:    "changes_from_to_form",
			query:   "changes from 1.21 to 1.22",
			want:    true,
			from:    "1.21",
			to:      "1.22",
			comment: "first alternation: 'changes from X to Y'",
		},
		{
			name:    "to_without_versions_negative",
			query:   "go to 1.22 features",
			want:    false,
			comment: "'to' without leading number — negative",
		},
		{
			name:    "capitalized_query",
			query:   "WHAT CHANGED BETWEEN 1.22 AND 1.23",
			want:    true,
			from:    "1.22",
			to:      "1.23",
			comment: "(?i) flag covers all-caps query",
		},
		{
			name:    "version_listing_no_transition_negative",
			query:   "go 1.20 and python 3.10",
			want:    false,
			comment: "two versions but no transition cue word — negative",
		},
		{
			name:    "rust_semver_0x",
			query:   "rust 0.1 to 0.2 breaking",
			want:    true,
			from:    "0.1",
			to:      "0.2",
			comment: "rust 0.x SemVer ranges",
		},
		{
			name:    "empty_query_negative",
			query:   "",
			want:    false,
			comment: "empty input — negative",
		},
	}

	for i := range cases {
		if cases[i].name == "ascii_arrow" {
			cases[i].want = true
			cases[i].from = "1.21"
			cases[i].to = "1.22"
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, from, to := IsCrossVersionQuery(tc.query)
			if matched != tc.want {
				t.Errorf("IsCrossVersionQuery(%q): matched=%v, want %v (%s)",
					tc.query, matched, tc.want, tc.comment)
			}
			if from != tc.from {
				t.Errorf("IsCrossVersionQuery(%q): from=%q, want %q",
					tc.query, from, tc.from)
			}
			if to != tc.to {
				t.Errorf("IsCrossVersionQuery(%q): to=%q, want %q",
					tc.query, to, tc.to)
			}
		})
	}
}

func TestIsCrossVersionQuery_EmptyReturnsEmptyTuple(t *testing.T) {
	matched, from, to := IsCrossVersionQuery("")
	if matched || from != "" || to != "" {
		t.Errorf("IsCrossVersionQuery(\"\") = (%v,%q,%q); want (false,\"\",\"\")",
			matched, from, to)
	}
}

func seedChangeNodes(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('testpkg', 'go', 'https://example.test/testpkg', 'crypto/sha256')
	`)
	if err != nil {
		t.Fatalf("seed package: %v", err)
	}
	pkgID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("seed package LastInsertId: %v", err)
	}

	for _, r := range []struct {
		ct, sym, desc, src string
	}{
		{"added", "crypto/sha256.New256", "new constructor", "explicit_changelog"},
		{"removed", "crypto/sha256.Old256", "deprecated removed", "explicit_changelog"},
		{"changed", "crypto/sha256.Sum256", "signature changed", "implicit_deepdiff"},
		{"deprecated", "crypto/sha256.DeprecatedHelper", "marked deprecated", "explicit_changelog"},
	} {
		_, err := db.Exec(`
			INSERT INTO ecosystem_changes
				(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, pkgID, "1.22", "1.23", r.ct, r.sym, r.desc, r.src)
		if err != nil {
			t.Fatalf("seed change row (%s/%s): %v", r.ct, r.sym, err)
		}
	}
	return pkgID
}

func seedChangeNodesAllTypes(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('alltypes', 'go', 'https://example.test/alltypes', 'alltypes')
	`)
	if err != nil {
		t.Fatalf("seed alltypes package: %v", err)
	}
	pkgID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("seed alltypes LastInsertId: %v", err)
	}

	for _, ct := range []ChangeType{ChangeAdded, ChangeRemoved, ChangeChanged, ChangeDeprecated, ChangeMoved} {
		_, err := db.Exec(`
			INSERT INTO ecosystem_changes
				(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, pkgID, "1.0", "1.1", string(ct), "sym."+string(ct), "desc-"+string(ct), "explicit_changelog")
		if err != nil {
			t.Fatalf("seed alltypes %s: %v", ct, err)
		}
	}
	return pkgID
}

// TestQueryCrossVersion_ReturnsMatchingNodes verifies the SQL pivot
// returns exactly the rows for the (packageID, vfrom, vto) tuple — and
// only those rows (a parallel package's rows do NOT leak).
func TestQueryCrossVersion_ReturnsMatchingNodes(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	pkgID := seedChangeNodes(t, db)

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('otherpkg', 'go', 'https://example.test/otherpkg', 'otherpkg')
	`)
	if err != nil {
		t.Fatalf("seed otherpkg: %v", err)
	}
	otherID, _ := res.LastInsertId()
	_, err = db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		VALUES (?, '1.22', '1.23', 'added', 'other.Sym', 'leakage canary', 'explicit_changelog')
	`, otherID)
	if err != nil {
		t.Fatalf("seed otherpkg change: %v", err)
	}

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "1.22", "1.23")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if got, want := len(nodes), 4; got != want {
		t.Fatalf("QueryCrossVersion len = %d; want %d (must NOT leak otherpkg)", got, want)
	}
	for _, n := range nodes {
		if n.PackageID != pkgID {
			t.Errorf("node.PackageID = %d; want %d", n.PackageID, pkgID)
		}
		if n.VersionFrom != "1.22" {
			t.Errorf("node.VersionFrom = %q; want 1.22", n.VersionFrom)
		}
		if n.VersionTo != "1.23" {
			t.Errorf("node.VersionTo = %q; want 1.23", n.VersionTo)
		}
	}
}

// TestQueryCrossVersion_EmptyResult verifies an empty pivot (no matching
// rows) returns a non-error empty slice — the dispatcher distinguishes
// "no change-nodes for this pair" from "error" and the contract MUST
// preserve that distinction.
func TestQueryCrossVersion_EmptyResult(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	nodes, err := idx.QueryCrossVersion(context.Background(), 99, "0.1", "0.2")
	if err != nil {
		t.Fatalf("QueryCrossVersion empty: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("QueryCrossVersion empty: got %d nodes; want 0", len(nodes))
	}
}

func TestQueryCrossVersion_GroupsByChangeType(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	pkgID := seedChangeNodesAllTypes(t, db)

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "1.0", "1.1")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if got, want := len(nodes), 5; got != want {
		t.Fatalf("len = %d; want %d (one per ChangeType)", got, want)
	}
	seen := map[ChangeType]int{}
	for _, n := range nodes {
		seen[n.ChangeType]++
	}
	for _, ct := range []ChangeType{ChangeAdded, ChangeRemoved, ChangeChanged, ChangeDeprecated, ChangeMoved} {
		if seen[ct] != 1 {
			t.Errorf("ChangeType %q count = %d; want 1", ct, seen[ct])
		}
	}
}

func TestQueryCrossVersion_SortedByChangeType(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('sorted', 'go', 'https://example.test/sorted', 'sorted')
	`)
	if err != nil {
		t.Fatalf("seed sorted package: %v", err)
	}
	pkgID, _ := res.LastInsertId()

	rows := []struct {
		ct, sym string
	}{
		{"removed", "z.Last"},
		{"added", "b.Beta"},
		{"removed", "a.First"},
		{"added", "a.Alpha"},
	}
	for _, r := range rows {
		_, err := db.Exec(`
			INSERT INTO ecosystem_changes
				(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
			VALUES (?, '2.0', '2.1', ?, ?, '', 'explicit_changelog')
		`, pkgID, r.ct, r.sym)
		if err != nil {
			t.Fatalf("seed sorted row: %v", err)
		}
	}

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "2.0", "2.1")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("len = %d; want 4", len(nodes))
	}

	want := []struct{ ct, sym string }{
		{"added", "a.Alpha"},
		{"added", "b.Beta"},
		{"removed", "a.First"},
		{"removed", "z.Last"},
	}
	for i, w := range want {
		if string(nodes[i].ChangeType) != w.ct || nodes[i].SymbolPath != w.sym {
			t.Errorf("nodes[%d] = (%s, %s); want (%s, %s)",
				i, nodes[i].ChangeType, nodes[i].SymbolPath, w.ct, w.sym)
		}
	}
}

func TestQueryCrossVersion_PopulatesAllFields(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('fields', 'go', 'https://example.test/fields', 'fields')
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	pkgID, _ := res.LastInsertId()
	_, err = db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		VALUES (?, '1.0', '1.1', 'changed', 'fields.Sym', 'a description', 'haiku_inferred')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed change: %v", err)
	}

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "1.0", "1.1")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("len = %d; want 1", len(nodes))
	}
	n := nodes[0]
	if n.ID == 0 {
		t.Errorf("ID = 0; want autoinc rowid")
	}
	if n.PackageID != pkgID {
		t.Errorf("PackageID = %d; want %d", n.PackageID, pkgID)
	}
	if n.VersionFrom != "1.0" {
		t.Errorf("VersionFrom = %q; want 1.0", n.VersionFrom)
	}
	if n.VersionTo != "1.1" {
		t.Errorf("VersionTo = %q; want 1.1", n.VersionTo)
	}
	if n.ChangeType != ChangeChanged {
		t.Errorf("ChangeType = %q; want changed", n.ChangeType)
	}
	if n.SymbolPath != "fields.Sym" {
		t.Errorf("SymbolPath = %q; want fields.Sym", n.SymbolPath)
	}
	if n.Description != "a description" {
		t.Errorf("Description = %q; want a description", n.Description)
	}
	if n.SourceExtracted != "haiku_inferred" {
		t.Errorf("SourceExtracted = %q; want haiku_inferred", n.SourceExtracted)
	}
}

func TestQueryCrossVersion_NullableSymbolPathDescription(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES ('nullable', 'go', 'https://example.test/nullable', 'nullable')
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	pkgID, _ := res.LastInsertId()

	_, err = db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, source_extracted)
		VALUES (?, '1.0', '1.1', 'added', 'operator_annotated')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed null-cols change: %v", err)
	}

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "1.0", "1.1")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("len = %d; want 1", len(nodes))
	}
	if nodes[0].SymbolPath != "" {
		t.Errorf("SymbolPath on NULL row = %q; want \"\"", nodes[0].SymbolPath)
	}
	if nodes[0].Description != "" {
		t.Errorf("Description on NULL row = %q; want \"\"", nodes[0].Description)
	}
}

func TestQueryCrossVersion_CtxCanceled(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	seedChangeNodes(t, db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.QueryCrossVersion(ctx, 1, "1.22", "1.23")
	if err == nil {
		t.Fatal("expected ctx error; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want errors.Is(context.Canceled)", err)
	}
}

func TestQueryCrossVersion_NoDB(t *testing.T) {

	idx := &Indexer{opts: IndexerOptions{DB: nil, Chain: &fakeAuditEmitter{}}}
	_, err := idx.QueryCrossVersion(context.Background(), 1, "1.22", "1.23")
	if err == nil {
		t.Fatal("expected error on nil DB; got nil")
	}
	if !strings.Contains(err.Error(), "no DB configured") {
		t.Errorf("err=%q does not mention `no DB configured`", err.Error())
	}
}

func TestQueryCrossVersion_VersionMismatchEmptySlice(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)
	pkgID := seedChangeNodes(t, db)

	nodes, err := idx.QueryCrossVersion(context.Background(), pkgID, "1.22", "1.99")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("got %d nodes for mismatched vto; want 0", len(nodes))
	}
}

func TestQueryCrossVersion_ClosedDB(t *testing.T) {
	db := setupQueryTestDB(t)
	idx := newQueryTestIndexer(t, db)

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err := idx.QueryCrossVersion(context.Background(), 1, "1.0", "1.1")
	if err == nil {
		t.Fatal("expected error on closed DB; got nil")
	}
	if !strings.Contains(err.Error(), "QueryCrossVersion query") {
		t.Errorf("err=%q does not mention `QueryCrossVersion query` wrap", err.Error())
	}
}
