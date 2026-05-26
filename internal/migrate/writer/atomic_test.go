package writer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile_BasicAndMode(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "file.txt")
	if err := atomicWriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode: got %o, want 0600", info.Mode().Perm())
	}
	body, _ := os.ReadFile(path)
	if string(body) != "hi" {
		t.Errorf("body: %s", body)
	}
}

func TestAtomicWriteFile_NoLeakOnSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "file.txt")
	if err := atomicWriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if e.Name() != "file.txt" {
			t.Errorf("leak: %s", e.Name())
		}
	}
}

func TestAtomicWriteFile_OverwriteExisting(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "new" {
		t.Errorf("body not overwritten: %s", body)
	}
}

func TestAtomicWriteFile_CreatesParentDirs(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "a", "b", "c", "file.txt")
	if err := atomicWriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatalf("create parents: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file missing: %v", err)
	}
}
