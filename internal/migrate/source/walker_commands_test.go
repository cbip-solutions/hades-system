package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkCommands_Multi_Sorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gamma.md", "alpha.md", "beta.md"} {
		if err := os.WriteFile(filepath.Join(root, "commands", name), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := &Inventory{}
	if err := walkCommands(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.Commands); got != 3 {
		t.Errorf("got %d commands, want 3", got)
	}

	if inv.Commands[0].Name != "alpha" || inv.Commands[1].Name != "beta" || inv.Commands[2].Name != "gamma" {
		t.Errorf("not sorted: %v", []string{inv.Commands[0].Name, inv.Commands[1].Name, inv.Commands[2].Name})
	}
}

func TestWalkCommands_NoCommandsDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := walkCommands(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Commands) != 0 {
		t.Errorf("got %d commands, want 0", len(inv.Commands))
	}
}

func TestWalkCommands_NonMDFilesIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "alpha.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "beta.txt"), []byte("not md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "gamma.py"), []byte("# py"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkCommands(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.Commands); got != 1 {
		t.Errorf("got %d commands, want 1 (only alpha.md)", got)
	}
}
