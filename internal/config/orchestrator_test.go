package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func TestLoadOrchestratorValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.toml")
	if err := os.WriteFile(path, []byte(`
[projects.alpha]
path = "/repos/alpha"

[projects.alpha.orchestrator]
default              = "worker-code"
fallback_chain       = ["deepseek-direct", "gemini-flash"]
allow_providers      = ["deepseek-direct", "gemini-flash", "bypass"]
auto_fallback_to_paygo = true

[projects.alpha.orchestrator.payg_safety]
per_session_cap_usd = 1.5
per_day_cap_usd     = 10.0
per_month_cap_usd   = 100.0
auto_pause_at_cap   = true

[projects.beta]
path = "/repos/beta"
`), 0o644); err != nil {
		t.Fatalf("write projects.toml: %v", err)
	}
	got, err := config.LoadOrchestrator(path, "alpha")
	if err != nil {
		t.Fatalf("LoadOrchestrator: %v", err)
	}
	if got.Default != "worker-code" {
		t.Errorf("Default = %q, want worker-code", got.Default)
	}
	if len(got.FallbackChain) != 2 || got.FallbackChain[0] != "deepseek-direct" {
		t.Errorf("FallbackChain wrong: %v", got.FallbackChain)
	}
	if !got.AutoFallbackToPAYGO {
		t.Error("AutoFallbackToPAYGO = false, want true")
	}
	if got.PAYGSafety.PerSessionCapUSD != 1.5 || got.PAYGSafety.PerMonthCapUSD != 100.0 {
		t.Errorf("PAYGSafety caps wrong: %+v", got.PAYGSafety)
	}
}

func TestLoadOrchestratorMissingProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.toml")
	if err := os.WriteFile(path, []byte("[projects.alpha]\npath = \"/x\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := config.LoadOrchestrator(path, "ghost")
	if err == nil {
		t.Fatal("LoadOrchestrator returned nil error for an unknown project")
	}
}

func TestLoadOrchestratorMissingFile(t *testing.T) {
	_, err := config.LoadOrchestrator(filepath.Join(t.TempDir(), "absent.toml"), "alpha")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}
