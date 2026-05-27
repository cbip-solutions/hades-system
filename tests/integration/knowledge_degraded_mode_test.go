// go:build integration && cgo
//go:build integration && cgo
// +build integration,cgo

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
)

type integErrorEmbedder struct{}

func (integErrorEmbedder) Dimensions() int { return 384 }
func (integErrorEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("integErrorEmbedder: vec unavailable (degraded mode simulation)")
}

type degradedModeStore struct {
	mu      sync.Mutex
	vaults  map[string]*sql.DB
	anchors []struct{ project, note, anchor string }
}

func (s *degradedModeStore) ListAuthorizedProjects(_ context.Context) ([]knowledgetypes.ProjectHandle, error) {
	return []knowledgetypes.ProjectHandle{
		{ProjectID: "degrade-proj", Alias: "degraded-project", VaultPath: "/vault/degrade"},
	}, nil
}

func (s *degradedModeStore) OpenProjectVault(_ context.Context, projectID string) (knowledgetypes.ProjectVault, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vaults[projectID], nil
}

func (s *degradedModeStore) UpdateAuditChainAnchor(_ context.Context, project, note, anchor string) error {
	s.mu.Lock()
	s.anchors = append(s.anchors, struct{ project, note, anchor string }{project, note, anchor})
	s.mu.Unlock()
	return nil
}

func seedDegradedNotes(t *testing.T, db *sql.DB, projectID string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		noteID := fmt.Sprintf("%s:deg%d", projectID, i)
		anchor := fmt.Sprintf("2026_05:evt-%08x:deghash%d", i*0x7654321+0xfedcba, i)
		content := fmt.Sprintf("fallback fts knowledge content item %d for project %s", i, projectID)
		title := fmt.Sprintf("Degraded Mode Note %d", i)

		_, err := db.Exec(`
			INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json,
			 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			noteID, projectID, title, content, "{}",
			time.Now().UTC().Format("2006-01-02 15:04:05"),
			"testuser", "degraded mode test seed", anchor, nil,
		)
		if err != nil {
			t.Fatalf("seedDegradedNotes INSERT pin_index %s: %v", noteID, err)
		}
		_, err = db.Exec(`
			INSERT INTO knowledge_pin_fts (rowid, content, title)
			SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
			noteID,
		)
		if err != nil {
			t.Fatalf("seedDegradedNotes INSERT pin_fts %s: %v", noteID, err)
		}
	}
}

func newDegradedAggregator(t *testing.T) (*aggregator.Aggregator, *sql.DB) {
	t.Helper()

	pinPath := filepath.Join(t.TempDir(), "deg-pin.db")
	pinDB, err := aggregator.Open(context.Background(), pinPath)
	if err != nil {
		t.Fatalf("aggregator.Open pin: %v", err)
	}
	if err := aggregator.Init(context.Background(), pinDB); err != nil {
		_ = pinDB.Close()
		t.Fatalf("aggregator.Init pin: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	projectPath := filepath.Join(t.TempDir(), "deg-project.db")
	projectDB, err := aggregator.Open(context.Background(), projectPath)
	if err != nil {
		t.Fatalf("aggregator.Open project: %v", err)
	}
	if err := aggregator.Init(context.Background(), projectDB); err != nil {
		_ = projectDB.Close()
		t.Fatalf("aggregator.Init project: %v", err)
	}
	seedDegradedNotes(t, projectDB, "degrade-proj", 5)
	t.Cleanup(func() { _ = projectDB.Close() })

	store := &degradedModeStore{
		vaults: map[string]*sql.DB{"degrade-proj": projectDB},
	}

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: integErrorEmbedder{},
		Store:    store,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	return agg, projectDB
}

func TestKnowledgeDegradedFallsBackToFTSGraph(t *testing.T) {
	agg, _ := newDegradedAggregator(t)

	req := aggregator.QueryRequest{
		Text:  "fallback fts knowledge content",
		Scope: aggregator.ScopeGlobal,
		Limit: 20,
	}

	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query with error embedder: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Query with error embedder returned 0 results; expected FTS5 results from degrade-proj")
	}

	for _, r := range results {
		if r.Source == "vec" {
			t.Errorf("result %q has Source=vec; expected only FTS5/graph results under embed failure",
				r.NoteID)
		}
	}

	if agg.Degraded() {
		t.Error("Aggregator.Degraded() = true; expected false for Failure mode #9 (embed-error path)")
	}
}

func TestKnowledgeDegradedQueryFTSAndGraphDirectly(t *testing.T) {
	agg, projectDB := newDegradedAggregator(t)

	_, err := projectDB.Exec(`
		INSERT OR IGNORE INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?, ?, 'wikilink')`,
		"degrade-proj:deg0", "degrade-proj:deg1",
	)
	if err != nil {
		t.Fatalf("INSERT wikilink: %v", err)
	}

	req := aggregator.QueryRequest{
		Text:          "fallback fts knowledge",
		Scope:         aggregator.ScopeProject,
		ProjectID:     "degrade-proj",
		Limit:         10,
		WikilinkDepth: 1,
	}

	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query ScopeProject with error embedder: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("ScopeProject query with error embedder returned 0 results; FTS5 must be functional")
	}

	for _, r := range results {
		if r.ProjectID != "degrade-proj" {
			t.Errorf("result %q has ProjectID %q; expected degrade-proj", r.NoteID, r.ProjectID)
		}
		if r.Source == "vec" {
			t.Errorf("result %q has Source=vec under embed failure; expected fts or graph", r.NoteID)
		}
	}
}
