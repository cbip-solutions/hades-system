package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadMCP_Wellformed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{"mcpServers":{"playwright":{"command":"npx","args":["@playwright/mcp"]}}}`)
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := readMCP(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.MCPServers == nil {
		t.Fatal("MCPServers nil")
	}
	if len(inv.MCPServers.MCPServers) != 1 {
		t.Errorf("MCPServers: %v", inv.MCPServers.MCPServers)
	}
	pw, ok := inv.MCPServers.MCPServers["playwright"]
	if !ok {
		t.Fatal("playwright server missing")
	}
	if pw.Command != "npx" {
		t.Errorf("command: %s", pw.Command)
	}
	if len(pw.Args) != 1 || pw.Args[0] != "@playwright/mcp" {
		t.Errorf("args: %v", pw.Args)
	}
}

func TestReadMCP_Malformed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	err := readMCP(root, inv)
	if !errors.Is(err, ErrMalformedMCP) {
		t.Errorf("err: got %v, want ErrMalformedMCP", err)
	}
}

func TestReadMCP_Missing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := readMCP(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.MCPServers != nil {
		t.Errorf("MCPServers: got %v, want nil", inv.MCPServers)
	}
}

func TestReadMCP_PathPopulated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{"mcpServers":{}}`)
	want := filepath.Join(root, ".mcp.json")
	if err := os.WriteFile(want, body, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := readMCP(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.MCPServers == nil || inv.MCPServers.Path != want {
		t.Errorf("Path: got %q, want %q", inv.MCPServers.Path, want)
	}
}
