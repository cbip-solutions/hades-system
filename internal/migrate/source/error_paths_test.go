package source

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// I-8 fix (feedback_testing_anti_patterns.md): the pre-fix file used
// `_ = err` with the comment "either OK". Tests passed whether the code
// behaved correctly or returned a spurious error — useless guard.
//
// Replacement strategy: each defensive read-dir-error test now asserts
// EXACTLY ONE of the two valid outcomes (error returned OR
// permission-skipped via warning), depending on the function's documented
// behavior. Platform-gated to non-root unix because the chmod-0 trick
// doesn't work for root + isn't reliable on Windows.

// TestWalkSkills_DirNotReadable asserts walkSkills returns a non-nil error
// when the skills/ directory itself is unreadable. The function does NOT
// downgrade dir-unreadable to a warning (that would be silent data loss);
// only individual SKILL.md read failures degrade gracefully.
func TestWalkSkills_DirNotReadable(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("dir-not-readable test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	inv := &Inventory{}
	err := walkSkills(root, inv)
	if err == nil {
		t.Errorf("walkSkills must surface dir-unreadable as a hard error (no silent data loss); got nil + Skills=%d", len(inv.Skills))
	}
}

func TestReadMDFilesFlat_DirNotReadable(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("dir-not-readable test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	_, err := readMDFilesFlat(root)
	if err == nil {
		t.Errorf("readMDFilesFlat must surface dir-unreadable as error; got nil")
	}
}

func TestWalkAnyExt_DirNotReadable(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("dir-not-readable test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	_, err := walkAnyExt(root, ".sh")
	if err == nil {
		t.Errorf("walkAnyExt must surface dir-unreadable as error; got nil")
	}
}

func TestWalkCommands_DirNotReadable(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("dir-not-readable test requires non-root unix")
	}
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	inv := &Inventory{}
	err := walkCommands(root, inv)
	if err == nil {
		t.Errorf("walkCommands must surface dir-unreadable as error; got nil")
	}
}
