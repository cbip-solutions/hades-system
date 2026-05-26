//go:build integration && cgo

package ecosystem_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// expectedEcosystemChunksColumns is the column list ecosystem_chunks
// MUST have after ecosystem.ApplyMigrations runs against an empty DB.
//
// Source of truth: internal/research/ecosystem/migrations/
// 003_ecosystem_chunks.sql lines 16-37. Phase G migration 009 alters
// ecosystem_versions (adds indefinite_retain) but does NOT touch
// ecosystem_chunks — so the chunks column set is purely from 003.
//
// Order is canonical (CREATE TABLE column declaration order, which
// SQLite preserves in pragma_table_info via cid ASC). The test sorts
// both expected + observed for diff-friendly assertion output, so order
// here is documentation rather than load-bearing — but the count and
// each name ARE load-bearing.
//
// Updating this list: any future migration that ALTER TABLE
// ecosystem_chunks ADD/DROP COLUMN must update this slice in the same
// commit; otherwise this test fails and surfaces the drift. The
// schema-stability invariant guarded here is the supporting check for
// inv-zen-204 (idempotent sweep): a sweep CANNOT be idempotent if the
// schema mutates between runs.
var expectedEcosystemChunksColumns = []string{
	"id",
	"package_id",
	"version_introduced",
	"version_deprecated",
	"stable_in_json",
	"content_text",
	"contextual_prefix",
	"chunk_fingerprint",
	"parent_chunk_id",
	"source_type",
	"symbol_path",
	"kind",
	"source_url",
	"embedding_binary_256d",
	"oversized",
}

func TestSweepIntegrity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dsn := filepath.Join(dir, "ecosystem.db") + "?_foreign_keys=on&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	}()

	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	observed, err := columnNames(db, "ecosystem_chunks")
	if err != nil {
		t.Fatalf("read ecosystem_chunks columns: %v", err)
	}

	expected := append([]string(nil), expectedEcosystemChunksColumns...)
	sort.Strings(expected)
	got := append([]string(nil), observed...)
	sort.Strings(got)

	missing := diffSlices(expected, got)
	unexpected := diffSlices(got, expected)

	if len(missing) == 0 && len(unexpected) == 0 {
		return
	}

	var msg strings.Builder
	msg.WriteString("inv-zen-204 supporting guard VIOLATED — ")
	msg.WriteString("ecosystem_chunks schema drift detected\n")
	msg.WriteString(fmt.Sprintf("  expected (%d): %s\n", len(expected), strings.Join(expected, ", ")))
	msg.WriteString(fmt.Sprintf("  observed (%d): %s\n", len(got), strings.Join(got, ", ")))
	if len(missing) > 0 {
		msg.WriteString(fmt.Sprintf("  MISSING from observed: %s\n", strings.Join(missing, ", ")))
	}
	if len(unexpected) > 0 {
		msg.WriteString(fmt.Sprintf("  UNEXPECTED in observed: %s\n", strings.Join(unexpected, ", ")))
	}
	msg.WriteString("\nIf this drift is intentional (a new migration added a column to ")
	msg.WriteString("ecosystem_chunks), update expectedEcosystemChunksColumns in this file ")
	msg.WriteString("in the same commit as the migration .sql file. Otherwise the ")
	msg.WriteString("inv-zen-204 weekly-sweep idempotence guarantee is at risk: a sweep ")
	msg.WriteString("cannot be schema-stable if the schema itself drifts between runs.")
	t.Fatal(msg.String())
}

func columnNames(db *sql.DB, table string) ([]string, error) {

	rows, err := db.Query(
		"SELECT name FROM pragma_table_info(?) ORDER BY cid",
		table,
	)
	if err != nil {
		return nil, fmt.Errorf("query pragma_table_info(%q): %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan column name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("table %q has zero columns (does it exist?)", table)
	}
	return names, nil
}

// diffSlices returns the elements of `a` not present in `b`. Both
// slices MUST be sorted (caller's responsibility — keeps the helper
// O(n+m) and allocation-light). Result preserves input order from `a`.
func diffSlices(a, b []string) []string {
	var out []string
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			j++
		default:
			i++
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, a[i])
	}
	return out
}
