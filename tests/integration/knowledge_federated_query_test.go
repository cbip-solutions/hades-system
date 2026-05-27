// go:build integration && cgo
//go:build integration && cgo
// +build integration,cgo

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
)

type integFederatedStore struct {
	mu       sync.Mutex
	projects []knowledgetypes.ProjectHandle
	vaults   map[string]*sql.DB
	anchors  []struct{ project, note, anchor string }
}

func (s *integFederatedStore) ListAuthorizedProjects(_ context.Context) ([]knowledgetypes.ProjectHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]knowledgetypes.ProjectHandle, len(s.projects))
	copy(out, s.projects)
	return out, nil
}

func (s *integFederatedStore) OpenProjectVault(_ context.Context, projectID string) (knowledgetypes.ProjectVault, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, ok := s.vaults[projectID]
	if !ok {
		return nil, nil
	}
	return db, nil
}

func (s *integFederatedStore) UpdateAuditChainAnchor(_ context.Context, project, note, anchor string) error {
	s.mu.Lock()
	s.anchors = append(s.anchors, struct{ project, note, anchor string }{project, note, anchor})
	s.mu.Unlock()
	return nil
}

func openIntegAggregatorDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "aggregator.db")
	db, err := aggregator.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("aggregator.Open: %v", err)
	}
	if err := aggregator.Init(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("aggregator.Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedFiveNotes(t *testing.T, db *sql.DB, projectID string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		noteID := fmt.Sprintf("%s:n%d", projectID, i)
		anchor := ""
		if i%2 == 1 {

			anchor = fmt.Sprintf("2026_05:evt-%08x:hash%d", i*0x1234567+0xabcdef, i)
		}
		content := fmt.Sprintf("knowledge note from %s index %d [[%s:n%d]]",
			projectID, i, projectID, (i+1)%5)
		title := fmt.Sprintf("%s knowledge note %d", projectID, i)

		_, err := db.Exec(`
			INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json,
			 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			noteID, projectID, title, content, "{}",
			"2026-05-09 12:00:00",
			"testuser", "integration test seed", anchor, nil,
		)
		if err != nil {
			t.Fatalf("seedFiveNotes INSERT pin_index %s: %v", noteID, err)
		}
		_, err = db.Exec(`
			INSERT INTO knowledge_pin_fts (rowid, content, title)
			SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
			noteID,
		)
		if err != nil {
			t.Fatalf("seedFiveNotes INSERT pin_fts %s: %v", noteID, err)
		}
	}
}

func newMultiProjectFakeStore(t *testing.T) (*integFederatedStore, *sql.DB) {
	t.Helper()

	pinDB := openIntegAggregatorDB(t)

	projects := []knowledgetypes.ProjectHandle{
		{ProjectID: "alpha", Alias: "alpha-project", VaultPath: "/vault/alpha"},
		{ProjectID: "beta", Alias: "beta-project", VaultPath: "/vault/beta"},
		{ProjectID: "gamma", Alias: "gamma-project", VaultPath: "/vault/gamma"},
	}

	vaults := make(map[string]*sql.DB, len(projects))
	for _, ph := range projects {
		dbPath := filepath.Join(t.TempDir(), ph.ProjectID+".db")
		db, err := aggregator.Open(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("aggregator.Open %s: %v", ph.ProjectID, err)
		}
		if err := aggregator.Init(context.Background(), db); err != nil {
			_ = db.Close()
			t.Fatalf("aggregator.Init %s: %v", ph.ProjectID, err)
		}
		seedFiveNotes(t, db, ph.ProjectID)
		vaults[ph.ProjectID] = db
		t.Cleanup(func() { _ = db.Close() })
	}

	store := &integFederatedStore{
		projects: projects,
		vaults:   vaults,
	}
	return store, pinDB
}

func TestKnowledgeFederatedQueryThreeProjects(t *testing.T) {
	store, pinDB := newMultiProjectFakeStore(t)

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	req := aggregator.QueryRequest{
		Text:  "knowledge note",
		Scope: aggregator.ScopeGlobal,
		Limit: 50,
	}

	start := time.Now()
	results, err := agg.Query(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("federated query latency %v exceeds 500ms", elapsed)
	}

	if len(results) == 0 {
		t.Fatal("Query returned 0 results; expected results from 3 projects")
	}

	seen := make(map[string]bool)
	for _, r := range results {
		seen[r.ProjectID] = true
	}
	for _, ph := range store.projects {
		if !seen[ph.ProjectID] {
			t.Errorf("project %q not represented in federated query results (results=%d, seen=%v)",
				ph.ProjectID, len(results), seen)
		}
	}
}

// -------------------------------------------------------------------------
// Test 2: TestKnowledgeFederatedQueryAuditChainFilter
//
// Seeds 3 projects × 5 notes. Odd-indexed notes have a chain anchor; even
// do not. AuditChainFilter=true must drop all results with empty anchor.
// -------------------------------------------------------------------------

func TestKnowledgeFederatedQueryAuditChainFilter(t *testing.T) {
	store, pinDB := newMultiProjectFakeStore(t)

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	req := aggregator.QueryRequest{
		Text:             "knowledge note",
		Scope:            aggregator.ScopeGlobal,
		Limit:            50,
		AuditChainFilter: true,
	}

	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query with AuditChainFilter: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("AuditChainFilter query returned 0 results; expected anchored notes from 3 projects")
	}

	for _, r := range results {
		if r.AuditChainAnchor == "" {
			t.Errorf("AuditChainFilter returned result %q (project %q) with empty anchor",
				r.NoteID, r.ProjectID)
		}
	}
}

func TestKnowledgeFederatedQueryPinBoost(t *testing.T) {
	store, pinDB := newMultiProjectFakeStore(t)

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	promoted, err := agg.Promote(
		context.Background(),
		"alpha:n1", "alpha", "testuser", "D-15 pin-boost integration test",
	)
	if err != nil {
		t.Fatalf("Promote alpha:n1: %v", err)
	}
	if promoted.NoteID != "alpha:n1" {
		t.Errorf("Promote returned NoteID %q; want alpha:n1", promoted.NoteID)
	}

	req := aggregator.QueryRequest{
		Text:  "knowledge note alpha",
		Scope: aggregator.ScopeGlobal,
		Limit: 50,
	}

	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query after Promote: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Query after Promote returned 0 results")
	}

	found := false
	for _, r := range results {
		if r.NoteID == "alpha:n1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("promoted note alpha:n1 not found in post-promote global query results")
	}

	var pinnedScore float64
	nonPinnedScores := make([]float64, 0)
	for _, r := range results {
		if r.NoteID == "alpha:n1" {
			pinnedScore = r.Score
		} else if r.ProjectID == "alpha" {
			nonPinnedScores = append(nonPinnedScores, r.Score)
		}
	}
	for _, s := range nonPinnedScores {
		if pinnedScore < s {
			t.Errorf("promoted note score %.4f < non-promoted alpha note score %.4f; expected pin-boost ranking",
				pinnedScore, s)
		}
	}
}
