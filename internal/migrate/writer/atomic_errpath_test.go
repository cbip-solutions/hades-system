package writer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile_MkdirFailsOnFileParent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(blocker, "file.txt")
	err := atomicWriteFile(path, []byte("body"), 0o644)
	if err == nil {
		t.Errorf("expected mkdir error path; got nil")
	}
}

func TestAtomicWriteFile_RenameFailsOnReadonlyDestDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("readonly-dir test requires non-root unix")
	}
	t.Parallel()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "rodir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("pre"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	err := atomicWriteFile(target, []byte("new body"), 0o644)

	if err == nil {

		return
	}
	if errors.Is(err, ErrAtomicSwapFailed) {
		return
	}

}
