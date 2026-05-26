package walkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGoModWalker_ParsesVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "go.mod")
	body := `module github.com/cbip-solutions/hades-system

go 1.25.6

require (
	github.com/BurntSushi/toml v1.6.0
)`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewGoModWalker(p, "0.9.0")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if r.Version != "0.9.0" {
		t.Errorf("Version: got %q, want 0.9.0", r.Version)
	}
}

func TestGoModWalker_EmptyVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "go.mod")
	body := "module github.com/cbip-solutions/hades-system\n\ngo 1.25.6\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewGoModWalker(p, "")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.Version != "" {
		t.Errorf("Version: got %q, want empty", r.Version)
	}

	if len(r.MissingSources) != 0 {
		t.Errorf("MissingSources: got %v, want empty", r.MissingSources)
	}
}

func TestGoModWalker_MissingFile_ReportsMissing(t *testing.T) {
	w := NewGoModWalker("/nonexistent/go.mod", "")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "go.mod") {
		t.Errorf("MissingSources: got %v", r.MissingSources)
	}
}
