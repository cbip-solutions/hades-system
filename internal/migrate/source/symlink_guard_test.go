package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkGuard_InsideRootOK(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "target.md")
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := assertNoSymlinkEscape(root); err != nil {
		t.Errorf("inside-root symlink rejected: %v", err)
	}
}

func TestSymlinkGuard_OutsideRootRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "evil.md")
	if err := os.WriteFile(target, []byte("evil"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "evil.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := assertNoSymlinkEscape(root)
	if !errors.Is(err, ErrSymlinkOutsideRoot) {
		t.Errorf("err: got %v, want ErrSymlinkOutsideRoot", err)
	}
}

func TestSymlinkGuard_DanglingOK(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	link := filepath.Join(root, "dangling.md")
	if err := os.Symlink(filepath.Join(root, "missing.md"), link); err != nil {
		t.Fatal(err)
	}
	if err := assertNoSymlinkEscape(root); err != nil {
		t.Errorf("dangling symlink rejected: %v", err)
	}
}

func TestSymlinkGuard_RelativeSymlink_Inside_OK(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "subdir", "target.md")
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "rel-link.md")
	if err := os.Symlink("./subdir/target.md", link); err != nil {
		t.Fatal(err)
	}
	if err := assertNoSymlinkEscape(root); err != nil {
		t.Errorf("relative inside-root symlink rejected: %v", err)
	}
}
