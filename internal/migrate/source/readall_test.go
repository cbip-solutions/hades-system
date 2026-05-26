package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAll_AllSurfacesIntegration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "skills", "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "research", "SKILL.md"), []byte("# research"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "hello.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "tool.execute.before.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"permissions":{"allow":["Read(*)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "projects", "proj-a", "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "proj-a", "memory", "MEMORY.md"), []byte("# mem"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(inv.Skills) != 1 {
		t.Errorf("skills: got %d, want 1", len(inv.Skills))
	}
	if len(inv.Commands) != 1 {
		t.Errorf("commands: got %d, want 1", len(inv.Commands))
	}
	if len(inv.Hooks) != 1 {
		t.Errorf("hooks: got %d, want 1", len(inv.Hooks))
	}
	if inv.Settings == nil {
		t.Errorf("Settings nil")
	}
	if len(inv.MemoryFiles) != 1 {
		t.Errorf("memory: got %d, want 1", len(inv.MemoryFiles))
	}
	if inv.MCPServers == nil {
		t.Errorf("MCPServers nil")
	}
}

func TestReadAll_PropagatesMalformedSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAll(root)
	if !errors.Is(err, ErrMalformedSettings) {
		t.Errorf("err: got %v, want ErrMalformedSettings", err)
	}
}

func TestReadAll_PropagatesMalformedMCP(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAll(root)
	if !errors.Is(err, ErrMalformedMCP) {
		t.Errorf("err: got %v, want ErrMalformedMCP", err)
	}
}

func TestReadAll_RelativePathResolved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	inv, err := ReadAll(".")
	if err != nil {
		t.Fatalf("ReadAll('.'): %v", err)
	}
	if inv == nil {
		t.Errorf("Inventory nil")
	}
}
