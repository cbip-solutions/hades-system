package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

func TestBootstrapTesseraGeneratesWitnessOnFirstRun(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mgr, cleanup, err := bootstrapTessera(context.Background(), dataRoot)
	if err != nil {
		t.Fatalf("bootstrapTessera: %v", err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	if mgr == nil {
		t.Fatal("bootstrapTessera returned nil Manager")
	}
	pub, err := mgr.Witness().Load()
	if err != nil {
		t.Fatalf("witness.Load post-bootstrap: %v", err)
	}
	if pub == nil {
		t.Error("witness has no pubkey post-bootstrap")
	}
}

func TestManagerProjectAdapterLazyCreate(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	root := t.TempDir()
	dataRoot := filepath.Join(root, "share", "zen-swarm")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mgr, cleanup, err := bootstrapTessera(context.Background(), dataRoot)
	if err != nil {
		t.Fatalf("bootstrapTessera: %v", err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	a1, err := mgr.ProjectAdapter(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ProjectAdapter p1: %v", err)
	}
	a2, err := mgr.ProjectAdapter(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ProjectAdapter p1 (second call): %v", err)
	}
	if a1 != a2 {
		t.Error("ProjectAdapter returned different instances for same project_id (must be cached)")
	}
	a3, err := mgr.ProjectAdapter(context.Background(), "p2")
	if err != nil {
		t.Fatalf("ProjectAdapter p2: %v", err)
	}
	if a3 == a1 {
		t.Error("ProjectAdapter returned same instance for different project_ids")
	}
	_ = tessera.Leaf{}
}
