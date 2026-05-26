package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCreatesParentDirsWithCorrectPerms(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, "deep", "nested", "zen-swarm"),
		Kind: LocationKindProjectScope,
	}
	if _, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("name=\"x\"\n"), Scope: "project"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	st, err := os.Stat(loc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("plugin dir mode = %v, want 0o700 (operator-private)", st.Mode().Perm())
	}
}

func TestInstallReturnsErrOnPathConflict(t *testing.T) {
	tmp := t.TempDir()

	conflictPath := filepath.Join(tmp, "conflict")
	if err := os.WriteFile(conflictPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := Location{Path: conflictPath, Kind: LocationKindProjectScope}
	_, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("name=\"x\"\n"), Scope: "project"})
	if err == nil {
		t.Error("Install: expected error on file-conflict, got nil")
	}
}

func TestInstallRejectsEmptyPath(t *testing.T) {
	_, err := Install(context.Background(), InstallOptions{
		Location: Location{Path: "", Kind: LocationKindProjectScope},
		Manifest: []byte("x"),
		Scope:    "project",
	})
	if err == nil {
		t.Error("Install with empty Path should error")
	}
}

func TestInstallRejectsInvalidKind(t *testing.T) {
	tmp := t.TempDir()
	_, err := Install(context.Background(), InstallOptions{
		Location: Location{Path: filepath.Join(tmp, "x"), Kind: LocationKindUnknown},
		Manifest: []byte("x"),
		Scope:    "project",
	})
	if err == nil {
		t.Error("Install with invalid Kind should error")
	}
}

func TestInstallRespectsCanceledContext(t *testing.T) {
	tmp := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Install(ctx, InstallOptions{
		Location: Location{Path: filepath.Join(tmp, "x"), Kind: LocationKindProjectScope},
		Manifest: []byte("x"),
		Scope:    "project",
	})
	if err == nil {
		t.Fatal("Install with canceled ctx should error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled (wrapped)", err)
	}
}

func TestInstallUserScopeAppendsSlug(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, ".hermes", "plugins", "zen-swarm-base"),
		Kind: LocationKindUserScope,
	}
	slug := "myproj-abc12345"
	canonical, err := Install(context.Background(), InstallOptions{
		Location: loc,
		Manifest: []byte("name=\"x\"\n"),
		Scope:    "user",
		Slug:     slug,
	})
	if err != nil {
		t.Fatalf("Install user-scope: %v", err)
	}

	st, err := os.Stat(canonical)
	if err != nil {
		t.Fatalf("canonical dir missing: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("canonical = %q is not a directory", canonical)
	}
}

func TestUninstallRemovesDir(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, ".hermes", "plugins", "zen-swarm"),
		Kind: LocationKindProjectScope,
	}
	if _, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("x"), Scope: "project"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(loc); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(loc.Path); !os.IsNotExist(err) {
		t.Errorf("dir still exists after Uninstall; err=%v", err)
	}
}

func TestUninstallRejectsEmptyPath(t *testing.T) {
	if err := Uninstall(Location{Path: "", Kind: LocationKindProjectScope}); err == nil {
		t.Error("Uninstall with empty Path should error")
	}
}

func TestUninstallNonExistentIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, "never-installed"),
		Kind: LocationKindProjectScope,
	}

	if err := Uninstall(loc); err != nil {
		t.Errorf("Uninstall non-existent: %v", err)
	}
}

func TestInstallMkdirFailureOnReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX directory perm enforcement")
	}
	tmp := t.TempDir()
	roParent := filepath.Join(tmp, "ro-parent")
	if err := os.MkdirAll(roParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roParent, 0o700) })

	target := filepath.Join(roParent, "child", "plugin")
	loc := Location{Path: target, Kind: LocationKindProjectScope}
	_, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("x"), Scope: "project"})
	if err == nil {
		t.Error("Install: expected error on read-only parent, got nil")
	}
}

func TestInstallWriteFileFailsWhenTmpIsDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plugin")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(target, "plugin.toml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	loc := Location{Path: target, Kind: LocationKindProjectScope}
	_, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("x"), Scope: "project"})
	if err == nil {
		t.Error("Install: expected error when tmp path is a directory, got nil")
	}
}

func TestInstallRenameFailsWhenDestIsDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plugin")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(target, "plugin.toml")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "marker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := Location{Path: target, Kind: LocationKindProjectScope}
	_, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: []byte("x"), Scope: "project"})
	if err == nil {
		t.Error("Install: expected error when dest is a non-empty directory, got nil")
	}
}

func TestUninstallReadOnlyParentReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX directory perm enforcement")
	}
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "ro-parent")
	target := filepath.Join(parent, "plugin")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(target, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	err := Uninstall(Location{Path: target, Kind: LocationKindProjectScope})
	if err == nil {
		t.Error("Uninstall: expected error under read-only parent, got nil")
	}
}
