//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func seedPinNote(t *testing.T, db *sql.DB, noteID, projectID, title, content, anchor string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		noteID, projectID, title, content, "{}",
		time.Now().UTC().Format("2006-01-02 15:04:05"),
		"testuser", "test reason", anchor, nil,
	)
	if err != nil {
		t.Fatalf("seedPinNote INSERT pin_index %s: %v", noteID, err)
	}

	_, err = db.Exec(`
		INSERT INTO knowledge_pin_fts (rowid, content, title)
		SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`, noteID)
	if err != nil {
		t.Fatalf("seedPinNote INSERT pin_fts %s: %v", noteID, err)
	}
}

type mockFederatedStore struct {
	mu       sync.Mutex
	projects []ProjectHandle
	vaults   map[string]*sql.DB
}

func (m *mockFederatedStore) ListAuthorizedProjects(_ context.Context) ([]ProjectHandle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.projects, nil
}

func (m *mockFederatedStore) OpenProjectVault(_ context.Context, projectID string) (ProjectVault, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	db, ok := m.vaults[projectID]
	if !ok {
		return nil, nil
	}
	return db, nil
}

func (m *mockFederatedStore) UpdateAuditChainAnchor(_ context.Context, _, _, _ string) error {
	return nil
}

type federatedFixture struct {
	agg      *Aggregator
	pinDB    *sql.DB
	store    *mockFederatedStore
	projects []ProjectHandle
}

// setupFederatedFixture creates:
//   - 3 per-project DBs (internal-platform-x / zen / hotel), each seeded with one note that
//     has a non-empty audit_chain_anchor.
//   - 1 aggregator pin-index DB seeded with a note from "internal-platform-x".
//   - A mockFederatedStore wiring the 3 project DBs.
//   - An Aggregator with a 384-d mock embedder wired to the pin-index DB.
//
// Cleanup is registered via t.Cleanup — callers do not need to close DBs.
func setupFederatedFixture(t *testing.T) *federatedFixture {
	t.Helper()
	tempDir := t.TempDir()

	pinDBPath := filepath.Join(tempDir, "pin.db")
	pinDB, err := Open(context.Background(), pinDBPath)
	if err != nil {
		t.Fatalf("Open pin DB: %v", err)
	}
	if err := Init(context.Background(), pinDB); err != nil {
		_ = pinDB.Close()
		t.Fatalf("Init pin DB: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	seedPinNote(t, pinDB,
		"pin-internal-platform-x-1", "internal-platform-x", "Internal-Platform-X Pin Note",
		"content about internal-platform-x intelligence", "2026_05:pin-internal-platform-x-1:anchorA")

	projects := []ProjectHandle{
		{ProjectID: "internal-platform-x", Alias: "internal-platform-x", VaultPath: "/vault/internal-platform-x"},
		{ProjectID: "zen", Alias: "zen-swarm", VaultPath: "/vault/zen"},
		{ProjectID: "hotel", Alias: "reference-project", VaultPath: "/vault/hotel"},
	}

	vaults := make(map[string]*sql.DB, len(projects))
	for _, ph := range projects {
		dbPath := filepath.Join(tempDir, ph.ProjectID+".db")
		db, err := Open(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("Open project DB %s: %v", ph.ProjectID, err)
		}
		if err := Init(context.Background(), db); err != nil {
			_ = db.Close()
			t.Fatalf("Init project DB %s: %v", ph.ProjectID, err)
		}

		noteID := ph.ProjectID + "-note-1"
		anchor := "2026_05:" + noteID + ":anchorX"
		seedPinNote(t, db,
			noteID, ph.ProjectID,
			ph.Alias+" knowledge note",
			"content about "+ph.Alias, anchor)
		vaults[ph.ProjectID] = db
		t.Cleanup(func() { _ = db.Close() })
	}

	store := &mockFederatedStore{
		projects: projects,
		vaults:   vaults,
	}

	embedder := newMockEmbedder(384)
	agg, err := New(Options{
		DB:       pinDB,
		Embedder: embedder,
		Store:    store,
	})
	if err != nil {
		t.Fatalf("New Aggregator: %v", err)
	}

	return &federatedFixture{
		agg:      agg,
		pinDB:    pinDB,
		store:    store,
		projects: projects,
	}
}

func TestQueryFederatedAcrossThreeProjects(t *testing.T) {
	fix := setupFederatedFixture(t)
	req := QueryRequest{
		Text:  "knowledge note",
		Scope: ScopeGlobal,
		Limit: 20,
	}
	results, err := fix.agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) < 3 {
		t.Errorf("global query returned %d results; want ≥ 3 (one per project)", len(results))
	}

	projectsSeen := make(map[string]bool)
	for _, r := range results {
		projectsSeen[r.ProjectID] = true
	}
	for _, ph := range fix.projects {
		if !projectsSeen[ph.ProjectID] {
			t.Errorf("project %q not represented in global query results", ph.ProjectID)
		}
	}
}

func TestQueryProjectScopeRestrictsToOneProject(t *testing.T) {
	fix := setupFederatedFixture(t)
	req := QueryRequest{
		Text:      "knowledge note internal-platform-x",
		Scope:     ScopeProject,
		ProjectID: "internal-platform-x",
		Limit:     20,
	}
	results, err := fix.agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("project-scope query returned 0 results; expected ≥ 1")
	}
	for _, r := range results {
		if r.ProjectID != "internal-platform-x" {
			t.Errorf("result from project %q leaked into project-scope internal-platform-x query", r.ProjectID)
		}
	}
}

func TestQueryAuditChainFilterDropsResultsWithoutAnchor(t *testing.T) {
	fix := setupFederatedFixture(t)

	internalPlatformXDB := fix.store.vaults["internal-platform-x"]
	seedPinNote(t, internalPlatformXDB,
		"internal-platform-x-noanchor", "internal-platform-x",
		"Note without anchor", "content noanchor", "")

	req := QueryRequest{
		Text:             "internal-platform-x",
		Scope:            ScopeProject,
		ProjectID:        "internal-platform-x",
		Limit:            20,
		AuditChainFilter: true,
	}
	results, err := fix.agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, r := range results {
		if r.AuditChainAnchor == "" {
			t.Errorf("audit-chain filter left result %q with empty AuditChainAnchor", r.NoteID)
		}
	}
}

func TestQueryRespectsContextCancellation(t *testing.T) {
	fix := setupFederatedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := QueryRequest{
		Text:  "anything",
		Scope: ScopeGlobal,
		Limit: 10,
	}
	_, err := fix.agg.Query(ctx, req)
	if err == nil {
		t.Error("Query with pre-cancelled ctx returned nil error; expected context error")
	}
}

func TestQueryRejectsInvalidRequest(t *testing.T) {
	fix := setupFederatedFixture(t)
	req := QueryRequest{
		Text:  "",
		Scope: ScopeGlobal,
	}
	_, err := fix.agg.Query(context.Background(), req)
	if err == nil {
		t.Error("Query with empty Text returned nil error; expected validation error")
	}
}

func TestQueryPinnedOnlyDoesNotConsultPerProjectStore(t *testing.T) {
	fix := setupFederatedFixture(t)

	spy := &spyStore{inner: fix.store}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    spy,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := QueryRequest{
		Text:  "Internal-Platform-X Pin Note",
		Scope: ScopePinnedOnly,
		Limit: 10,
	}
	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Error("pinned-only query returned 0 results; expected ≥ 1 from pin index")
	}

	if spy.listCalls > 0 {
		t.Errorf("pinned-only called ListAuthorizedProjects %d times; expected 0", spy.listCalls)
	}
	if spy.openCalls > 0 {
		t.Errorf("pinned-only called OpenProjectVault %d times; expected 0", spy.openCalls)
	}
}

type spyStore struct {
	inner     PerProjectKnowledgeStore
	mu        sync.Mutex
	listCalls int
	openCalls int
}

func (s *spyStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	s.mu.Lock()
	s.listCalls++
	s.mu.Unlock()
	return s.inner.ListAuthorizedProjects(ctx)
}

func (s *spyStore) OpenProjectVault(ctx context.Context, projectID string) (ProjectVault, error) {
	s.mu.Lock()
	s.openCalls++
	s.mu.Unlock()
	return s.inner.OpenProjectVault(ctx, projectID)
}

func (s *spyStore) UpdateAuditChainAnchor(ctx context.Context, projectID, noteID, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, projectID, noteID, anchor)
}

type errorEmbedder struct{ dim int }

func (e *errorEmbedder) Dimensions() int { return e.dim }
func (e *errorEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embedder: model unavailable")
}

func TestQueryEmbedderErrorGraceDegrade(t *testing.T) {
	fix := setupFederatedFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: &errorEmbedder{dim: 384},
		Store:    fix.store,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:  "Internal-Platform-X Pin Note",
		Scope: ScopePinnedOnly,
		Limit: 10,
	}

	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query with failing embedder returned error: %v", err)
	}
	if len(results) == 0 {
		t.Error("Query with failing embedder returned 0 results; expected FTS results from graceful degrade")
	}
}

func TestQueryProjectScopeVaultOpenError(t *testing.T) {
	fix := setupFederatedFixture(t)

	errStore := &errorOpenStore{inner: fix.store}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    errStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:      "internal-platform-x",
		Scope:     ScopeProject,
		ProjectID: "internal-platform-x",
		Limit:     10,
	}
	_, err = agg.Query(context.Background(), req)
	if err == nil {
		t.Error("Query with vault-open error returned nil; expected error")
	}
}

func TestQueryProjectScopeVaultNotDB(t *testing.T) {
	fix := setupFederatedFixture(t)

	notDBStore := &notDBVaultStore{inner: fix.store}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    notDBStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:      "internal-platform-x",
		Scope:     ScopeProject,
		ProjectID: "internal-platform-x",
		Limit:     10,
	}
	_, err = agg.Query(context.Background(), req)
	if err == nil {
		t.Error("Query with non-DB vault returned nil; expected error")
	}
}

func TestQueryGlobalListProjectsError(t *testing.T) {
	fix := setupFederatedFixture(t)

	errStore := &errorListStore{inner: fix.store}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    errStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:  "anything",
		Scope: ScopeGlobal,
		Limit: 10,
	}
	_, err = agg.Query(context.Background(), req)
	if err == nil {
		t.Error("Query global with ListAuthorizedProjects error returned nil; expected error")
	}
}

func TestQueryDBNilDB(t *testing.T) {
	req := &QueryRequest{
		Text:  "test",
		Scope: ScopeGlobal,
		Limit: 10,
	}
	results, err := queryDB(context.Background(), nil, req, nil, "")
	if err != nil {
		t.Fatalf("queryDB nil db: %v", err)
	}
	if results != nil {
		t.Errorf("queryDB nil db returned %v; want nil", results)
	}
}

func TestQueryUnknownScope(t *testing.T) {
	fix := setupFederatedFixture(t)

	req := QueryRequest{
		Text:  "something",
		Scope: Scope("bogus"),
		Limit: 10,
	}
	_, err := fix.agg.Query(context.Background(), req)
	if err == nil {
		t.Error("Query with bogus scope returned nil; expected validation error")
	}
}

func TestQueryDBFTSError(t *testing.T) {
	fix := setupFederatedFixture(t)

	tmpDB, err := Open(context.Background(), t.TempDir()+"/closed.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), tmpDB); err != nil {
		_ = tmpDB.Close()
		t.Fatalf("Init: %v", err)
	}
	_ = tmpDB.Close()

	req := &QueryRequest{
		Text:  "anything",
		Scope: ScopeGlobal,
		Limit: 10,
	}

	_, err = queryDB(context.Background(), tmpDB, req, nil, "")
	if err == nil {
		t.Error("queryDB with closed DB returned nil error; expected FTS error")
	}
	_ = fix // silence unused warning
}

func TestQueryGlobalCancelledContext(t *testing.T) {
	fix := setupFederatedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := QueryRequest{
		Text:  "anything",
		Scope: ScopeGlobal,
		Limit: 10,
	}
	_, err := fix.agg.Query(ctx, req)
	if err == nil {
		t.Error("Query global with pre-cancelled ctx returned nil; expected context error")
	}
}

func TestQueryDBWithVecResults(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"vec-note-1", "vproj", "Vec Result Note",
		"semantic content for vector search embedding test", "{}",
		"2026-05-09 12:00:00", "testuser", "test reason", "anchor-vec1", nil,
	)
	if err != nil {
		t.Fatalf("INSERT pin_index: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO knowledge_pin_fts(rowid, content, title) SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`, "vec-note-1"); err != nil {
		t.Fatalf("INSERT pin_fts: %v", err)
	}

	emb := make([]float32, vecDimensions)
	for i := range emb {
		emb[i] = float32(0.05)
	}
	embBytes := float32SliceBytes(emb)
	if _, err := db.Exec(`INSERT INTO knowledge_pin_vec(rowid, embedding) SELECT rowid, ? FROM knowledge_pin_index WHERE note_id = ?`, embBytes, "vec-note-1"); err != nil {
		t.Fatalf("INSERT pin_vec: %v", err)
	}

	queryEmb := make([]float32, vecDimensions)
	for i := range queryEmb {
		queryEmb[i] = float32(0.05)
	}

	req := &QueryRequest{
		Text:          "semantic content",
		Scope:         ScopeGlobal,
		Limit:         10,
		WikilinkDepth: 2,
	}
	topKs, err := queryDB(context.Background(), db, req, queryEmb, "vproj")
	if err != nil {
		t.Fatalf("queryDB with vec: %v", err)
	}

	var ftsFound, vecFound bool
	for _, tk := range topKs {
		if tk.Source == "fts" {
			ftsFound = true
		}
		if tk.Source == "vec" {
			vecFound = true
		}
	}
	if !ftsFound {
		t.Error("queryDB: FTS TopK not present in results")
	}
	if !vecFound {
		t.Error("queryDB: vec TopK not present; expected vec results when embedding seeded")
	}
}

func TestQueryDBWithGraphResults(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"gr-seed", "grproj", "XYZGraph Seed Note",
		"xyzgraph unique term only in seed", "{}",
		"2026-05-09 12:00:00", "testuser", "test reason", "anchor-gr-seed", nil,
	)
	if err != nil {
		t.Fatalf("INSERT gr-seed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO knowledge_pin_fts(rowid, content, title) SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`, "gr-seed"); err != nil {
		t.Fatalf("INSERT fts gr-seed: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO knowledge_pin_index
		(note_id, project_id, title, content, frontmatter_json,
		 promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"gr-link", "grproj", "Linked Note",
		"completely different content without the unique term", "{}",
		"2026-05-09 12:00:01", "testuser", "test reason", "anchor-gr-link", nil,
	)
	if err != nil {
		t.Fatalf("INSERT gr-link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO knowledge_pin_fts(rowid, content, title) SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`, "gr-link"); err != nil {
		t.Fatalf("INSERT fts gr-link: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO knowledge_pin_wikilinks(source_note_id, target_note_id, link_type) VALUES (?,?,?)`,
		"gr-seed", "gr-link", "wikilink"); err != nil {
		t.Fatalf("INSERT wikilink: %v", err)
	}

	req := &QueryRequest{
		Text:          "xyzgraph",
		Scope:         ScopeGlobal,
		Limit:         10,
		WikilinkDepth: 1,
	}
	topKs, err := queryDB(context.Background(), db, req, nil, "grproj")
	if err != nil {
		t.Fatalf("queryDB with graph: %v", err)
	}
	var graphFound bool
	for _, tk := range topKs {
		if tk.Source == "graph" {
			graphFound = true
		}
	}
	if !graphFound {
		t.Errorf("queryDB: graph TopK not present in %d topKs; expected graph results from wikilink traversal", len(topKs))
	}
}

func TestQueryDBGraphErrorPath(t *testing.T) {

	db := openTestDB(t)

	seedPinNote(t, db, "g-note-1", "gproj", "Graph Error Test",
		"error path coverage test note", "anchor-g1")

	if _, err := db.Exec(`DROP TABLE knowledge_pin_wikilinks`); err != nil {
		t.Fatalf("DROP wikilinks: %v", err)
	}

	req := &QueryRequest{
		Text:          "error path coverage",
		Scope:         ScopeGlobal,
		Limit:         10,
		WikilinkDepth: 2,
	}

	_, err := queryDB(context.Background(), db, req, nil, "")
	if err == nil {
		t.Error("queryDB with dropped wikilinks table returned nil error; expected graph error")
	}
}

func TestQueryGlobalWithNilVault(t *testing.T) {
	fix := setupFederatedFixture(t)

	nilVaultStore := &nilVaultForProjectStore{
		inner:     fix.store,
		nilForPID: "hotel",
	}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    nilVaultStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:  "knowledge note",
		Scope: ScopeGlobal,
		Limit: 20,
	}
	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Error("global query with nil vault for hotel returned 0 results; expected internal-platform-x + zen")
	}
}

func TestQueryGlobalWithErrorVault(t *testing.T) {
	fix := setupFederatedFixture(t)

	errVaultStore := &errorVaultForProjectStore{
		inner:     fix.store,
		errForPID: "zen",
	}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    errVaultStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:  "knowledge note",
		Scope: ScopeGlobal,
		Limit: 20,
	}
	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Error("global query with vault error for zen returned 0 results")
	}
}

func TestQueryGlobalWithNotDBVault(t *testing.T) {
	fix := setupFederatedFixture(t)

	notDBStore := &notDBVaultForProjectStore{
		inner:       fix.store,
		notDBForPID: "zen",
	}
	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    notDBStore,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := QueryRequest{
		Text:  "knowledge note",
		Scope: ScopeGlobal,
		Limit: 20,
	}
	results, err := agg.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Error("global query with non-DB vault for zen returned 0 results")
	}
}

type errorOpenStore struct{ inner PerProjectKnowledgeStore }

func (s *errorOpenStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *errorOpenStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return nil, errors.New("errorOpenStore: vault unavailable")
}
func (s *errorOpenStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type notDBVaultStore struct{ inner PerProjectKnowledgeStore }

func (s *notDBVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *notDBVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {

	return "not-a-db", nil
}
func (s *notDBVaultStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type errorListStore struct{ inner PerProjectKnowledgeStore }

func (s *errorListStore) ListAuthorizedProjects(_ context.Context) ([]ProjectHandle, error) {
	return nil, errors.New("errorListStore: list unavailable")
}
func (s *errorListStore) OpenProjectVault(ctx context.Context, pid string) (ProjectVault, error) {
	return s.inner.OpenProjectVault(ctx, pid)
}
func (s *errorListStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type nilVaultForProjectStore struct {
	inner     PerProjectKnowledgeStore
	nilForPID string
}

func (s *nilVaultForProjectStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *nilVaultForProjectStore) OpenProjectVault(ctx context.Context, pid string) (ProjectVault, error) {
	if pid == s.nilForPID {
		return nil, nil
	}
	return s.inner.OpenProjectVault(ctx, pid)
}
func (s *nilVaultForProjectStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type errorVaultForProjectStore struct {
	inner     PerProjectKnowledgeStore
	errForPID string
}

func (s *errorVaultForProjectStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *errorVaultForProjectStore) OpenProjectVault(ctx context.Context, pid string) (ProjectVault, error) {
	if pid == s.errForPID {
		return nil, errors.New("errorVaultForProjectStore: vault error")
	}
	return s.inner.OpenProjectVault(ctx, pid)
}
func (s *errorVaultForProjectStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type notDBVaultForProjectStore struct {
	inner       PerProjectKnowledgeStore
	notDBForPID string
}

func (s *notDBVaultForProjectStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *notDBVaultForProjectStore) OpenProjectVault(ctx context.Context, pid string) (ProjectVault, error) {
	if pid == s.notDBForPID {
		return "not-a-db", nil
	}
	return s.inner.OpenProjectVault(ctx, pid)
}
func (s *notDBVaultForProjectStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}
