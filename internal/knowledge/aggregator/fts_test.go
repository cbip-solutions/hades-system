//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"strings"
	"testing"
)

func seedFTS(t *testing.T) *Aggregator {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	notes := []PinNote{
		{
			NoteID:           "p1:n1",
			ProjectID:        "p1",
			Title:            "Doctrine bundle TOML",
			Content:          "max-scope build-final-product no-defer canonical doctrine bundle",
			FrontmatterJSON:  "{}",
			PromoteReason:    "canonical",
			AuditChainAnchor: "x",
		},
		{
			NoteID:           "p1:n2",
			ProjectID:        "p1",
			Title:            "Methodology",
			Content:          "TDD subagent-driven-development plan execution",
			FrontmatterJSON:  "{}",
			PromoteReason:    "ref",
			AuditChainAnchor: "x",
		},
		{
			NoteID:           "p2:n1",
			ProjectID:        "p2",
			Title:            "Plan 9 brainstorm",
			Content:          "Tessera tile-log audit chain knowledge aggregator",
			FrontmatterJSON:  "{}",
			PromoteReason:    "ref",
			AuditChainAnchor: "x",
		},
	}

	for _, n := range notes {
		_, err := db.Exec(`INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json, promoted_at,
			 promoted_by, promote_reason, audit_chain_anchor)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			n.NoteID, n.ProjectID, n.Title, n.Content, n.FrontmatterJSON,
			"2026-05-07T00:00:00Z", "testuser", n.PromoteReason, n.AuditChainAnchor)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_index %s: %v", n.NoteID, err)
		}

		_, err = db.Exec(`INSERT INTO knowledge_pin_fts (rowid, content, title)
			SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`, n.NoteID)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_fts %s: %v", n.NoteID, err)
		}
	}

	a, err := New(Options{
		DB:       db,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestQueryFTSHappyPath(t *testing.T) {
	a := seedFTS(t)
	results, err := a.QueryFTS(context.Background(), "doctrine bundle", 10)
	if err != nil {
		t.Fatalf("QueryFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'doctrine bundle'")
	}
	top := results[0]
	combined := strings.ToLower(top.Title + " " + top.Snippet)
	if !strings.Contains(combined, "doctrine") && !strings.Contains(combined, "bundle") {
		t.Errorf("top result does not match query: title=%q snippet=%q", top.Title, top.Snippet)
	}
	if top.Source != "fts" {
		t.Errorf("Source = %q; want \"fts\"", top.Source)
	}
	if top.Score <= 0 {
		t.Errorf("Score = %f; want positive (BM25 normalised to -bm25)", top.Score)
	}
}

func TestQueryFTSEmptyQueryReturnsEmpty(t *testing.T) {
	a := seedFTS(t)
	results, err := a.QueryFTS(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("QueryFTS empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty query returned %d results; want 0", len(results))
	}

	results2, err := a.QueryFTS(context.Background(), "   ", 10)
	if err != nil {
		t.Fatalf("QueryFTS whitespace: %v", err)
	}
	if len(results2) != 0 {
		t.Errorf("whitespace query returned %d results; want 0", len(results2))
	}
}

func TestQueryFTSZeroLimitReturnsEmpty(t *testing.T) {
	a := seedFTS(t)
	results, err := a.QueryFTS(context.Background(), "doctrine", 0)
	if err != nil {
		t.Fatalf("QueryFTS limit=0: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("limit=0 returned %d results; want 0", len(results))
	}
}

// TestQueryFTSEscapesSpecialChars verifies that FTS5 operator characters in
// the query text do not cause a syntax error. The sanitiser strips them.
func TestQueryFTSEscapesSpecialChars(t *testing.T) {
	a := seedFTS(t)

	_, err := a.QueryFTS(context.Background(), `("quoted) +AND -bad`, 10)
	if err != nil {
		t.Fatalf("QueryFTS with special chars must not error: %v", err)
	}
}

func TestQueryFTSLimitEnforced(t *testing.T) {
	a := seedFTS(t)
	results, err := a.QueryFTS(context.Background(), "doctrine OR plan OR methodology", 1)
	if err != nil {
		t.Fatalf("QueryFTS: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("limit not enforced: got %d results; want ≤1", len(results))
	}
}

func TestQueryFTSSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		input string
		empty bool
	}{
		{`hello world`, false},
		{`"quoted phrase"`, false},
		{`(group)`, false},
		{`+plus -minus`, false},
		{`*star :colon`, false},
		{`"(+*:-)"`, true},
		{`   `, true},
		{``, true},
	}
	for _, tc := range cases {
		got := sanitizeFTSQuery(tc.input)
		if tc.empty && got != "" {
			t.Errorf("sanitizeFTSQuery(%q) = %q; want empty", tc.input, got)
		}
		if !tc.empty && got == "" {
			t.Errorf("sanitizeFTSQuery(%q) = empty; want non-empty", tc.input)
		}
	}
}
