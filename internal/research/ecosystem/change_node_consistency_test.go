// go:build integration && cgo
//go:build integration && cgo
// +build integration,cgo

// Package ecosystem — change_node_consistency_test.go
//
// Integration tests Task E-7 — Change-node graph
// consistency verifier.
//
// Scope (per E-7 task description):
// 1. ChangeExtractor.WriteChangeNodes persists []ChangeNode with
// invariant write-side enforcement (every (version_from, version_to)
// pair must have matching ecosystem_versions rows before INSERT).
// 2. ChangeExtractor.SweepChangeNodes walks ecosystem_changes; asserts
// every row's (version_from, version_to) pair has corresponding
// ecosystem_versions rows; errors if orphans found. cron
// consumes this weekly.
// 3. End-to-end pipeline: ParseChangelog → WriteChangeNodes →
// Indexer.QueryCrossVersion (E-6 surface) — verifies the full
// data flow lands rows the cross-version pivot can read back.
//
// Drift reconciliation:
// 1. NewIndexer is 2-value `(*Indexer, error)` and requires non-nil
// DB + Chain (E-6 ship + indexer.go:111). Test wires via
// fakeAuditEmitter (declared in indexer_test.go same package).
// 2. Indexer.QueryCrossVersion is 4-arg: `(ctx, packageID, vFrom, vTo)`
// with no db parameter — E-6 frozen surface uses idx.opts.DB.
// 3. package ecosystem (internal) to reuse fakeAuditEmitter — keeps
// drift-1 wiring local + avoids re-declaring the audit chain helper
// across `package ecosystem_test`.
// 4. Build tag `integration && cgo` — sqlite3 driver requires CGO and
// the test is end-to-end (real DB + real migrations + real SQL),
// matching B-12 + integration test convention.
// 5. ApplyMigrations(db) for tight fidelity vs the plan-file's
// stripped-down inline schema. Production migrations use
// `ecosystem` (not `language`) column and full UNIQUE/FK
// constraints — exercising the real schema catches column-name
// drift earlier than a synthetic schema would.
//
// Coverage discipline (CLAUDE.md hard rule 5): `change_extractor.go` is
// the cross-version graph-write surface, security/correctness-critical
// for invariant. Suite aims for ≥85% per-fn coverage on the new
// symbols including defense-in-depth branches (nil DB, empty slice,
// closed DB, ctx cancellation, partial orphans).

package ecosystem

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// setupConsistencyTestDB opens a fresh tmp-file SQLite DB with
// foreign_keys=ON + WAL journal_mode, applies all migrations,
// and registers cleanup.
//
// Why a distinct fixture name vs setupIndexerTestDB / setupQueryTestDB:
// each test file owns its own fixture so cross-file changes to fixture
// shape do not propagate (defense-in-depth against test-fixture drift;
// mirrors the indexer_query / indexer_test split precedent).
func setupConsistencyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/consistency-test.db"
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

func insertConsistencyPackage(t *testing.T, db *sql.DB, eco Ecosystem, name, ns string) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO ecosystem_packages
			(name, ecosystem, upstream_url, canonical_namespace)
		VALUES (?, ?, ?, ?)
	`, name, string(eco), "https://example.test/"+name, ns)
	if err != nil {
		t.Fatalf("insert package %s: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId %s: %v", name, err)
	}
	return id
}

func insertConsistencyVersion(t *testing.T, db *sql.DB, pkgID int64, version string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO ecosystem_versions (package_id, version, released_at)
		VALUES (?, ?, ?)
	`, pkgID, version, time.Now().UTC())
	if err != nil {
		t.Fatalf("insert version %s: %v", version, err)
	}
}

func newConsistencyExtractor() *ChangeExtractor {
	return NewChangeExtractor(ChangeExtractorOptions{})
}

func TestWriteChangeNodes_InvZen193_VersionsMustExist(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "crypto/sha256", "crypto/sha256")

	insertConsistencyVersion(t, db, pkgID, "1.22")
	insertConsistencyVersion(t, db, pkgID, "1.23")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "crypto/sha256"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.22",
			VersionTo:       "1.23",
			ChangeType:      ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum512",
			Description:     "Sum512 added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes: %v", err)
	}

	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM ecosystem_changes
		WHERE package_id=? AND version_from='1.22' AND version_to='1.23'
	`, pkgID).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 change row, got %d", count)
	}
}

func TestWriteChangeNodes_RejectsOrphanedVersions(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "log/slog", "log/slog")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "log/slog"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.21",
			VersionTo:       "1.22",
			ChangeType:      ChangeAdded,
			SymbolPath:      "log/slog.New",
			Description:     "New added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes)
	if err == nil {
		t.Fatal("want error when both version rows missing (inv-zen-193), got nil")
	}
	if !strings.Contains(err.Error(), "inv-zen-193") {
		t.Errorf("error must mention inv-zen-193; got %q", err.Error())
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_changes WHERE package_id=?`, pkgID).Scan(&count)
	if count != 0 {
		t.Errorf("WriteChangeNodes inserted %d rows after error; want 0 (atomicity)", count)
	}
}

func TestWriteChangeNodes_VersionFromMissing(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoPython, "asyncio", "asyncio")

	insertConsistencyVersion(t, db, pkgID, "3.12")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoPython, Name: "asyncio"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "3.11",
			VersionTo:       "3.12",
			ChangeType:      ChangeRemoved,
			SymbolPath:      "asyncio.coroutine",
			Description:     "coroutine removed",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes)
	if err == nil {
		t.Fatal("want error when version_from missing, got nil")
	}
	if !strings.Contains(err.Error(), "version_from") {
		t.Errorf("error must identify version_from as the missing side; got %q", err.Error())
	}
}

func TestWriteChangeNodes_EmptyNodes_NoOp(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoRust, "tokio", "tokio")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoRust, Name: "tokio"}
	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, []ChangeNode{}); err != nil {
		t.Fatalf("WriteChangeNodes empty: %v", err)
	}
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nil); err != nil {
		t.Fatalf("WriteChangeNodes nil: %v", err)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_changes WHERE package_id=?`, pkgID).Scan(&count)
	if count != 0 {
		t.Errorf("empty slice inserted %d rows; want 0", count)
	}
}

func TestWriteChangeNodes_IdempotentInsertOrIgnore(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoTypeScript, "typescript", "typescript")
	insertConsistencyVersion(t, db, pkgID, "5.0")
	insertConsistencyVersion(t, db, pkgID, "5.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoTypeScript, Name: "typescript"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "5.0",
			VersionTo:       "5.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "typescript.createProgram",
			Description:     "createProgram added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("first WriteChangeNodes: %v", err)
	}
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("second WriteChangeNodes (idempotent): %v", err)
	}

	var count int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM ecosystem_changes
		WHERE package_id=? AND version_from='5.0' AND version_to='5.1'
	`, pkgID).Scan(&count)
	if count != 1 {
		t.Errorf("INSERT OR IGNORE dedup failed: got %d rows, want 1", count)
	}
}

func TestWriteChangeNodes_DBError(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "errpkg", "errpkg")
	insertConsistencyVersion(t, db, pkgID, "1.0")
	insertConsistencyVersion(t, db, pkgID, "1.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "errpkg"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "errpkg.Sym",
			Description:     "Sym added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	if err := db.Close(); err != nil {
		t.Fatalf("force-close db: %v", err)
	}

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes)
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

func TestWriteChangeNodes_NilDB(t *testing.T) {
	pkg := PackageRef{ID: 1, Ecosystem: EcoGo, Name: "nilpkg"}
	nodes := []ChangeNode{
		{
			PackageID:       1,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "nilpkg.Sym",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(context.Background(), nil, pkg, nodes)
	if err == nil {
		t.Fatal("want error on nil DB, got nil")
	}
	if !strings.Contains(err.Error(), "nil db") {
		t.Errorf("error must identify the nil DB condition; got %q", err.Error())
	}
}

func TestWriteChangeNodes_VersionToMissing(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "v2-missing", "v2-missing")

	insertConsistencyVersion(t, db, pkgID, "1.0")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "v2-missing"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "v2.Sym",
			Description:     "Sym added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes)
	if err == nil {
		t.Fatal("want error when version_to missing, got nil")
	}
	if !strings.Contains(err.Error(), "version_to") {
		t.Errorf("error must identify version_to as the missing side; got %q", err.Error())
	}
}

func TestWriteChangeNodes_EmptySymbolPath_Placeholder(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "emptysym", "emptysym")
	insertConsistencyVersion(t, db, pkgID, "1.0")
	insertConsistencyVersion(t, db, pkgID, "1.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "emptysym"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeChanged,
			SymbolPath:      "",
			Description:     "free-form changelog text",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes empty symbol: %v", err)
	}

	var symPath string
	err := db.QueryRow(`
		SELECT symbol_path FROM ecosystem_changes
		WHERE package_id=? AND version_from='1.0' AND version_to='1.1'
	`, pkgID).Scan(&symPath)
	if err != nil {
		t.Fatalf("query symbol_path: %v", err)
	}
	wantPrefix := "unknown:1.0:1.1:"
	if !strings.HasPrefix(symPath, wantPrefix) {
		t.Errorf("placeholder symbol_path = %q; want prefix %q", symPath, wantPrefix)
	}

	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes second pass: %v", err)
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_changes WHERE package_id=?`, pkgID).Scan(&count)
	if count != 1 {
		t.Errorf("placeholder + INSERT OR IGNORE dedup failed: got %d rows, want 1", count)
	}
}

func TestWriteChangeNodes_EmptySourceExtracted_DefaultsToExplicitChangelog(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "emptysrc", "emptysrc")
	insertConsistencyVersion(t, db, pkgID, "1.0")
	insertConsistencyVersion(t, db, pkgID, "1.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "emptysrc"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "emptysrc.Sym",
			Description:     "Sym added",
			SourceExtracted: "",
		},
	}

	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes empty source: %v", err)
	}

	var src string
	err := db.QueryRow(`
		SELECT source_extracted FROM ecosystem_changes
		WHERE package_id=? AND version_from='1.0' AND version_to='1.1'
	`, pkgID).Scan(&src)
	if err != nil {
		t.Fatalf("query source_extracted: %v", err)
	}
	if src != SourceExplicitChangelog {
		t.Errorf("default source_extracted = %q; want %q", src, SourceExplicitChangelog)
	}
}

func TestWriteChangeNodes_CtxCanceled(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "ctxpkg", "ctxpkg")
	insertConsistencyVersion(t, db, pkgID, "1.0")
	insertConsistencyVersion(t, db, pkgID, "1.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "ctxpkg"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "ctxpkg.Sym",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ce := newConsistencyExtractor()
	err := ce.WriteChangeNodes(ctx, db, pkg, nodes)
	if err == nil {
		t.Fatal("want ctx.Err() on cancelled ctx, got nil")
	}
}

func TestSweepChangeNodes_Consistent(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoPython, "asyncio", "asyncio")
	insertConsistencyVersion(t, db, pkgID, "3.11")
	insertConsistencyVersion(t, db, pkgID, "3.12")

	_, err := db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, source_extracted)
		VALUES (?, '3.11', '3.12', 'removed', 'asyncio.coroutine', 'explicit_changelog')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed change: %v", err)
	}

	ce := newConsistencyExtractor()
	if err := ce.SweepChangeNodes(context.Background(), db); err != nil {
		t.Errorf("SweepChangeNodes on consistent dataset: %v", err)
	}
}

func TestSweepChangeNodes_InconsistentDetected(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoRust, "tokio", "tokio")

	insertConsistencyVersion(t, db, pkgID, "1.35")

	_, err := db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, source_extracted)
		VALUES (?, '1.35', '1.36', 'added', 'tokio.spawn', 'explicit_changelog')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed orphan change: %v", err)
	}

	ce := newConsistencyExtractor()
	err = ce.SweepChangeNodes(context.Background(), db)
	if err == nil {
		t.Fatal("want error from SweepChangeNodes on inconsistent dataset, got nil")
	}
	if !strings.Contains(err.Error(), "inv-zen-193") {
		t.Errorf("error must mention inv-zen-193; got %q", err.Error())
	}
}

func TestSweepChangeNodes_PartialOrphan_DetectsOnePair(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "partial", "partial")

	for _, v := range []string{"1.0", "1.1", "1.2", "1.3", "1.4"} {
		insertConsistencyVersion(t, db, pkgID, v)
	}

	consistent := []struct {
		vFrom, vTo, sym string
	}{
		{"1.0", "1.1", "partial.Sym1"},
		{"1.1", "1.2", "partial.Sym2"},
		{"1.2", "1.3", "partial.Sym3"},
		{"1.3", "1.4", "partial.Sym4"},
	}
	for _, r := range consistent {
		_, err := db.Exec(`
			INSERT INTO ecosystem_changes
				(package_id, version_from, version_to, change_type, symbol_path, source_extracted)
			VALUES (?, ?, ?, 'added', ?, 'explicit_changelog')
		`, pkgID, r.vFrom, r.vTo, r.sym)
		if err != nil {
			t.Fatalf("seed consistent: %v", err)
		}
	}

	_, err := db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, source_extracted)
		VALUES (?, '1.4', '1.99', 'added', 'partial.Orphan', 'explicit_changelog')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	ce := newConsistencyExtractor()
	err = ce.SweepChangeNodes(context.Background(), db)
	if err == nil {
		t.Fatal("want error from SweepChangeNodes with 1 orphan, got nil")
	}
	if !strings.Contains(err.Error(), "1 orphaned") {
		t.Errorf("error must report exactly 1 orphan; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "1.99") {
		t.Errorf("error must name the orphan version; got %q", err.Error())
	}
}

func TestSweepChangeNodes_Idempotent(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoTypeScript, "ts-idem", "ts-idem")
	insertConsistencyVersion(t, db, pkgID, "5.0")
	insertConsistencyVersion(t, db, pkgID, "5.1")

	_, err := db.Exec(`
		INSERT INTO ecosystem_changes
			(package_id, version_from, version_to, change_type, symbol_path, source_extracted)
		VALUES (?, '5.0', '5.1', 'added', 'ts.createProgram', 'explicit_changelog')
	`, pkgID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	ce := newConsistencyExtractor()
	if err := ce.SweepChangeNodes(context.Background(), db); err != nil {
		t.Fatalf("first SweepChangeNodes: %v", err)
	}
	if err := ce.SweepChangeNodes(context.Background(), db); err != nil {
		t.Fatalf("second SweepChangeNodes (idempotency): %v", err)
	}
}

func TestSweepChangeNodes_EmptyDB(t *testing.T) {
	db := setupConsistencyTestDB(t)
	ce := newConsistencyExtractor()
	if err := ce.SweepChangeNodes(context.Background(), db); err != nil {
		t.Errorf("SweepChangeNodes empty DB: %v", err)
	}
}

func TestSweepChangeNodes_NilDB(t *testing.T) {
	ce := newConsistencyExtractor()
	err := ce.SweepChangeNodes(context.Background(), nil)
	if err == nil {
		t.Fatal("want error on nil DB, got nil")
	}
	if !strings.Contains(err.Error(), "nil db") {
		t.Errorf("error must identify the nil DB condition; got %q", err.Error())
	}
}

func TestSweepChangeNodes_DBError(t *testing.T) {
	db := setupConsistencyTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("force-close db: %v", err)
	}
	ce := newConsistencyExtractor()
	err := ce.SweepChangeNodes(context.Background(), db)
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

func TestSweepChangeNodes_CtxCanceled(t *testing.T) {
	db := setupConsistencyTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ce := newConsistencyExtractor()
	err := ce.SweepChangeNodes(ctx, db)
	if err == nil {
		t.Fatal("want ctx.Err() on cancelled ctx, got nil")
	}
}

func TestEndToEndChangeExtraction(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "log/slog", "log/slog")
	insertConsistencyVersion(t, db, pkgID, "1.21")
	insertConsistencyVersion(t, db, pkgID, "1.22")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "log/slog"}
	changelog := &Changelog{
		Package:        pkg,
		VersionFrom:    "1.21",
		VersionTo:      "1.22",
		FormatDetected: "keep-a-changelog",
		RawText: `# Changelog

## [1.22] - 2024-02-06

### Added
- log/slog.NewLogger convenience constructor

### Changed
- log/slog.Handler interface extended with Enable(level) method
`,
	}

	ce := newConsistencyExtractor()

	nodes := ce.ParseChangelog(context.Background(), changelog)
	if len(nodes) == 0 {
		t.Fatal("ParseChangelog returned 0 nodes")
	}

	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes: %v", err)
	}

	idxr, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	result, err := idxr.QueryCrossVersion(context.Background(), pkgID, "1.21", "1.22")
	if err != nil {
		t.Fatalf("QueryCrossVersion: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("want cross-version query result, got 0 nodes")
	}

	wantPaths := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		wantPaths[n.SymbolPath] = true
	}
	gotPaths := make(map[string]bool, len(result))
	for _, n := range result {
		gotPaths[n.SymbolPath] = true
	}
	for p := range wantPaths {
		if !gotPaths[p] {
			t.Errorf("symbol_path %q missing from QueryCrossVersion result", p)
		}
	}
}

func TestEndToEndChangeExtraction_SweepAcceptsRoundTrip(t *testing.T) {
	db := setupConsistencyTestDB(t)
	pkgID := insertConsistencyPackage(t, db, EcoGo, "round/trip", "round/trip")
	insertConsistencyVersion(t, db, pkgID, "1.0")
	insertConsistencyVersion(t, db, pkgID, "1.1")

	pkg := PackageRef{ID: pkgID, Ecosystem: EcoGo, Name: "round/trip"}
	nodes := []ChangeNode{
		{
			PackageID:       pkgID,
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeAdded,
			SymbolPath:      "round/trip.Sym",
			Description:     "Sym added",
			SourceExtracted: SourceExplicitChangelog,
		},
	}

	ce := newConsistencyExtractor()
	if err := ce.WriteChangeNodes(context.Background(), db, pkg, nodes); err != nil {
		t.Fatalf("WriteChangeNodes: %v", err)
	}
	if err := ce.SweepChangeNodes(context.Background(), db); err != nil {
		t.Errorf("SweepChangeNodes after WriteChangeNodes: %v (inv-zen-193 must hold post-write)", err)
	}
}
