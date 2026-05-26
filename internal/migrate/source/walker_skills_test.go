package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkSkills_Empty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 0 {
		t.Errorf("got %d skills, want 0", len(inv.Skills))
	}
}

func TestWalkSkills_Multi_Sorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"gamma", "alpha", "beta"} {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.Skills); got != 3 {
		t.Fatalf("got %d skills, want 3", got)
	}

	if inv.Skills[0].Name != "alpha" || inv.Skills[1].Name != "beta" || inv.Skills[2].Name != "gamma" {
		t.Errorf("not sorted: %v", []string{inv.Skills[0].Name, inv.Skills[1].Name, inv.Skills[2].Name})
	}
}

func TestWalkSkills_MissingSKILLmd_Warns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Warnings) == 0 {
		t.Errorf("expected warning, got none")
	}
	if len(inv.Skills) != 0 {
		t.Errorf("got %d skills, want 0", len(inv.Skills))
	}
}

func TestWalkSkills_NotADirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "skills"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 0 {
		t.Errorf("got %d skills, want 0", len(inv.Skills))
	}
}

func TestWalkSkills_BodyPreservedVerbatim(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("# alpha\n\nLine 1.\nLine 2 with special chars: !@#$%^&*()_+={}[]|\\:;\"'<>,.?/~`\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkSkills(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(inv.Skills))
	}
	if string(inv.Skills[0].Body) != string(want) {
		t.Errorf("body not verbatim: got %q want %q", inv.Skills[0].Body, want)
	}
}
