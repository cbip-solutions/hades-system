//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type promoteFixture struct {
	agg        *Aggregator
	pinDB      *sql.DB
	projectDB  *sql.DB
	projectID  string
	noteID     string
	operatorID string
	store      *trackingStore
	fixedTime  time.Time
}

type trackingStore struct {
	inner   PerProjectKnowledgeStore
	updates []struct{ project, note, anchor string }
}

func (s *trackingStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}

func (s *trackingStore) OpenProjectVault(ctx context.Context, projectID string) (ProjectVault, error) {
	return s.inner.OpenProjectVault(ctx, projectID)
}

func (s *trackingStore) UpdateAuditChainAnchor(ctx context.Context, projectID, noteID, anchor string) error {
	s.updates = append(s.updates, struct{ project, note, anchor string }{projectID, noteID, anchor})
	return s.inner.UpdateAuditChainAnchor(ctx, projectID, noteID, anchor)
}

func setupPromoteFixture(t *testing.T) *promoteFixture {
	t.Helper()
	tempDir := t.TempDir()
	fixedTime := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)

	pinDBPath := filepath.Join(tempDir, "pin.db")
	pinDB, err := Open(context.Background(), pinDBPath)
	if err != nil {
		t.Fatalf("Open pinDB: %v", err)
	}
	if err := Init(context.Background(), pinDB); err != nil {
		_ = pinDB.Close()
		t.Fatalf("Init pinDB: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	projectDBPath := filepath.Join(tempDir, "project.db")
	projectDB, err := Open(context.Background(), projectDBPath)
	if err != nil {
		t.Fatalf("Open projectDB: %v", err)
	}
	if err := Init(context.Background(), projectDB); err != nil {
		_ = projectDB.Close()
		t.Fatalf("Init projectDB: %v", err)
	}
	t.Cleanup(func() { _ = projectDB.Close() })

	noteID := "promote-test-note"
	projectID := "test-project"
	seedPinNote(t, projectDB, noteID, projectID, "Test Note Title",
		"content about [[wikilink-target]] and more text", "")

	inner := &mockFederatedStore{
		projects: []ProjectHandle{{ProjectID: projectID, Alias: "test", VaultPath: "/vault/test"}},
		vaults:   map[string]*sql.DB{projectID: projectDB},
	}
	store := &trackingStore{inner: inner}

	embedder := newMockEmbedder(384)
	agg, err := New(Options{
		DB:       pinDB,
		Embedder: embedder,
		Store:    store,
		Clock:    promoteClock{fixedAt: fixedTime},
	})
	if err != nil {
		t.Fatalf("New Aggregator: %v", err)
	}

	return &promoteFixture{
		agg:        agg,
		pinDB:      pinDB,
		projectDB:  projectDB,
		projectID:  projectID,
		noteID:     noteID,
		operatorID: "testuser",
		store:      store,
		fixedTime:  fixedTime,
	}
}

type promoteClock struct {
	fixedAt time.Time
}

func (c promoteClock) Now() time.Time {
	if c.fixedAt.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return c.fixedAt
}

func TestPromoteHappyPath(t *testing.T) {
	fix := setupPromoteFixture(t)

	result, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "initial promote for cross-project index")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if result.NoteID != fix.noteID {
		t.Errorf("result.NoteID = %q; want %q", result.NoteID, fix.noteID)
	}
	if result.AuditChainAnchor == "" {
		t.Error("result.AuditChainAnchor empty; expected non-empty anchor from noopChainAnchorComputer")
	}
	if result.PromotedAt.IsZero() {
		t.Error("result.PromotedAt is zero; expected non-zero timestamp")
	}
	if result.Idempotent {
		t.Error("result.Idempotent = true on first promote; expected false")
	}

	var cnt int
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&cnt); err != nil {
		t.Fatalf("SELECT COUNT pin_index: %v", err)
	}
	if cnt != 1 {
		t.Errorf("pin_index count = %d; want 1", cnt)
	}

	var ftsCount int
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "wikilink").Scan(&ftsCount); err != nil {
		t.Fatalf("SELECT COUNT pin_fts: %v", err)
	}
	if ftsCount == 0 {
		t.Error("FTS index has no rows for promoted note; expected at least 1")
	}

	if len(fix.store.updates) == 0 {
		t.Error("UpdateAuditChainAnchor not called; expected at least 1 call")
	}
}

func TestPromoteEmptyReasonReturnsErrPromoteReasonRequired(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "")
	if err == nil {
		t.Fatal("Promote with empty reason succeeded; expected ErrPromoteReasonRequired")
	}
	if !errors.Is(err, ErrPromoteReasonRequired) {
		t.Errorf("error = %v; want errors.Is(err, ErrPromoteReasonRequired)", err)
	}
}

func TestPromoteEmptyOperator(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, "", "valid reason")
	if err == nil {
		t.Fatal("Promote with empty operatorID succeeded; expected error")
	}
}

func TestPromoteEmptyNoteID(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(), "", fix.projectID, fix.operatorID, "valid reason")
	if err == nil {
		t.Fatal("Promote with empty noteID succeeded; expected error")
	}
}

func TestPromoteEmptyProjectID(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(), fix.noteID, "", fix.operatorID, "valid reason")
	if err == nil {
		t.Fatal("Promote with empty projectID succeeded; expected error")
	}
}

func TestPromoteIdempotentDoublePromote(t *testing.T) {
	fix := setupPromoteFixture(t)

	reason := "first promote"
	if _, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, reason); err != nil {
		t.Fatalf("first Promote: %v", err)
	}

	result2, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, reason)
	if err != nil {
		t.Fatalf("second Promote: %v", err)
	}
	if !result2.Idempotent {
		t.Error("second Promote result.Idempotent = false; expected true")
	}

	var cnt int
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&cnt); err != nil {
		t.Fatalf("SELECT COUNT pin_index: %v", err)
	}
	if cnt != 1 {
		t.Errorf("pin_index count after double promote = %d; want 1", cnt)
	}
}

func TestPromoteNonexistentNote(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(), "does-not-exist", fix.projectID, fix.operatorID, "valid reason")
	if err == nil {
		t.Fatal("Promote nonexistent note succeeded; expected error")
	}
}

func TestPromoteWikilinkExtraction(t *testing.T) {
	fix := setupPromoteFixture(t)

	if _, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "wikilink test"); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	var cnt int
	expectedTarget := fix.projectID + ":wikilink-target"
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_wikilinks WHERE source_note_id = ? AND target_note_id = ?`,
		fix.noteID, expectedTarget,
	).Scan(&cnt); err != nil {
		t.Fatalf("SELECT COUNT wikilinks: %v", err)
	}
	if cnt == 0 {
		t.Errorf("no wikilink edge found for source=%q target=%q; expected 1", fix.noteID, expectedTarget)
	}
}

type failEmbedder struct{ dim int }

func (e *failEmbedder) Dimensions() int { return e.dim }
func (e *failEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("failEmbedder: model fault (test)")
}

func TestPromoteEmbedderFaultGracefulDegrade(t *testing.T) {
	fix := setupPromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: &failEmbedder{dim: 384},
		Store:    fix.store,
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New with failEmbedder: %v", err)
	}

	result, err := agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "embedder fault test")
	if err != nil {
		t.Fatalf("Promote with failing embedder: %v", err)
	}
	if result.NoteID != fix.noteID {
		t.Errorf("result.NoteID = %q; want %q", result.NoteID, fix.noteID)
	}

	var pinCnt int
	if err := fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&pinCnt); err != nil {
		t.Fatalf("SELECT COUNT pin_index: %v", err)
	}
	if pinCnt != 1 {
		t.Errorf("pin_index count = %d; want 1", pinCnt)
	}

	var vecCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_vec WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		fix.noteID,
	).Scan(&vecCnt); err != nil {
		t.Fatalf("SELECT COUNT pin_vec: %v", err)
	}
	if vecCnt != 0 {
		t.Errorf("vec count = %d; want 0 (embedding nil on fault)", vecCnt)
	}
}

type errChain struct{}

func (errChain) ComputeAnchor(_ context.Context, _, _ string, _ []byte, _ time.Time) (string, error) {
	return "", errors.New("errChain: chain unavailable (test)")
}

type failVaultStore struct{ inner PerProjectKnowledgeStore }

func (s *failVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *failVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return nil, errors.New("failVaultStore: vault open error")
}
func (s *failVaultStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

type badVaultStore struct{ inner PerProjectKnowledgeStore }

func (s *badVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *badVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return "not-a-db", nil
}
func (s *badVaultStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

func TestPromoteVaultOpenError(t *testing.T) {
	fix := setupPromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    &failVaultStore{inner: fix.store},
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "vault open error test")
	if err == nil {
		t.Fatal("Promote with vault-open error returned nil; expected error")
	}
}

func TestPromoteVaultNotDB(t *testing.T) {
	fix := setupPromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    &badVaultStore{inner: fix.store},
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "bad vault type test")
	if err == nil {
		t.Fatal("Promote with non-*sql.DB vault returned nil; expected error")
	}
}

func TestPromoteChainAnchorError(t *testing.T) {
	fix := setupPromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    fix.store,
		Chain:    errChain{},
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "chain error test")
	if err == nil {
		t.Fatal("Promote with failing chain returned nil; expected error")
	}
}

func TestPromoteDegradedModeSkipsVec(t *testing.T) {
	fix := setupPromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    fix.store,
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	agg.markDegraded()

	result, err := agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "degraded mode test")
	if err != nil {
		t.Fatalf("Promote in degraded mode: %v", err)
	}
	if result.NoteID != fix.noteID {
		t.Errorf("result.NoteID = %q; want %q", result.NoteID, fix.noteID)
	}

	var pinCnt int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&pinCnt)
	if pinCnt != 1 {
		t.Errorf("pin_index count = %d; want 1", pinCnt)
	}

	var vecCnt int
	fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_vec WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		fix.noteID,
	).Scan(&vecCnt)
	if vecCnt != 0 {
		t.Errorf("vec count = %d; want 0 (degraded mode skips vec)", vecCnt)
	}
}

func TestPromoteIdempotentRefreshesFTSAndVec(t *testing.T) {
	fix := setupPromoteFixture(t)

	if _, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "first promote"); err != nil {
		t.Fatalf("first Promote: %v", err)
	}

	var cnt1 int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "wikilink").Scan(&cnt1)
	if cnt1 == 0 {
		t.Error("FTS 'wikilink' count = 0 after first promote; expected 1")
	}

	result2, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "second promote refresh")
	if err != nil {
		t.Fatalf("second Promote: %v", err)
	}
	if !result2.Idempotent {
		t.Error("second Promote: Idempotent=false; expected true")
	}

	var cnt2 int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&cnt2)
	if cnt2 != 1 {
		t.Errorf("pin_index count after re-promote = %d; want 1", cnt2)
	}

	var cnt3 int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "wikilink").Scan(&cnt3)
	if cnt3 == 0 {
		t.Error("FTS 'wikilink' count = 0 after re-promote; expected 1")
	}

	var vecCnt int
	fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_vec WHERE rowid = (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		fix.noteID,
	).Scan(&vecCnt)
	if vecCnt != 1 {
		t.Errorf("vec count after re-promote = %d; want 1", vecCnt)
	}
}

type closedVaultStore struct {
	inner    PerProjectKnowledgeStore
	closedDB *sql.DB
}

func (s *closedVaultStore) ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error) {
	return s.inner.ListAuthorizedProjects(ctx)
}
func (s *closedVaultStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return s.closedDB, nil
}
func (s *closedVaultStore) UpdateAuditChainAnchor(ctx context.Context, pid, nid, anchor string) error {
	return s.inner.UpdateAuditChainAnchor(ctx, pid, nid, anchor)
}

func TestPromoteSelectNoteError(t *testing.T) {
	fix := setupPromoteFixture(t)

	closedDB, err := Open(context.Background(), t.TempDir()+"/closed.db")
	if err != nil {
		t.Fatalf("Open closedDB: %v", err)
	}
	if err := Init(context.Background(), closedDB); err != nil {
		closedDB.Close()
		t.Fatalf("Init closedDB: %v", err)
	}
	closedDB.Close()

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    &closedVaultStore{inner: fix.store, closedDB: closedDB},
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "closed vault test")
	if err == nil {
		t.Fatal("Promote with closed vault DB returned nil; expected error")
	}
}

func TestPromoteBeginTxError(t *testing.T) {
	fix := setupPromoteFixture(t)

	fix.pinDB.Close()

	_, err := fix.agg.Promote(context.Background(), fix.noteID, fix.projectID, fix.operatorID, "begin tx error test")
	if err == nil {
		t.Fatal("Promote with closed pinDB returned nil; expected BeginTx error")
	}
}

func TestErrPromoteReasonRequiredSentinel(t *testing.T) {
	if ErrPromoteReasonRequired == nil {
		t.Fatal("ErrPromoteReasonRequired is nil; must be a non-nil sentinel")
	}

	wrapped := errors.New("outer: " + ErrPromoteReasonRequired.Error())

	if !errors.Is(ErrPromoteReasonRequired, ErrPromoteReasonRequired) {
		t.Error("errors.Is(ErrPromoteReasonRequired, ErrPromoteReasonRequired) = false; want true")
	}
	_ = wrapped

	msg := ErrPromoteReasonRequired.Error()
	if !containsStr(msg, "inv-zen-146") {
		t.Errorf("ErrPromoteReasonRequired.Error() = %q; must contain \"inv-zen-146\"", msg)
	}
}

func containsStr(s, sub string) bool {
	sl := len(sub)
	if sl == 0 {
		return true
	}
	for i := 0; i+sl <= len(s); i++ {
		if s[i:i+sl] == sub {
			return true
		}
	}
	return false
}
