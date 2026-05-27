// tests/compliance/inv_zen_277_test.go
//
// Compliance gate for invariant: project alias
// resolution is implemented by internal/daemon/projectsaliasadapter and
// translates BOTH raw 64-hex id_sha256 (pass-through) AND human aliases
// (e.g. "zen-swarm-3572a35b") to the canonical id_sha256 the caronte
// engine + caronteadapter consume.
//
// Four anchors per phase plan §Task A-5:
//
// 1. source-regex 1: file `internal/daemon/projectsaliasadapter/adapter.go`
// exists (sentinel against a future refactor that moves the package
// without updating the wiring in cmd/zen-swarm-ctld/main.go).
// 2. source-regex 2: the `hexID.MatchString(idOrAlias)` pass-through
// branch is present — 64-hex inputs MUST be recognised statically
// without a DB round-trip.
// 3. source-regex 3: the `alias = ? OR id_sha256 = ?` SQL query is
// present — the resolver MUST accept BOTH columns in a single query
// .
// 4. behavioural test: an in-memory daemon-shared SQLite is seeded
// with one row; Resolve(alias)+Resolve(id_sha256)+Resolve("unknown")
// produce the expected canonical id, canonical id (pass-through),
// and ErrAliasNotFound respectively.
//
// Sister-test pattern (feedback_sister_test_pattern): bite-check is to
// revert adapter.go to a single-column query (`WHERE id_sha256 = ?`);
// this test MUST fail on the alias-lookup behavioural anchor.
//
// invariant.
package compliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectsaliasadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestInvZen277SourceRegex_AdapterFileExists(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal", "daemon", "projectsaliasadapter", "adapter.go")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("inv-zen-277 violated: %s missing (err=%v); adapter package was moved or deleted", path, err)
	}
}

func TestInvZen277SourceRegex_HexPassThrough(t *testing.T) {
	src := readSourceForInv276277(t, "internal/daemon/projectsaliasadapter/adapter.go")
	const needle = `hexID.MatchString(idOrAlias)`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-277 violated: adapter.go missing %q pass-through; 64-hex inputs incur unnecessary DB lookups", needle)
	}
}

func TestInvZen277SourceRegex_DualColumnQuery(t *testing.T) {
	src := readSourceForInv276277(t, "internal/daemon/projectsaliasadapter/adapter.go")
	const needle = `alias = ? OR id_sha256 = ?`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-277 violated: adapter.go missing %q query; resolver no longer accepts alias inputs", needle)
	}
}

// TestInvZen277SourceRegex_ExcludeArchived ensures archived rows are
// excluded by the resolver query. Operator UX: archive = soft-delete;
// archived projects MUST refuse dispatch (engine + adapter would
// otherwise route to a zombie project).
func TestInvZen277SourceRegex_ExcludeArchived(t *testing.T) {
	src := readSourceForInv276277(t, "internal/daemon/projectsaliasadapter/adapter.go")
	const needle = `archived_at IS NULL`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-277 violated: adapter.go missing %q clause; archived projects may still resolve and route", needle)
	}
}

func TestInvZen277Behavioural_AliasAndCanonicalRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	const canonical = "aa1100000000000000000000000000000000000000000000000000000000abcd"
	const alias = "test-aa11"
	now := time.Now().UnixMilli()
	if err := store.InsertProjectAlias(s.DB(), store.ProjectAliasRow{
		IDSha256:      canonical,
		Alias:         alias,
		CanonicalPath: "/tmp/test-aa11",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}); err != nil {
		t.Fatalf("seed projects_alias: %v", err)
	}

	a := projectsaliasadapter.New(s)

	got, err := a.Resolve(context.Background(), alias)
	if err != nil {
		t.Errorf("Resolve(alias=%q): %v; want canonical=%q", alias, err, canonical)
	}
	if got != canonical {
		t.Errorf("Resolve(alias=%q) = %q; want canonical=%q", alias, got, canonical)
	}

	got, err = a.Resolve(context.Background(), canonical)
	if err != nil {
		t.Errorf("Resolve(canonical=%q): %v; pass-through should succeed", canonical, err)
	}
	if got != canonical {
		t.Errorf("Resolve(canonical=%q) = %q; want %q (identity pass-through)", canonical, got, canonical)
	}

	_, err = a.Resolve(context.Background(), "no-such-alias")
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(unknown) err = %v; want errors.Is mcpgateway.ErrAliasNotFound", err)
	}
}

func TestInvZen277Behavioural_ArchivedExcluded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	const canonical = "bb2200000000000000000000000000000000000000000000000000000000abcd"
	const alias = "test-bb22"
	now := time.Now().UnixMilli()
	if err := store.InsertProjectAlias(s.DB(), store.ProjectAliasRow{
		IDSha256:      canonical,
		Alias:         alias,
		CanonicalPath: "/tmp/test-bb22",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		ArchivedAt:    now,
	}); err != nil {
		t.Fatalf("seed projects_alias (archived): %v", err)
	}

	a := projectsaliasadapter.New(s)
	_, err = a.Resolve(context.Background(), alias)
	if !errors.Is(err, mcpgateway.ErrAliasNotFound) {
		t.Errorf("Resolve(archived alias=%q) err = %v; want errors.Is mcpgateway.ErrAliasNotFound (archived rows excluded)", alias, err)
	}
}
