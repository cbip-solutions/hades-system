package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkHooks_BashAndPython(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "tool.execute.before.sh"), []byte("#!/bin/bash\necho hi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "session.created.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkHooks(root, inv); err != nil {
		t.Fatal(err)
	}
	if got := len(inv.Hooks); got != 2 {
		t.Fatalf("got %d hooks, want 2", got)
	}
	langs := map[string]string{}
	for _, h := range inv.Hooks {
		langs[h.EventName] = h.Lang
	}
	if langs["tool.execute.before"] != "bash" {
		t.Errorf("bash mismatch: %v", langs)
	}
	if langs["session.created"] != "python" {
		t.Errorf("python mismatch: %v", langs)
	}
}

func TestWalkHooks_Sorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tool.execute.before.sh", "session.created.py", "permission.asked.sh"} {
		if err := os.WriteFile(filepath.Join(root, "hooks", name), []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := &Inventory{}
	if err := walkHooks(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Hooks) != 3 {
		t.Fatalf("got %d hooks, want 3", len(inv.Hooks))
	}

	got := []string{inv.Hooks[0].EventName, inv.Hooks[1].EventName, inv.Hooks[2].EventName}
	want := []string{"permission.asked", "session.created", "tool.execute.before"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hooks[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWalkHooks_NoHooksDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := walkHooks(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Hooks) != 0 {
		t.Errorf("got %d hooks, want 0", len(inv.Hooks))
	}
}

func TestWalkHooks_NonScriptIgnored(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "alpha.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := walkHooks(root, inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Hooks) != 1 {
		t.Errorf("got %d hooks, want 1 (only .sh/.py)", len(inv.Hooks))
	}
}
