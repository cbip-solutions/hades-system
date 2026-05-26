package compliance

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

var extensionHookColumns = []string{
	"audit_chain_anchor",
	"ecosystem_join_keys",
	"caronte_symbol_refs",
}

func TestInvZen130_InsertSQLDoesNotMentionExtensionHookColumns(t *testing.T) {
	root := repoRoot(t)
	indexGo := filepath.Join(root, "internal", "knowledge", "index.go")
	body, err := os.ReadFile(indexGo)
	if err != nil {
		t.Fatalf("inv-zen-130: read %s: %v", indexGo, err)
	}
	src := string(body)

	insertBody := extractConstStringLiteral(src, "indexInsertSQL")
	if insertBody == "" {
		t.Fatalf("inv-zen-130: could not locate `const indexInsertSQL = ...` in %s\n"+
			"either the constant was renamed or the parser is out of date — update this test",
			indexGo)
	}

	for _, col := range extensionHookColumns {
		if strings.Contains(insertBody, col) {
			t.Errorf("inv-zen-130 violation: %q appears inside indexInsertSQL body:\n%s",
				col, insertBody)
		}
	}
}

// TestInvZen130_PostInsertColumnsAreNull is the runtime witness.
// Constructs a fresh knowledge index, calls knowledge.IndexDoc(ctx, db, doc)
// with a fully-populated Doc (including the three sql.NullString
// extension-hook fields set to Valid=true), then probes the resulting
// row via SQL. The three columns MUST be NULL in the actual DB row,
// regardless of whether the Doc had them set in Go-land.
//
// The Valid=true setup is a load-bearing trap: a naive implementation
// could plausibly write the values when Doc fields are Valid. The spec
// is explicit — Plan 7 INSERT NEVER populates them, even if the caller
// supplies values. The data flows from Plan 9 / Plan 14 / caronte
// writers (separate code paths, materialization time), NOT from this
// INSERT. This test would fire if a developer "helpfully" forwarded
// Doc.AuditChainAnchor into the INSERT bind list.
//
// Why we set the Doc fields at all if the contract ignores them:
// catching the trap is the whole point. A test that left the Doc fields
// zero-value would not differentiate between "implementation correctly
// ignores Doc fields" and "implementation forgot to bind them".
func TestInvZen130_PostInsertColumnsAreNull(t *testing.T) {
	db := openKnowledgeIndexForInvZen130(t)
	defer db.Close()

	now := time.Now()
	doc := knowledge.Doc{
		FilePath:     "/tmp/inv-zen-130-probe.md",
		ProjectID:    "p1",
		ProjectAlias: "alias-1",
		FileType:     knowledge.FileTypeMemory,
		Title:        "probe",
		ContentText:  "body",
		LastModified: now,
		LastIndexed:  now,

		AuditChainAnchor:  sql.NullString{String: "should-be-ignored", Valid: true},
		EcosystemJoinKeys: sql.NullString{String: `["should-be-ignored"]`, Valid: true},
		CaronteSymbolRefs: sql.NullString{String: `["should-be-ignored"]`, Valid: true},
	}
	if err := knowledge.IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("inv-zen-130: knowledge.IndexDoc: %v", err)
	}

	for _, col := range extensionHookColumns {
		var v sql.NullString
		query := "SELECT " + col + " FROM knowledge_meta WHERE file_path = ?"
		err := db.QueryRow(query, doc.FilePath).Scan(&v)
		if err != nil {
			t.Fatalf("inv-zen-130: probe column %q: %v", col, err)
		}
		if v.Valid {
			t.Errorf("inv-zen-130 violation: column %q is non-NULL after IndexDoc() (got %q); "+
				"Plan 7 INSERT must NEVER populate extension-hook columns",
				col, v.String)
		}
	}

	var nullCount int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM knowledge_meta
		WHERE file_path = ?
		  AND audit_chain_anchor IS NULL
		  AND ecosystem_join_keys IS NULL
		  AND caronte_symbol_refs IS NULL
	`, doc.FilePath).Scan(&nullCount)
	if err != nil {
		t.Fatalf("inv-zen-130: cross-column probe: %v", err)
	}
	if nullCount != 1 {
		t.Errorf("inv-zen-130 violation: cross-column probe returned %d rows with all 3 columns NULL, want 1",
			nullCount)
	}
}

func TestInvZen130_SchemaSQLAndGoFileDeclareSameColumns(t *testing.T) {
	root := repoRoot(t)

	sqlPath := filepath.Join(root, "internal", "store", "schema",
		"061_knowledge_index_extension_hooks.sql")
	goPath := filepath.Join(root, "internal", "knowledge", "index.go")

	sqlBytes, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("inv-zen-130: read sql migration %s: %v", sqlPath, err)
	}
	goBytes, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatalf("inv-zen-130: read go file %s: %v", goPath, err)
	}

	sqlText := string(sqlBytes)
	goText := string(goBytes)

	for _, col := range extensionHookColumns {
		if !strings.Contains(sqlText, col) {
			t.Errorf("inv-zen-130 schema-parity: 061_*.sql missing extension-hook column %q",
				col)
		}
		if !strings.Contains(goText, col) {
			t.Errorf("inv-zen-130 schema-parity: index.go missing extension-hook column %q",
				col)
		}
	}
}

// TestInvZen130_SchemaCreateMetaDeclaresExtensionColumns is the in-DDL
// schema witness — the schemaCreateMeta DDL section of index.go MUST
// list all three columns. Tests (a) and (c) cover indexInsertSQL and
// the .sql file respectively; this test covers the third declaration
// site, the runtime CREATE TABLE statement.
//
// Without this anchor, a refactor could remove a column from the
// schemaCreateMeta DDL while leaving the Doc struct + .sql file +
// indexInsertSQL untouched. The runtime test (b) would catch it
// (column doesn't exist → SQL error), but the failure message would be
// confusing ("no such column: audit_chain_anchor"). This test surfaces
// the root cause directly.
func TestInvZen130_SchemaCreateMetaDeclaresExtensionColumns(t *testing.T) {
	root := repoRoot(t)
	indexGo := filepath.Join(root, "internal", "knowledge", "index.go")
	body, err := os.ReadFile(indexGo)
	if err != nil {
		t.Fatalf("inv-zen-130: read %s: %v", indexGo, err)
	}
	createMeta := extractConstStringLiteral(string(body), "schemaCreateMeta")
	if createMeta == "" {
		t.Fatalf("inv-zen-130: could not locate `const schemaCreateMeta = ...` in %s",
			indexGo)
	}
	for _, col := range extensionHookColumns {
		if !strings.Contains(createMeta, col) {
			t.Errorf("inv-zen-130 violation: schemaCreateMeta DDL missing column %q:\n%s",
				col, createMeta)
		}
	}
}

func TestInvZen130_SchemaSentinelReachable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge-sentinel.db")
	db, err := knowledge.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("inv-zen-130: knowledge.Open (sentinel reachability): %v", err)
	}
	defer db.Close()
	if err := knowledge.Init(context.Background(), db); err != nil {
		t.Errorf("inv-zen-130: knowledge.Init: %v", err)
	}
}

func extractConstStringLiteral(src, name string) string {

	patterns := []string{
		"const " + name + " = `",
		"\n\t" + name + " = `",
		"\n    " + name + " = `",
		"\n" + name + " = `",
	}
	var idx int = -1
	var matchedPattern string
	for _, pat := range patterns {
		if i := strings.Index(src, pat); i >= 0 {
			idx = i
			matchedPattern = pat
			break
		}
	}
	if idx < 0 {
		return ""
	}

	openTick := idx + len(matchedPattern)
	if openTick >= len(src) {
		return ""
	}
	closeTick := strings.Index(src[openTick:], "`")
	if closeTick < 0 {
		return ""
	}
	return src[openTick : openTick+closeTick]
}

func openKnowledgeIndexForInvZen130(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	db, err := knowledge.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("inv-zen-130: knowledge.Open: %v", err)
	}
	if err := knowledge.Init(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("inv-zen-130: knowledge.Init: %v", err)
	}
	return db
}
