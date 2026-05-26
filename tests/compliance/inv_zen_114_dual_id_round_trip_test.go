package compliance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter"
	"github.com/cbip-solutions/hades-system/internal/projectctx"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestInvZen114DualIDRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	tomlPath := filepath.Join(tmp, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte("[project]\nid = \"test-alias\"\n"), 0o644); err != nil {
		t.Fatalf("write zenswarm.toml: %v", err)
	}

	canonical, err := projectctx.CanonicalPath(tmp)
	if err != nil {
		t.Fatalf("CanonicalPath: %v", err)
	}
	expectedID, err := projectctx.ResolveProjectID(canonical)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	resolvedAlias, err := projectctx.ResolveAlias(canonical)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if resolvedAlias != "test-alias" {
		t.Errorf("ResolveAlias = %q, want test-alias", resolvedAlias)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	a := projectctxadapter.New(s)

	ctx := context.Background()
	res, err := projectctx.Activate(ctx, a, canonical, resolvedAlias)
	if err != nil {
		t.Fatalf("Activate(1): %v", err)
	}
	if res.Project.Alias != "test-alias" {
		t.Errorf("Project.Alias = %q, want test-alias", res.Project.Alias)
	}
	if res.Project.ID != expectedID {
		t.Errorf("Project.ID = %q, want %q", res.Project.ID, expectedID)
	}
	if !res.IsFirstActivation {
		t.Errorf("IsFirstActivation = false, want true")
	}

	res2, err := projectctx.Activate(ctx, a, canonical, resolvedAlias)
	if err != nil {
		t.Fatalf("Activate(2): %v", err)
	}
	if res2.Project.Alias != res.Project.Alias {
		t.Errorf("alias drift: %q vs %q", res2.Project.Alias, res.Project.Alias)
	}
	if res2.Project.ID != res.Project.ID {
		t.Errorf("id drift: %q vs %q", res2.Project.ID, res.Project.ID)
	}
	if res2.IsFirstActivation {
		t.Errorf("IsFirstActivation = true on second activate, want false")
	}

	got, err := a.GetByAlias(ctx, resolvedAlias)
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("GetByAlias returned nil")
	}
	if got.ID != expectedID {
		t.Errorf("GetByAlias.ID = %q, want %q", got.ID, expectedID)
	}
	if got.Alias != "test-alias" {
		t.Errorf("GetByAlias.Alias = %q, want test-alias", got.Alias)
	}
	if got.CanonicalPath != canonical {
		t.Errorf("GetByAlias.CanonicalPath = %q, want %q", got.CanonicalPath, canonical)
	}

	gotByID, err := a.GetByID(ctx, expectedID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if gotByID == nil {
		t.Fatal("GetByID returned nil")
	}
	if gotByID.Alias != "test-alias" {
		t.Errorf("GetByID.Alias = %q, want test-alias", gotByID.Alias)
	}
}

func TestInvZen114PathFormDoesNotAffectID(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	id1, _ := projectctx.ResolveProjectID(tmp)
	id2, _ := projectctx.ResolveProjectID(tmp + "/")
	id3, _ := projectctx.ResolveProjectID(filepath.Clean(tmp + "/./"))
	if id1 != id2 || id1 != id3 {
		t.Errorf("path-form drift: %q vs %q vs %q", id1, id2, id3)
	}
}

func TestInvZen114SymlinkResolvesToCanonical(t *testing.T) {
	if isWindows() {
		t.Skip("symlink semantics differ on Windows")
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	idTarget, _ := projectctx.ResolveProjectID(target)
	idLink, _ := projectctx.ResolveProjectID(link)
	if idTarget != idLink {
		t.Errorf("symlink resolution drift: target=%q link=%q (must be equal post EvalSymlinks)", idTarget, idLink)
	}
}

func isWindows() bool { return strings.Contains(string(os.PathSeparator), "\\") }
