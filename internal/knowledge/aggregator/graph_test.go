//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"testing"
)

func seedGraph(t *testing.T) *Aggregator {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	notes := []struct {
		noteID    string
		projectID string
		title     string
		content   string
	}{
		{"A", "p1", "Note A", "content for A, the seed note"},
		{"B", "p1", "Note B", "content for B, reached via wikilink"},
		{"C", "p1", "Note C", "content for C, reached via relates"},
		{"D", "p1", "Note D", "content for D, reached via 2-hop wikilink"},
	}

	for _, n := range notes {
		_, err := db.Exec(`INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json, promoted_at,
			 promoted_by, promote_reason, audit_chain_anchor)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			n.noteID, n.projectID, n.title, n.content, "{}",
			"2026-05-09T00:00:00Z", "testuser", "test-reason", "anchor-"+n.noteID)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_index %s: %v", n.noteID, err)
		}
	}

	edges := []struct {
		source, target, linkType string
	}{
		{"A", "B", "wikilink"},
		{"A", "C", "relates"},
		{"B", "D", "wikilink"},
	}

	for _, e := range edges {
		_, err := db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
			VALUES (?,?,?)`, e.source, e.target, e.linkType)
		if err != nil {
			t.Fatalf("INSERT knowledge_pin_wikilinks %s->%s: %v", e.source, e.target, err)
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

func TestQueryGraphEmptySeedReturnsNil(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), nil, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph(nil seed): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty seed returned %d results; want 0", len(results))
	}

	results2, err := a.QueryGraph(context.Background(), []string{}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph([] seed): %v", err)
	}
	if len(results2) != 0 {
		t.Errorf("empty-slice seed returned %d results; want 0", len(results2))
	}
}

func TestQueryGraphOneHopBFS(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 1, 10)
	if err != nil {
		t.Fatalf("QueryGraph depth=1: %v", err)
	}

	found := make(map[string]bool)
	for _, r := range results {
		found[r.NoteID] = true
	}
	if !found["B"] {
		t.Errorf("depth=1 BFS: expected B in results; got %v", noteIDs(results))
	}
	if !found["C"] {
		t.Errorf("depth=1 BFS: expected C in results; got %v", noteIDs(results))
	}

	if found["D"] {
		t.Errorf("depth=1 BFS: D (hop-2) should not appear; got %v", noteIDs(results))
	}

	if found["A"] {
		t.Errorf("depth=1 BFS: seed A should not appear in results; got %v", noteIDs(results))
	}
}

func TestQueryGraphTwoHopBFS(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph depth=2: %v", err)
	}
	found := make(map[string]bool)
	for _, r := range results {
		found[r.NoteID] = true
	}
	if !found["B"] {
		t.Errorf("depth=2 BFS: B missing from results; got %v", noteIDs(results))
	}
	if !found["C"] {
		t.Errorf("depth=2 BFS: C missing from results; got %v", noteIDs(results))
	}
	if !found["D"] {
		t.Errorf("depth=2 BFS: D (hop-2 via B) missing from results; got %v", noteIDs(results))
	}

	if found["A"] {
		t.Errorf("depth=2 BFS: seed A should not appear in results; got %v", noteIDs(results))
	}
}

// TestQueryGraphCycleDetection asserts that cycles in the wikilink graph do not
// cause infinite loops and each node appears at most once in the result set.
// We add a back-edge D->A (which forms a cycle A->B->D->A) and an edge C->B
// to create an alternate path to B.
func TestQueryGraphCycleDetection(t *testing.T) {
	a := seedGraph(t)

	_, err := a.db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?,?,?)`, "D", "A", "backlink")
	if err != nil {
		t.Fatalf("INSERT cycle edge D->A: %v", err)
	}

	_, err = a.db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?,?,?)`, "C", "B", "relates")
	if err != nil {
		t.Fatalf("INSERT edge C->B: %v", err)
	}

	results, err := a.QueryGraph(context.Background(), []string{"A"}, 3, 20)
	if err != nil {
		t.Fatalf("QueryGraph cycle: %v", err)
	}

	seen := make(map[string]int)
	for _, r := range results {
		seen[r.NoteID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("cycle detection failed: %s appears %d times in results", id, count)
		}
	}

	if seen["A"] > 0 {
		t.Errorf("seed A should not appear in results even with cycle edge D->A")
	}
}

func TestQueryGraphDefaultDepth(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 0, 10)
	if err != nil {
		t.Fatalf("QueryGraph depth=0 (default): %v", err)
	}
	found := make(map[string]bool)
	for _, r := range results {
		found[r.NoteID] = true
	}

	if !found["D"] {
		t.Errorf("depth=0 defaulted to %d; D (hop-2) should appear; got %v",
			defaultWikilinkDepth, noteIDs(results))
	}
}

func TestQueryGraphDefaultLimit(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 0)
	if err != nil {
		t.Fatalf("QueryGraph limit=0 (default): %v", err)
	}

	if len(results) < 3 {
		t.Errorf("limit=0 defaulted badly; expected ≥3 results, got %d: %v",
			len(results), noteIDs(results))
	}
}

func TestQueryGraphScoresByHopAndEdgeType(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results; got none")
	}

	scoreB := -1.0
	scoreC := -1.0
	for _, r := range results {
		switch r.NoteID {
		case "B":
			scoreB = r.Score
		case "C":
			scoreC = r.Score
		}
	}
	if scoreB < 0 {
		t.Fatalf("B not found in results: %v", noteIDs(results))
	}
	if scoreC < 0 {
		t.Fatalf("C not found in results: %v", noteIDs(results))
	}
	if scoreB <= scoreC {
		t.Errorf("expected scoreB (wikilink, %.6f) > scoreC (relates, %.6f)", scoreB, scoreC)
	}

	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("result %q has Score=%f; want >0", r.NoteID, r.Score)
		}
	}
}

func TestQueryGraphSourceLabel(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph: %v", err)
	}
	for _, r := range results {
		if r.Source != "graph" {
			t.Errorf("result %q has Source=%q; want \"graph\"", r.NoteID, r.Source)
		}
	}
}

func TestQueryGraphMetadataPopulated(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 1, 10)
	if err != nil {
		t.Fatalf("QueryGraph: %v", err)
	}
	for _, r := range results {
		if r.Title == "" {
			t.Errorf("result %q has empty Title; expected metadata from knowledge_pin_index", r.NoteID)
		}
		if r.AuditChainAnchor == "" {
			t.Errorf("result %q has empty AuditChainAnchor; expected metadata from knowledge_pin_index", r.NoteID)
		}
	}
}

func TestQueryGraphLimitEnforced(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 1)
	if err != nil {
		t.Fatalf("QueryGraph limit=1: %v", err)
	}
	if len(results) > 1 {
		t.Errorf("limit=1 not enforced: got %d results", len(results))
	}
}

func TestQueryGraphMissingMetadataExcluded(t *testing.T) {
	a := seedGraph(t)

	_, err := a.db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?,?,?)`, "A", "ghost-note", "wikilink")
	if err != nil {
		t.Fatalf("INSERT edge to ghost-note: %v", err)
	}

	results, err := a.QueryGraph(context.Background(), []string{"A"}, 1, 10)
	if err != nil {
		t.Fatalf("QueryGraph: %v", err)
	}

	for _, r := range results {
		if r.NoteID == "ghost-note" {
			t.Errorf("ghost-note (missing metadata) appeared in results; defensive exclusion failed")
		}
	}
}

func TestQueryGraphSortedDescByScore(t *testing.T) {
	a := seedGraph(t)
	results, err := a.QueryGraph(context.Background(), []string{"A"}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted desc by Score: results[%d].Score=%f > results[%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestHopWeight(t *testing.T) {
	cases := []struct {
		hop  int
		want float64
	}{
		{1, 1.0},
		{2, 0.5},
		{3, 1.0 / 3.0},
		{4, 0.25},
	}
	for _, tc := range cases {
		got := hopWeight(tc.hop)
		diff := got - tc.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1e-9 {
			t.Errorf("hopWeight(%d) = %f; want %f", tc.hop, got, tc.want)
		}
	}
}

func TestQueryGraphInDegreeBoost(t *testing.T) {
	a := seedGraph(t)

	_, err := a.db.Exec(`INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json, promoted_at,
		 promoted_by, promote_reason, audit_chain_anchor)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"X", "p1", "Note X", "content X", "{}", "2026-05-09T00:00:00Z", "testuser", "reason-X", "anchor-X")
	if err != nil {
		t.Fatalf("INSERT X: %v", err)
	}

	_, err = a.db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?,?,?)`, "X", "B", "wikilink")
	if err != nil {
		t.Fatalf("INSERT X->B: %v", err)
	}

	results, err := a.QueryGraph(context.Background(), []string{"A", "X"}, 1, 10)
	if err != nil {
		t.Fatalf("QueryGraph multi-seed: %v", err)
	}

	scoreB := -1.0
	scoreC := -1.0
	for _, r := range results {
		switch r.NoteID {
		case "B":
			scoreB = r.Score
		case "C":
			scoreC = r.Score
		}
	}
	if scoreB < 0 {
		t.Fatalf("B not found in results: %v", noteIDs(results))
	}
	if scoreC < 0 {
		t.Fatalf("C not found in results: %v", noteIDs(results))
	}

	if scoreB <= scoreC {
		t.Errorf("in-degree boost failed: scoreB (2 seeds, %.6f) should > scoreC (1 seed, %.6f)",
			scoreB, scoreC)
	}
}

func TestEdgeTypeWeightFallback(t *testing.T) {

	for _, lt := range []string{"wikilink", "backlink", "relates"} {
		if _, ok := edgeTypeWeight[lt]; !ok {
			t.Errorf("edgeTypeWeight missing %q", lt)
		}
	}

	if _, ok := edgeTypeWeight["unknown"]; ok {
		t.Errorf("edgeTypeWeight should not have \"unknown\"; fallback path would be unreachable")
	}
}

func TestQueryGraphMaxNodesLimit(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for i, id := range []string{"S", "X1", "X2", "X3", "X4", "X5", "X6"} {
		_ = i
		_, err := db.Exec(`INSERT INTO knowledge_pin_index
			(note_id, project_id, title, content, frontmatter_json, promoted_at,
			 promoted_by, promote_reason, audit_chain_anchor)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			id, "p1", "Title "+id, "content "+id, "{}",
			"2026-05-09T00:00:00Z", "testuser", "r", "anc-"+id)
		if err != nil {
			t.Fatalf("INSERT %s: %v", id, err)
		}
	}

	chain := [][]string{{"S", "X1"}, {"X1", "X2"}, {"X2", "X3"}, {"X3", "X4"}, {"X4", "X5"}, {"X5", "X6"}}
	for _, e := range chain {
		_, err := db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
			VALUES (?,?,?)`, e[0], e[1], "wikilink")
		if err != nil {
			t.Fatalf("INSERT edge %v: %v", e, err)
		}
	}

	a, err := New(Options{DB: db, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.QueryGraph(context.Background(), []string{"S"}, 10, 1)
	if err != nil {
		t.Fatalf("QueryGraph maxNodes: %v", err)
	}

	validIDs := map[string]bool{"X1": true, "X2": true, "X3": true, "X4": true}
	for _, r := range results {
		if !validIDs[r.NoteID] && r.NoteID != "X5" {

			if r.NoteID == "X6" {
				t.Errorf("maxNodes=5 violated: X6 (beyond bound) appeared in results")
			}
		}
	}
}

func TestQueryGraphOnlySeedsVisited(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json, promoted_at,
		 promoted_by, promote_reason, audit_chain_anchor)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"isolated", "p1", "Isolated", "solo", "{}", "2026-05-09T00:00:00Z", "testuser", "r", "anc")
	if err != nil {
		t.Fatalf("INSERT isolated: %v", err)
	}

	a, err := New(Options{DB: db, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.QueryGraph(context.Background(), []string{"isolated"}, 2, 10)
	if err != nil {
		t.Fatalf("QueryGraph isolated: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("isolated seed returned %d results; want 0", len(results))
	}
}

func TestExpandFrontierEmptyInput(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	edges, err := expandFrontier(context.Background(), db, nil)
	if err != nil {
		t.Fatalf("expandFrontier(nil): %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expandFrontier(nil) returned %d edges; want 0", len(edges))
	}

	edges2, err := expandFrontier(context.Background(), db, []string{})
	if err != nil {
		t.Fatalf("expandFrontier([]): %v", err)
	}
	if len(edges2) != 0 {
		t.Errorf("expandFrontier([]) returned %d edges; want 0", len(edges2))
	}
}

func TestFetchPinMetadataEmptyInput(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	meta, err := fetchPinMetadata(context.Background(), db, nil)
	if err != nil {
		t.Fatalf("fetchPinMetadata(nil): %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("fetchPinMetadata(nil) returned %d entries; want 0", len(meta))
	}

	meta2, err := fetchPinMetadata(context.Background(), db, []string{})
	if err != nil {
		t.Fatalf("fetchPinMetadata([]): %v", err)
	}
	if len(meta2) != 0 {
		t.Errorf("fetchPinMetadata([]) returned %d entries; want 0", len(meta2))
	}
}

func TestExpandFrontierClosedDB(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}

	_ = db.Close()

	_, err = expandFrontier(context.Background(), db, []string{"some-id"})
	if err == nil {
		t.Error("expandFrontier with closed DB: expected error; got nil")
	}
}

func TestFetchPinMetadataClosedDB(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}

	_ = db.Close()

	_, err = fetchPinMetadata(context.Background(), db, []string{"some-id"})
	if err == nil {
		t.Error("fetchPinMetadata with closed DB: expected error; got nil")
	}
}

func TestQueryGraphClosedDBExpandError(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir()+"/aggregator.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = db.Exec(`INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json, promoted_at,
		 promoted_by, promote_reason, audit_chain_anchor)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"S", "p1", "S", "s", "{}", "2026-05-09T00:00:00Z", "testuser", "r", "anc")
	if err != nil {
		t.Fatalf("INSERT S: %v", err)
	}
	_, err = db.Exec(`INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json, promoted_at,
		 promoted_by, promote_reason, audit_chain_anchor)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		"T", "p1", "T", "t", "{}", "2026-05-09T00:00:00Z", "testuser", "r", "anc")
	if err != nil {
		t.Fatalf("INSERT T: %v", err)
	}
	_, err = db.Exec(`INSERT INTO knowledge_pin_wikilinks (source_note_id, target_note_id, link_type)
		VALUES (?,?,?)`, "S", "T", "wikilink")
	if err != nil {
		t.Fatalf("INSERT edge S->T: %v", err)
	}

	a, err := New(Options{DB: db, Embedder: newMockEmbedder(384), Store: newMockStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_ = db.Close()

	_, err = a.QueryGraph(context.Background(), []string{"S"}, 2, 10)
	if err == nil {
		t.Error("QueryGraph with closed DB: expected error from expandFrontier; got nil")
	}
}

func noteIDs(results []QueryResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.NoteID
	}
	return ids
}
