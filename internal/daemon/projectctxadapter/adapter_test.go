package projectctxadapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/projectctx"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAdapterImplementsProjectStore(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	var _ projectctx.ProjectStore = a
	if a == nil {
		t.Fatal("New returned nil")
	}
}

func TestAdapterNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil store; got none")
		}
	}()
	_ = New(nil)
}

func TestAdapterGetByAliasNotFound(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	got, err := a.GetByAlias(context.Background(), "missing-alias")
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestAdapterGetByIDNotFound(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	got, err := a.GetByID(context.Background(), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestAdapterInsertThenGetByAlias(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p := &projectctx.Project{
		ID:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := a.GetByAlias(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("got nil after Insert")
	}
	if got.ID != p.ID {
		t.Errorf("ID = %s, want %s", got.ID, p.ID)
	}
	if got.Alias != p.Alias {
		t.Errorf("Alias = %s, want %s", got.Alias, p.Alias)
	}
	if got.CanonicalPath != p.CanonicalPath {
		t.Errorf("CanonicalPath = %s, want %s", got.CanonicalPath, p.CanonicalPath)
	}
	if !got.FirstSeenAt.Equal(p.FirstSeenAt) {
		t.Errorf("FirstSeenAt = %v, want %v", got.FirstSeenAt, p.FirstSeenAt)
	}
	if !got.LastSeenAt.Equal(p.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, p.LastSeenAt)
	}
	if got.IsArchived() {
		t.Errorf("IsArchived() = true, want false (active project)")
	}
}

func TestAdapterInsertThenGetByID(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p := &projectctx.Project{
		ID:            "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		Alias:         "nexus",
		CanonicalPath: "/path/to/projects/nexus",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := a.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("got nil after Insert")
	}
	if got.Alias != "nexus" {
		t.Errorf("Alias = %s, want nexus", got.Alias)
	}
}

func TestAdapterInsertAliasCollision(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p1 := &projectctx.Project{
		ID:            "0000000000000000000000000000000000000000000000000000000000000001",
		Alias:         "duplicated",
		CanonicalPath: "/path/one",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	p2 := &projectctx.Project{
		ID:            "0000000000000000000000000000000000000000000000000000000000000002",
		Alias:         "duplicated",
		CanonicalPath: "/path/two",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p1); err != nil {
		t.Fatalf("Insert p1: %v", err)
	}
	err := a.Insert(ctx, p2)
	if err == nil {
		t.Fatalf("expected UNIQUE-violation error on duplicated alias; got nil")
	}

	if !errors.Is(err, store.ErrDuplicateAlias) {
		t.Errorf("err does not chain to store.ErrDuplicateAlias: %v", err)
	}
}

func TestAdapterInsertNilProject(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	if err := a.Insert(context.Background(), nil); err == nil {
		t.Error("expected error on nil project; got nil")
	}
}

func TestAdapterAppendPathHistoryNilEntry(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	if err := a.AppendPathHistory(context.Background(), nil); err == nil {
		t.Error("expected error on nil entry; got nil")
	}
}

func TestAdapterAppendAndGetPathHistory(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	id := projectctx.ProjectID("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	alias := projectctx.Alias("internal-platform-x")
	p := &projectctx.Project{
		ID:            id,
		Alias:         alias,
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{
		ProjectID:   id,
		Path:        "/path/to/projects/internal-platform-x",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("AppendPathHistory(first): %v", err)
	}
	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{
		ProjectID:   id,
		Path:        "/path/to/internal-platform-x-relocated",
		FirstSeenAt: now.Add(time.Hour),
		LastSeenAt:  now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("AppendPathHistory(second): %v", err)
	}
	hist, err := a.GetPathHistory(ctx, alias)
	if err != nil {
		t.Fatalf("GetPathHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Errorf("len(history) = %d, want 2", len(hist))
	}

	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{
		ProjectID:   id,
		Path:        "/path/to/projects/internal-platform-x",
		FirstSeenAt: now.Add(2 * time.Hour),
		LastSeenAt:  now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("AppendPathHistory(upsert): %v", err)
	}
	hist2, _ := a.GetPathHistory(ctx, alias)
	if len(hist2) != 2 {
		t.Errorf("len(history) after upsert = %d, want 2 (no new row)", len(hist2))
	}

	for _, h := range hist2 {
		if h.ProjectID != id {
			t.Errorf("history ProjectID = %s, want %s", h.ProjectID, id)
		}
		if h.Path == "" {
			t.Error("history Path is empty")
		}
		if h.FirstSeenAt.IsZero() || h.LastSeenAt.IsZero() {
			t.Error("history times are zero")
		}
	}
}

func TestAdapterGetPathHistoryEmpty(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	hist, err := a.GetPathHistory(context.Background(), "no-such-alias")
	if err != nil {
		t.Fatalf("GetPathHistory: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("len(history) = %d, want 0 for missing alias", len(hist))
	}
}

func TestAdapterUpdateLastSeen(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p := &projectctx.Project{
		ID:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	later := now.Add(24 * time.Hour)
	if err := a.UpdateLastSeen(ctx, "internal-platform-x", later); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}
	got, _ := a.GetByAlias(ctx, "internal-platform-x")
	if got == nil {
		t.Fatal("nil")
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, later)
	}
	if !got.FirstSeenAt.Equal(now) {
		t.Errorf("FirstSeenAt = %v, want %v (must not change)", got.FirstSeenAt, now)
	}
}

func TestAdapterUpdateLastSeenMissingAlias(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	if err := a.UpdateLastSeen(context.Background(), "no-such-alias", time.Now()); err == nil {
		t.Error("expected error on missing alias; got nil")
	}
}

func TestAdapterArchive(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p := &projectctx.Project{
		ID:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := a.Archive(ctx, "internal-platform-x"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, err := a.GetByAlias(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("GetByAlias post-Archive: %v", err)
	}
	if got == nil {
		t.Fatal("Archive should not delete; row should remain with archived flag")
	}
	if !got.IsArchived() {
		t.Errorf("IsArchived() = false, want true")
	}
	if got.ArchivedAt == nil || got.ArchivedAt.IsZero() {
		t.Errorf("ArchivedAt is nil or zero, want non-nil non-zero timestamp")
	}
}

func TestAdapterArchiveMissingAlias(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	if err := a.Archive(context.Background(), "no-such-alias"); err == nil {
		t.Error("expected error on missing alias; got nil")
	}
}

func TestAdapterRemove(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	p := &projectctx.Project{
		ID:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{
		ProjectID:   p.ID,
		Path:        p.CanonicalPath,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("AppendPathHistory: %v", err)
	}
	if err := a.Remove(ctx, "internal-platform-x"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err := a.GetByAlias(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("GetByAlias post-Remove: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil after Remove", got)
	}
	hist, _ := a.GetPathHistory(ctx, "internal-platform-x")
	if len(hist) != 0 {
		t.Errorf("len(history) post-Remove = %d, want 0 (cascade)", len(hist))
	}
}

func TestAdapterRemoveMissingAlias(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	if err := a.Remove(context.Background(), "no-such-alias"); err == nil {
		t.Error("expected error on missing alias; got nil")
	}
}

func TestAdapterContextCancellation(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := a.GetByAlias(ctx, "anything"); err == nil {
		t.Error("GetByAlias: expected ctx error, got nil")
	}
	if _, err := a.GetByID(ctx, "anything"); err == nil {
		t.Error("GetByID: expected ctx error, got nil")
	}
	if err := a.Insert(ctx, &projectctx.Project{
		ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Alias: "x", CanonicalPath: "/x",
	}); err == nil {
		t.Error("Insert: expected ctx error, got nil")
	}
	if err := a.UpdateLastSeen(ctx, "x", time.Now()); err == nil {
		t.Error("UpdateLastSeen: expected ctx error, got nil")
	}
	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{ProjectID: "x", Path: "/x"}); err == nil {
		t.Error("AppendPathHistory: expected ctx error, got nil")
	}
	if _, err := a.GetPathHistory(ctx, "x"); err == nil {
		t.Error("GetPathHistory: expected ctx error, got nil")
	}
	if err := a.Archive(ctx, "x"); err == nil {
		t.Error("Archive: expected ctx error, got nil")
	}
	if err := a.Remove(ctx, "x"); err == nil {
		t.Error("Remove: expected ctx error, got nil")
	}
	if _, err := a.List(ctx, false); err == nil {
		t.Error("List: expected ctx error, got nil")
	}
}

func TestAdapterFieldByFieldRoundTrip(t *testing.T) {

	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC().Round(time.Second)
	p := &projectctx.Project{
		ID:            projectctx.ProjectID("abc123def456abc123def456abc123def456abc123def456abc123def4567890"),
		Alias:         projectctx.Alias("zen-swarm"),
		CanonicalPath: "/path/to/projects/zen-swarm",
		FirstSeenAt:   now,
		LastSeenAt:    now.Add(time.Hour),
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := a.GetByAlias(ctx, p.Alias)
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got.ID != p.ID || got.Alias != p.Alias || got.CanonicalPath != p.CanonicalPath {
		t.Errorf("round-trip drift: got %+v, want %+v", got, p)
	}
	if !got.FirstSeenAt.Equal(p.FirstSeenAt) || !got.LastSeenAt.Equal(p.LastSeenAt) {
		t.Errorf("time drift: got first=%v last=%v, want first=%v last=%v", got.FirstSeenAt, got.LastSeenAt, p.FirstSeenAt, p.LastSeenAt)
	}
	if got.IsArchived() {
		t.Errorf("IsArchived = true on active project")
	}
}

func TestAdapterList(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()

	p1 := &projectctx.Project{
		ID:            "0000000000000000000000000000000000000000000000000000000000000010",
		Alias:         "active-project",
		CanonicalPath: "/p/active",
		FirstSeenAt:   now,
		LastSeenAt:    now.Add(2 * time.Hour),
	}
	p2 := &projectctx.Project{
		ID:            "0000000000000000000000000000000000000000000000000000000000000020",
		Alias:         "archived-project",
		CanonicalPath: "/p/archived",
		FirstSeenAt:   now,
		LastSeenAt:    now.Add(time.Hour),
	}
	if err := a.Insert(ctx, p1); err != nil {
		t.Fatalf("Insert p1: %v", err)
	}
	if err := a.Insert(ctx, p2); err != nil {
		t.Fatalf("Insert p2: %v", err)
	}
	if err := a.Archive(ctx, "archived-project"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	active, err := a.List(ctx, false)
	if err != nil {
		t.Fatalf("List(false): %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("len(List(false)) = %d, want 1", len(active))
	}
	if active[0].Alias != "active-project" {
		t.Errorf("List(false)[0].Alias = %s, want active-project", active[0].Alias)
	}
	if active[0].IsArchived() {
		t.Errorf("List(false) returned archived project")
	}

	all, err := a.List(ctx, true)
	if err != nil {
		t.Fatalf("List(true): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(List(true)) = %d, want 2", len(all))
	}

	if all[0].Alias != "active-project" {
		t.Errorf("List(true)[0].Alias = %s, want active-project (DESC by last_seen)", all[0].Alias)
	}

	var found *projectctx.Project
	for i := range all {
		if all[i].Alias == "archived-project" {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatal("archived-project missing from List(true)")
	}
	if !found.IsArchived() {
		t.Errorf("archived-project IsArchived() = false")
	}
}

func TestAdapterListEmpty(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	got, err := a.List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(List) = %d, want 0 on empty store", len(got))
	}
}

func TestAdapterStoreErrorPropagation(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()

	if err := s.DB().Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	if _, err := a.GetByAlias(ctx, "x"); err == nil {
		t.Error("GetByAlias: expected error after DB close, got nil")
	}
	if _, err := a.GetByID(ctx, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err == nil {
		t.Error("GetByID: expected error after DB close, got nil")
	}
	if err := a.Insert(ctx, &projectctx.Project{ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Alias: "x", CanonicalPath: "/x"}); err == nil {
		t.Error("Insert: expected error after DB close, got nil")
	}
	if err := a.UpdateLastSeen(ctx, "x", time.Now()); err == nil {
		t.Error("UpdateLastSeen: expected error after DB close, got nil")
	}
	if err := a.AppendPathHistory(ctx, &projectctx.PathHistoryEntry{ProjectID: "x", Path: "/x"}); err == nil {
		t.Error("AppendPathHistory: expected error after DB close, got nil")
	}
	if _, err := a.GetPathHistory(ctx, "x"); err == nil {
		t.Error("GetPathHistory: expected error after DB close, got nil")
	}
	if err := a.Archive(ctx, "x"); err == nil {
		t.Error("Archive: expected error after DB close, got nil")
	}
	if err := a.Remove(ctx, "x"); err == nil {
		t.Error("Remove: expected error after DB close, got nil")
	}
	if _, err := a.List(ctx, false); err == nil {
		t.Error("List: expected error after DB close, got nil")
	}
}

func TestAdapterProjectToRowArchivedAtNonNil(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	archivedT := now.Add(2 * time.Hour)
	p := &projectctx.Project{
		ID:            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias:         "preexisting-archived",
		CanonicalPath: "/p/restored",
		FirstSeenAt:   now,
		LastSeenAt:    now.Add(time.Hour),
		ArchivedAt:    &archivedT,
	}
	if err := a.Insert(ctx, p); err != nil {
		t.Fatalf("Insert pre-archived: %v", err)
	}
	got, err := a.GetByAlias(ctx, p.Alias)
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("nil after Insert")
	}
	if !got.IsArchived() {
		t.Errorf("IsArchived() = false; want true on pre-archived insert")
	}
	if got.ArchivedAt == nil {
		t.Fatal("ArchivedAt is nil; want non-nil")
	}
	if !got.ArchivedAt.Equal(archivedT) {
		t.Errorf("ArchivedAt = %v, want %v", got.ArchivedAt, archivedT)
	}
}
