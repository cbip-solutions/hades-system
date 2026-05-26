package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAll_PublicAPI(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	inv, err := ReadAll(tmp)
	if err != nil {
		t.Fatalf("ReadAll empty: %v", err)
	}
	if got := len(inv.Skills); got != 0 {
		t.Errorf("Skills: got %d, want 0", got)
	}
	if got := len(inv.Commands); got != 0 {
		t.Errorf("Commands: got %d, want 0", got)
	}
	if got := len(inv.Hooks); got != 0 {
		t.Errorf("Hooks: got %d, want 0", got)
	}
	if inv.Settings != nil {
		t.Errorf("Settings: got %v, want nil", inv.Settings)
	}
	if got := len(inv.MemoryFiles); got != 0 {
		t.Errorf("MemoryFiles: got %d, want 0", got)
	}
	if inv.MCPServers != nil {
		t.Errorf("MCPServers: got %v, want nil", inv.MCPServers)
	}
}

func TestReadAll_SourceMustExist(t *testing.T) {
	t.Parallel()
	_, err := ReadAll(filepath.Join(t.TempDir(), "does-not-exist"))
	if !errors.Is(err, ErrSourceMissing) {
		t.Errorf("err: got %v, want ErrSourceMissing", err)
	}
}

func TestReadAll_RefusesSymlinkOutsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("evil"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "skills", "evil"), 0o755); err != nil {
		t.Fatalf("mkdir skills/evil: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "skills", "evil", "SKILL.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := ReadAll(root)
	if !errors.Is(err, ErrSymlinkOutsideRoot) {
		t.Errorf("err: got %v, want ErrSymlinkOutsideRoot", err)
	}
}

func TestReadAll_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "abc"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "abc", "SKILL.md"), []byte("# abc skill"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	inv1, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll 1: %v", err)
	}
	inv2, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll 2: %v", err)
	}
	if len(inv1.Skills) != len(inv2.Skills) || inv1.Skills[0].Name != inv2.Skills[0].Name {
		t.Errorf("non-idempotent: %v vs %v", inv1.Skills, inv2.Skills)
	}
}
