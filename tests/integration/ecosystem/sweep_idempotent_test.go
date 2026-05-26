//go:build integration && cgo

package ecosystem_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

type dbSnapshot struct {
	tables map[string]int
}

func snapshotDB(t *testing.T, dbPath string) *dbSnapshot {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open DB snapshot: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iter table names: %v", err)
	}
	rows.Close()

	snap := &dbSnapshot{tables: make(map[string]int, len(tables))}
	for _, tbl := range tables {
		var count int
		row := db.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %q", tbl))
		if err := row.Scan(&count); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		snap.tables[tbl] = count
	}
	return snap
}

func (a *dbSnapshot) diffFrom(b *dbSnapshot) []string {
	var diffs []string
	for tbl, countA := range a.tables {
		countB, ok := b.tables[tbl]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("table %q: present in run-1 but missing in run-2", tbl))
			continue
		}
		if countA != countB {
			diffs = append(diffs, fmt.Sprintf("table %q: row count diverged %d → %d", tbl, countA, countB))
		}
	}
	for tbl := range b.tables {
		if _, ok := a.tables[tbl]; !ok {
			diffs = append(diffs, fmt.Sprintf("table %q: absent in run-1 but present in run-2", tbl))
		}
	}
	sort.Strings(diffs)
	return diffs
}

func createEcosystemSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	const ddl = `
		CREATE TABLE IF NOT EXISTS ecosystem_packages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			language TEXT NOT NULL,
			upstream_url TEXT NOT NULL,
			canonical_namespace TEXT NOT NULL,
			last_indexed_at DATETIME,
			last_upstream_check DATETIME,
			latest_stable_version TEXT,
			UNIQUE(language, name)
		);
		CREATE TABLE IF NOT EXISTS ecosystem_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			package_id INTEGER NOT NULL,
			version_introduced TEXT NOT NULL,
			content_text TEXT NOT NULL,
			chunk_fingerprint TEXT NOT NULL,
			FOREIGN KEY (package_id) REFERENCES ecosystem_packages(id)
		);
		CREATE TABLE IF NOT EXISTS ecosystem_symbols (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			package_id INTEGER NOT NULL,
			symbol_path TEXT NOT NULL,
			introduced_in TEXT,
			UNIQUE(package_id, symbol_path, introduced_in),
			FOREIGN KEY (package_id) REFERENCES ecosystem_packages(id)
		);
		CREATE TABLE IF NOT EXISTS ecosystem_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			package_id INTEGER NOT NULL,
			version_from TEXT NOT NULL,
			version_to TEXT NOT NULL,
			change_type TEXT NOT NULL,
			FOREIGN KEY (package_id) REFERENCES ecosystem_packages(id)
		);
		CREATE TABLE IF NOT EXISTS ecosystem_audit_chain (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type INTEGER NOT NULL,
			emitted_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
	`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("create schema: %v", err)
	}
}

func fingerprintFor(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func seedDeterministicCorpus(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	pkgs := []struct {
		name, language, url, ns, version string
	}{
		{"fmt", "go", "https://pkg.go.dev/fmt", "fmt", "1.21.0"},
		{"argparse", "python", "https://docs.python.org/3/library/argparse.html", "argparse", "3.12.0"},
		{"react", "typescript", "https://github.com/facebook/react", "react", "18.3.0"},
		{"tokio", "rust", "https://docs.rs/tokio", "tokio", "1.36.0"},
	}
	chunks := []struct {
		pkgID             int
		versionIntroduced string
		contentText       string
	}{
		{1, "1.0.0", "Package fmt implements formatted I/O with functions analogous to C's printf and scanf."},
		{1, "1.1.0", "Println formats using the default formats for its operands and writes to standard output."},
		{2, "3.2.0", "argparse is a Python module to parse command-line options."},
		{3, "16.0.0", "React is a JavaScript library for building user interfaces."},
		{4, "1.0.0", "Tokio is an asynchronous runtime for the Rust programming language."},
	}
	symbols := []struct {
		pkgID        int
		path         string
		introducedIn string
	}{
		{1, "fmt.Println", "1.0.0"},
		{1, "fmt.Sprintf", "1.0.0"},
		{2, "argparse.ArgumentParser", "3.2.0"},
		{3, "React.Component", "16.0.0"},
		{4, "tokio::spawn", "1.0.0"},
	}
	changes := []struct {
		pkgID                int
		from, to, changeType string
	}{
		{1, "1.20.0", "1.21.0", "added"},
		{3, "17.0.0", "18.0.0", "removed"},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, p := range pkgs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ecosystem_packages (name, language, upstream_url, canonical_namespace, latest_stable_version)
			 VALUES (?, ?, ?, ?, ?)`,
			p.name, p.language, p.url, p.ns, p.version,
		); err != nil {
			t.Fatalf("insert package %s: %v", p.name, err)
		}
	}
	for _, c := range chunks {
		fp := fingerprintFor(c.contentText)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ecosystem_chunks (package_id, version_introduced, content_text, chunk_fingerprint)
			 VALUES (?, ?, ?, ?)`,
			c.pkgID, c.versionIntroduced, c.contentText, fp,
		); err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
	}
	for _, s := range symbols {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ecosystem_symbols (package_id, symbol_path, introduced_in)
			 VALUES (?, ?, ?)`,
			s.pkgID, s.path, s.introducedIn,
		); err != nil {
			t.Fatalf("insert symbol: %v", err)
		}
	}
	for _, ch := range changes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ecosystem_changes (package_id, version_from, version_to, change_type)
			 VALUES (?, ?, ?, ?)`,
			ch.pkgID, ch.from, ch.to, ch.changeType,
		); err != nil {
			t.Fatalf("insert change: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
}

type fingerprintSweepSimulator struct {
	dbPath string
}

func (f *fingerprintSweepSimulator) Sweep(ctx context.Context) (mismatches int, err error) {
	db, err := sql.Open("sqlite3", f.dbPath)
	if err != nil {
		return 0, fmt.Errorf("open DB: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT id, content_text, chunk_fingerprint FROM ecosystem_chunks ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("query chunks: %w", err)
	}
	type pendingFix struct {
		id         int
		recomputed string
	}
	var fixes []pendingFix
	for rows.Next() {
		var id int
		var content, stored string
		if err := rows.Scan(&id, &content, &stored); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan chunk row: %w", err)
		}
		recomputed := fingerprintFor(content)
		if !strings.EqualFold(recomputed, stored) {
			fixes = append(fixes, pendingFix{id: id, recomputed: recomputed})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iter chunks: %w", err)
	}
	rows.Close()

	for _, fix := range fixes {
		mismatches++
		if _, err := db.ExecContext(ctx,
			`UPDATE ecosystem_chunks SET chunk_fingerprint = ? WHERE id = ?`,
			fix.recomputed, fix.id); err != nil {
			return mismatches, fmt.Errorf("update chunk %d: %w", fix.id, err)
		}
	}
	return mismatches, nil
}

func TestWeeklySweep_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "go.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create test DB: %v", err)
	}
	createEcosystemSchema(t, db)
	if cerr := db.Close(); cerr != nil {
		t.Fatalf("close after schema: %v", cerr)
	}

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("reopen DB for seed: %v", err)
	}
	seedDeterministicCorpus(t, db)
	if cerr := db.Close(); cerr != nil {
		t.Fatalf("close after seed: %v", cerr)
	}

	snap1 := snapshotDB(t, dbPath)

	if got, want := snap1.tables["ecosystem_packages"], 4; got != want {
		t.Fatalf("seed packages: got %d want %d", got, want)
	}
	if got, want := snap1.tables["ecosystem_chunks"], 5; got != want {
		t.Fatalf("seed chunks: got %d want %d", got, want)
	}
	if got, want := snap1.tables["ecosystem_symbols"], 5; got != want {
		t.Fatalf("seed symbols: got %d want %d", got, want)
	}
	if got, want := snap1.tables["ecosystem_changes"], 2; got != want {
		t.Fatalf("seed changes: got %d want %d", got, want)
	}

	sweeper := &fingerprintSweepSimulator{dbPath: dbPath}

	mismatches1, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep #1: %v", err)
	}
	if mismatches1 != 0 {
		t.Fatalf("sweep #1: expected zero fingerprint mismatches on self-consistent seed, got %d", mismatches1)
	}
	snap2 := snapshotDB(t, dbPath)

	if diffs := snap1.diffFrom(snap2); len(diffs) > 0 {
		t.Errorf("inv-zen-204 VIOLATED — sweep #1 mutated DB on steady state:\n%s", strings.Join(diffs, "\n"))
	}

	mismatches2, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep #2: %v", err)
	}
	if mismatches2 != 0 {
		t.Fatalf("sweep #2: expected zero fingerprint mismatches, got %d", mismatches2)
	}
	snap3 := snapshotDB(t, dbPath)

	if diffs := snap2.diffFrom(snap3); len(diffs) > 0 {
		t.Errorf("inv-zen-204 VIOLATED — sweep #2 diverged from sweep #1:\n%s", strings.Join(diffs, "\n"))
	}

	if diffs := snap1.diffFrom(snap3); len(diffs) > 0 {
		t.Errorf("inv-zen-204 VIOLATED — pre-sweep ≠ post-second-sweep:\n%s", strings.Join(diffs, "\n"))
	}
}

func TestWeeklySweep_Idempotent_EmptyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create empty DB: %v", err)
	}
	createEcosystemSchema(t, db)
	if cerr := db.Close(); cerr != nil {
		t.Fatalf("close empty DB: %v", cerr)
	}

	snap1 := snapshotDB(t, dbPath)
	if got := snap1.tables["ecosystem_chunks"]; got != 0 {
		t.Fatalf("empty seed: expected 0 chunks, got %d", got)
	}

	sweeper := &fingerprintSweepSimulator{dbPath: dbPath}
	if _, err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep #1: %v", err)
	}
	snap2 := snapshotDB(t, dbPath)
	if _, err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep #2: %v", err)
	}
	snap3 := snapshotDB(t, dbPath)

	if diffs := snap1.diffFrom(snap2); len(diffs) > 0 {
		t.Errorf("empty DB: sweep #1 changed state: %s", strings.Join(diffs, "\n"))
	}
	if diffs := snap2.diffFrom(snap3); len(diffs) > 0 {
		t.Errorf("empty DB: sweep #2 diverged from #1: %s", strings.Join(diffs, "\n"))
	}
}

func TestWeeklySweep_DetectsMismatch_ThenIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "mixed.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create DB: %v", err)
	}
	createEcosystemSchema(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO ecosystem_packages (name, language, upstream_url, canonical_namespace)
		 VALUES ('fmt','go','https://pkg.go.dev/fmt','fmt')`,
	); err != nil {
		t.Fatalf("seed package: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO ecosystem_chunks (package_id, version_introduced, content_text, chunk_fingerprint)
		 VALUES (1, '1.0.0', 'content needing repair', 'sha256:0000000000000000000000000000000000000000000000000000000000000000')`,
	); err != nil {
		t.Fatalf("seed bad chunk: %v", err)
	}
	if cerr := db.Close(); cerr != nil {
		t.Fatalf("close after bad seed: %v", cerr)
	}

	sweeper := &fingerprintSweepSimulator{dbPath: dbPath}
	mismatches1, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatalf("sweep #1: %v", err)
	}
	if mismatches1 != 1 {
		t.Fatalf("sweep #1: expected 1 mismatch repair, got %d", mismatches1)
	}

	snapAfterRepair := snapshotDB(t, dbPath)

	mismatches2, err := sweeper.Sweep(ctx)
	if err != nil {
		t.Fatalf("sweep #2: %v", err)
	}
	if mismatches2 != 0 {
		t.Fatalf("sweep #2: expected zero mismatches after repair, got %d", mismatches2)
	}
	snapPostSecondSweep := snapshotDB(t, dbPath)

	if diffs := snapAfterRepair.diffFrom(snapPostSecondSweep); len(diffs) > 0 {
		t.Errorf("inv-zen-204 VIOLATED — sweep #2 mutated post-repair state: %s",
			strings.Join(diffs, "\n"))
	}
}
