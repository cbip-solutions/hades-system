// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
)

func TestKnowledgeAdapterQueryMapsAuditChainAnchor(t *testing.T) {
	fix := newKnowledgeAdapterFixture(t)
	seedPlan9KnowledgePin(t, fix.pinDB, "note-a", "proj-a", "Alpha", "alpha searchable body", "2026_05:evt-a:hash-a")

	rows, err := fix.adapter.Query(context.Background(), handlers.KnowledgeQueryReqP9{
		Query:      "searchable",
		Scope:      "pinned-only",
		AuditChain: true,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].AuditChainAnchor != "2026_05:evt-a:hash-a" {
		t.Fatalf("AuditChainAnchor = %q", rows[0].AuditChainAnchor)
	}
	if rows[0].ChainProof != "" {
		t.Fatalf("ChainProof = %q, want empty: P9 exposes anchors, not inclusion proofs", rows[0].ChainProof)
	}
}

func TestKnowledgeAdapterPromoteRequiresProjectID(t *testing.T) {
	fix := newKnowledgeAdapterFixture(t)
	err := fix.adapter.Promote(context.Background(), "note-a", "", "operator approved", "operator-test")
	if err == nil || !strings.Contains(err.Error(), "projectID required") {
		t.Fatalf("Promote err = %v, want projectID required", err)
	}
}

func TestKnowledgeAdapterPromoteAndUnpromoteUseAggregator(t *testing.T) {
	fix := newKnowledgeAdapterFixture(t)
	seedPlan9KnowledgePin(t, fix.projectDB, "note-a", "proj-a", "Alpha", "alpha source body", "source-anchor")

	if err := fix.adapter.Promote(context.Background(), "note-a", "proj-a", "operator approved", "operator-test"); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	var pinCount int
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, "note-a").Scan(&pinCount); err != nil {
		t.Fatalf("count pin: %v", err)
	}
	if pinCount != 1 {
		t.Fatalf("pinCount = %d, want 1", pinCount)
	}

	if err := fix.adapter.Unpromote(context.Background(), "note-a", "proj-a", "superseded", "operator-test"); err != nil {
		t.Fatalf("Unpromote: %v", err)
	}
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, "note-a").Scan(&pinCount); err != nil {
		t.Fatalf("count pin after unpromote: %v", err)
	}
	if pinCount != 0 {
		t.Fatalf("pinCount after unpromote = %d, want 0", pinCount)
	}
}

func TestKnowledgeAdapterListAndRebuildAreSynchronous(t *testing.T) {
	fix := newKnowledgeAdapterFixture(t)
	seedPlan9KnowledgePin(t, fix.pinDB, "note-a", "proj-a", "Alpha", "alpha rebuildable body", "2026_05:evt-a:hash-a")

	list, err := fix.adapter.List(context.Background(), "proj-a", true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !list[0].Pinned || list[0].ProjectID != "proj-a" {
		t.Fatalf("List = %+v", list)
	}

	resp, err := fix.adapter.Rebuild(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if resp.RebuiltCount != 1 {
		t.Fatalf("RebuiltCount = %d, want 1", resp.RebuiltCount)
	}
	if resp.JobID == "" || resp.StartedAt == 0 {
		t.Fatalf("Rebuild response missing job/timestamp: %+v", resp)
	}
}

type knowledgeAdapterFixture struct {
	adapter   *KnowledgeAdapter
	pinDB     *sql.DB
	projectDB *sql.DB
}

func newKnowledgeAdapterFixture(t *testing.T) knowledgeAdapterFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	pinDB, err := aggregator.Open(ctx, filepath.Join(dir, "aggregator.db"))
	if err != nil {
		t.Fatalf("aggregator.Open pin: %v", err)
	}
	if err := aggregator.Init(ctx, pinDB); err != nil {
		_ = pinDB.Close()
		t.Fatalf("aggregator.Init pin: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	projectDB, err := aggregator.Open(ctx, filepath.Join(dir, "project.db"))
	if err != nil {
		t.Fatalf("aggregator.Open project: %v", err)
	}
	if err := aggregator.Init(ctx, projectDB); err != nil {
		_ = projectDB.Close()
		t.Fatalf("aggregator.Init project: %v", err)
	}
	t.Cleanup(func() { _ = projectDB.Close() })

	store := &knowledgeFixtureStore{
		projects: []aggregator.ProjectHandle{{ProjectID: "proj-a", Alias: "project-a", VaultPath: filepath.Join(dir, "project.db")}},
		vaults:   map[string]*sql.DB{"proj-a": projectDB},
	}
	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}
	adapter, err := NewKnowledgeAdapter(KnowledgeAdapterDeps{
		Aggregator: agg,
		Now:        func() int64 { return 1770000000 },
	})
	if err != nil {
		t.Fatalf("NewKnowledgeAdapter: %v", err)
	}
	return knowledgeAdapterFixture{adapter: adapter, pinDB: pinDB, projectDB: projectDB}
}

type knowledgeFixtureStore struct {
	projects []aggregator.ProjectHandle
	vaults   map[string]*sql.DB
}

func (s *knowledgeFixtureStore) ListAuthorizedProjects(context.Context) ([]aggregator.ProjectHandle, error) {
	return append([]aggregator.ProjectHandle(nil), s.projects...), nil
}

func (s *knowledgeFixtureStore) OpenProjectVault(_ context.Context, projectID string) (aggregator.ProjectVault, error) {
	return s.vaults[projectID], nil
}

func (s *knowledgeFixtureStore) UpdateAuditChainAnchor(context.Context, string, string, string) error {
	return nil
}

func seedPlan9KnowledgePin(t *testing.T, db *sql.DB, noteID, projectID, title, content, anchor string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		noteID, projectID, title, content, "{}",
		"2026-05-26 12:00:00", "testuser", "seed reason", anchor, nil,
	)
	if err != nil {
		t.Fatalf("insert pin %s: %v", noteID, err)
	}
	_, err = db.Exec(`
		INSERT INTO knowledge_pin_fts(rowid, content, title)
		SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	)
	if err != nil {
		t.Fatalf("insert fts %s: %v", noteID, err)
	}
}
