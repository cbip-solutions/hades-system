package projectsaliasadapter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
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

func canonicalID(suffix string) string {

	base := suffix + strings.Repeat("0", 64-len(suffix))
	return base[:64]
}

func seedAlias(t *testing.T, s *store.Store, alias, idSha256, canonicalPath string, archivedMs int64) {
	t.Helper()
	if err := store.InsertProjectAlias(s.DB(), store.ProjectAliasRow{
		IDSha256:      idSha256,
		Alias:         alias,
		CanonicalPath: canonicalPath,
		FirstSeenAt:   time.Now().UnixMilli(),
		LastSeenAt:    time.Now().UnixMilli(),
		ArchivedAt:    archivedMs,
	}); err != nil {
		t.Fatalf("InsertProjectAlias(%s): %v", alias, err)
	}
}

func TestNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil store; got none")
		}
	}()
	_ = New(nil)
}

func TestAdapterImplementsResolverInterface(t *testing.T) {
	s := newTestStore(t)
	var _ mcpgateway.ProjectsAliasResolver = New(s)
}

func TestResolveRawSha256PassThrough(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("aa11")
	got, err := a.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve(64-hex): %v; expected pass-through nil err", err)
	}
	if got != id {
		t.Errorf("got %q, want %q (pass-through identity)", got, id)
	}
}

func TestResolveAliasLookup(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("bb22")
	seedAlias(t, s, "test-bb22", id, "/tmp/test-bb22", 0)
	got, err := a.Resolve(context.Background(), "test-bb22")
	if err != nil {
		t.Fatalf("Resolve(alias): %v", err)
	}
	if got != id {
		t.Errorf("got %q, want %q (alias→id_sha256 resolution)", got, id)
	}
}

func TestResolveAliasNotFound(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	_, err := a.Resolve(context.Background(), "ghost-alias")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(ghost) err = %v; expected errors.Is ErrAliasNotFound", err)
	}
}

// TestResolveArchivedSkipped — anchor 4: an archived alias is NOT
// resolvable via Resolve (the daemon's MCP ingress treats archived
// projects as inactive). Verified by seeding a row with archived_at !=
// NULL, then asserting Resolve returns ErrAliasNotFound.
//
// Operator UX rationale: archive is a soft-delete; the row stays for
// audit + path-history. Active dispatch (caronte engine, MCP tools) MUST
// refuse archived rows so the operator gets a clear "archived" hint
// instead of routing to a zombie project.
func TestResolveArchivedSkipped(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("cc33")
	seedAlias(t, s, "test-cc33", id, "/tmp/test-cc33", time.Now().UnixMilli())
	_, err := a.Resolve(context.Background(), "test-cc33")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(archived) err = %v; expected ErrAliasNotFound (archived rows excluded)", err)
	}
}

func TestResolveCacheHit(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("dd44")
	seedAlias(t, s, "test-dd44", id, "/tmp/test-dd44", 0)

	first, err := a.Resolve(context.Background(), "test-dd44")
	if err != nil || first != id {
		t.Fatalf("Resolve(1): got %q, %v; want %q, nil", first, err, id)
	}

	if _, err := s.DB().Exec(`DELETE FROM projects_alias WHERE alias = ?`, "test-dd44"); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	second, err := a.Resolve(context.Background(), "test-dd44")
	if err != nil {
		t.Fatalf("Resolve(2): %v; cache should have served the result", err)
	}
	if second != id {
		t.Errorf("Resolve(2) got %q, want %q (cache hit), DB row was deleted", second, id)
	}
}

func TestResolveCacheTTLExpiry(t *testing.T) {
	s := newTestStore(t)
	a := New(s)

	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	now := t0
	a.now = func() time.Time { return now }

	id := canonicalID("ee55")
	seedAlias(t, s, "test-ee55", id, "/tmp/test-ee55", 0)

	first, err := a.Resolve(context.Background(), "test-ee55")
	if err != nil || first != id {
		t.Fatalf("Resolve(1): got %q, %v; want %q, nil", first, err, id)
	}

	if _, err := s.DB().Exec(`DELETE FROM projects_alias WHERE alias = ?`, "test-ee55"); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	now = t0.Add(61 * time.Second)

	_, err = a.Resolve(context.Background(), "test-ee55")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(2 post-TTL) err = %v; expected ErrAliasNotFound after cache expired + row deleted", err)
	}
}

func TestResolveCacheWithinTTL(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	now := t0
	a.now = func() time.Time { return now }

	id := canonicalID("ff66")
	seedAlias(t, s, "test-ff66", id, "/tmp/test-ff66", 0)

	if _, err := a.Resolve(context.Background(), "test-ff66"); err != nil {
		t.Fatalf("Resolve(1): %v", err)
	}

	if _, err := s.DB().Exec(`DELETE FROM projects_alias WHERE alias = ?`, "test-ff66"); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	now = t0.Add(30 * time.Second)

	got, err := a.Resolve(context.Background(), "test-ff66")
	if err != nil {
		t.Errorf("Resolve(2 within TTL): %v; expected cache hit", err)
	}
	if got != id {
		t.Errorf("Resolve(2 within TTL) = %q, want %q (cache hit)", got, id)
	}
}

func TestResolveRawIDDBFallback(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("aa77")
	seedAlias(t, s, "test-aa77", id, "/tmp/test-aa77", 0)

	got, err := a.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve(raw-id): %v", err)
	}
	if got != id {
		t.Errorf("got %q, want %q (pass-through, no DB)", got, id)
	}
}

func TestInvalidate(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("bb88")
	seedAlias(t, s, "test-bb88", id, "/tmp/test-bb88", 0)

	if _, err := a.Resolve(context.Background(), "test-bb88"); err != nil {
		t.Fatalf("Resolve(warm): %v", err)
	}

	a.Invalidate("test-bb88")
	if _, err := s.DB().Exec(`DELETE FROM projects_alias WHERE alias = ?`, "test-bb88"); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	_, err := a.Resolve(context.Background(), "test-bb88")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(post-invalidate) err = %v; expected ErrAliasNotFound", err)
	}
}

func TestInvalidateAll(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	idA := canonicalID("0aa1")
	idB := canonicalID("0bb1")
	seedAlias(t, s, "test-0aa1", idA, "/tmp/test-0aa1", 0)
	seedAlias(t, s, "test-0bb1", idB, "/tmp/test-0bb1", 0)

	if _, err := a.Resolve(context.Background(), "test-0aa1"); err != nil {
		t.Fatalf("warm A: %v", err)
	}
	if _, err := a.Resolve(context.Background(), "test-0bb1"); err != nil {
		t.Fatalf("warm B: %v", err)
	}

	a.InvalidateAll()

	if _, err := s.DB().Exec(`DELETE FROM projects_alias`); err != nil {
		t.Fatalf("delete rows: %v", err)
	}
	if _, err := a.Resolve(context.Background(), "test-0aa1"); !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(post-InvalidateAll A) err = %v; expected ErrAliasNotFound", err)
	}
	if _, err := a.Resolve(context.Background(), "test-0bb1"); !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(post-InvalidateAll B) err = %v; expected ErrAliasNotFound", err)
	}
}

func TestResolveContextCancelled(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	id := canonicalID("cc99")
	seedAlias(t, s, "test-cc99", id, "/tmp/test-cc99", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Resolve(ctx, "test-cc99")
	if err == nil {
		t.Error("Resolve(cancelled ctx) returned nil err; expected ctx cancellation surfaced")
	}
}

func TestResolveEmptyInput(t *testing.T) {
	s := newTestStore(t)
	a := New(s)
	_, err := a.Resolve(context.Background(), "")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(empty) err = %v; expected ErrAliasNotFound", err)
	}
}

func TestResolveDBError(t *testing.T) {
	s := newTestStore(t)
	a := New(s)

	if err := s.DB().Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	_, err := a.Resolve(context.Background(), "any-alias")
	if err == nil {
		t.Fatal("Resolve(closed-DB) returned nil err; expected wrapped DB error")
	}
	if errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(closed-DB) err = %v; must NOT alias to ErrAliasNotFound (infra failure ≠ missing row)", err)
	}
}
