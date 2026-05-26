package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func TestLoadRoutingValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routing.toml")
	if err := os.WriteFile(path, []byte(`
default = "worker-code"

[[rules]]
when    = "task.kind == 'review'"
profile = "worker-reasoning"

[[rules]]
when    = "task.kind == 'tactical'"
profile = "tactical"
`), 0o644); err != nil {
		t.Fatalf("write routing.toml: %v", err)
	}
	got, err := config.LoadRouting(path)
	if err != nil {
		t.Fatalf("LoadRouting: %v", err)
	}
	if got.Default != "worker-code" {
		t.Errorf("Default = %q, want worker-code", got.Default)
	}
	if len(got.Rules) != 2 || got.Rules[0].Profile != "worker-reasoning" {
		t.Errorf("Rules wrong: %v", got.Rules)
	}
}

func TestLoadRoutingMissingFile(t *testing.T) {
	_, err := config.LoadRouting(filepath.Join(t.TempDir(), "absent.toml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}
