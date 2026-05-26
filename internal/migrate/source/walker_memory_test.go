package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkMemory_Multi(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, slug := range []string{"proj-a", "proj-b"} {
		dir := filepath.Join(root, "projects", slug, "memory")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# "+slug), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "feedback_foo.md"), []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.MemoryFiles); got != 4 {
		t.Errorf("got %d memory files, want 4", got)
	}
}

func TestWalkMemory_NoProjectsDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.MemoryFiles) != 0 {
		t.Errorf("got %d memory files, want 0", len(inv.MemoryFiles))
	}
}

func TestWalkMemory_SkipsNonMemoryDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	dir := filepath.Join(root, "projects", "proj-a", "not-memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir2 := filepath.Join(root, "projects", "proj-a", "memory")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "file.md"), []byte("body2"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.MemoryFiles); got != 1 {
		t.Errorf("got %d memory files, want 1", got)
	}
}

func TestWalkMemory_NonMDIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "proj-a", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.txt"), []byte("not md"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkMemory(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.MemoryFiles); got != 1 {
		t.Errorf("got %d memory files, want 1 (only .md)", got)
	}
}

func TestWalkMemory_Sorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	for _, slug := range []string{"zeta-proj", "alpha-proj"} {
		dir := filepath.Join(root, "projects", slug, "memory")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"z.md", "a.md", "m.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("body"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	inv1 := &Inventory{}
	inv2 := &Inventory{}
	if err := walkMemory(root, inv1); err != nil {
		t.Fatal(err)
	}
	if err := walkMemory(root, inv2); err != nil {
		t.Fatal(err)
	}
	if len(inv1.MemoryFiles) != len(inv2.MemoryFiles) {
		t.Fatalf("non-deterministic cardinality: %d vs %d", len(inv1.MemoryFiles), len(inv2.MemoryFiles))
	}
	for i := range inv1.MemoryFiles {
		if inv1.MemoryFiles[i].Path != inv2.MemoryFiles[i].Path {
			t.Errorf("non-deterministic order at %d: %q vs %q", i, inv1.MemoryFiles[i].Path, inv2.MemoryFiles[i].Path)
		}
	}
}
