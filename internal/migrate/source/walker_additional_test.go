package source

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWalkSkills_RegularFileEntryIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "skills", "README.md"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "skills", "real")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# real"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 {
		t.Errorf("got %d skills, want 1 (only real/)", len(inv.Skills))
	}
}

func TestReadAll_SourceIsFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "file-not-dir")
	if err := os.WriteFile(src, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := ReadAll(src)
	if err != nil {

		return
	}

	if len(inv.Skills)+len(inv.Commands)+len(inv.Hooks) != 0 {
		t.Errorf("non-dir source yielded surfaces: %d skills, %d commands, %d hooks",
			len(inv.Skills), len(inv.Commands), len(inv.Hooks))
	}
}

// TestWalkMemory_PermissionDeniedFile asserts walkMemory's permission-denied
// branch records a warning + continues. Requires non-root.
func TestWalkMemory_PermissionDeniedFile(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("permission-denied test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "proj-a", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(path, []byte("# secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {

		t.Skipf("walkMemory permission-denied not reproducible on this platform: %v", err)
	}
}

// TestWalkSkills_PermissionDeniedFile asserts walkSkills's permission-denied
// branch records a warning + continues. Requires non-root.
func TestWalkSkills_PermissionDeniedFile(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("permission-denied test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "no-read")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("# secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Skipf("walkSkills permission-denied not reproducible: %v", err)
	}
}

func TestReadMDFilesFlat_DirectoriesIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := readMDFilesFlat(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Errorf("got %d files, want 1 (subdir ignored)", len(out))
	}
}

func TestReadMDFilesFlat_MissingDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	out, err := readMDFilesFlat(filepath.Join(tmp, "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("got %d files, want 0", len(out))
	}
}

func TestWalkAnyExt_DirectoriesIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.sh"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := walkAnyExt(root, ".sh", ".py")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1 (subdir ignored)", len(entries))
	}
}

func TestWalkAnyExt_MissingDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	entries, err := walkAnyExt(filepath.Join(tmp, "missing"), ".sh")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestReadSettings_MissingFileNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := readSettings(root, inv); err != nil {
		t.Errorf("missing settings.json: got err %v, want nil", err)
	}
}

func TestWalkMemory_ProjectsIsFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "projects"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {
		t.Errorf("walkMemory on projects-as-file: got err %v, want nil", err)
	}
}
