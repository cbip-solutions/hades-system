package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func writeProvidersInitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBuildProviderRegistryConstructorsRegistered(t *testing.T) {
	dir := t.TempDir()
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry: %v", err)
	}
	defer reg.Close()

	if got := reg.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty (no providers.toml)", got)
	}
}

func TestBuildProviderRegistryLoadsProviders(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_DEEPSEEK", "sk-test-key")
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name             = "deepseek-direct"
type             = "openai-compat"
endpoint         = "https://api.deepseek.com"
model            = "deepseek-chat"
family           = "deepseek"
api_key_keychain = "zen-swarm/deepseek"
`)
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry: %v", err)
	}
	defer reg.Close()
	got := reg.List()
	if len(got) != 1 || got[0] != "deepseek-direct" {
		t.Errorf("List() = %v, want [deepseek-direct]", got)
	}
}

func TestBuildProviderRegistrySkipsMissingKeychainKey(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	// Deliberately do NOT set ZEN_KEYCHAIN_ZEN_SWARM_DEEPSEEK.
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name             = "deepseek-direct"
type             = "openai-compat"
endpoint         = "https://api.deepseek.com"
model            = "deepseek-chat"
family           = "deepseek"
api_key_keychain = "zen-swarm/deepseek"
`)
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry must not fail on a missing key: %v", err)
	}
	defer reg.Close()

	b, err := reg.Get("deepseek-direct")
	if err != nil {
		t.Fatalf("missing-key provider not registered: %v", err)
	}

	if _, ferr := b.Forward(t.Context(), providersTierRequestStub()); ferr == nil {
		t.Error("disabled backend Forward returned nil error, want ErrTierUnavailable")
	}
}

func TestBuildProviderRegistryRejectsBadProvidersTOML(t *testing.T) {
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name = "broken"
type = "not-a-real-type"
`)
	_, err := BuildProviderRegistry(dir)
	if err == nil {
		t.Fatal("BuildProviderRegistry accepted a malformed providers.toml")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name the offending entry", err.Error())
	}
}

// TestBuildProviderRegistry_BypassInCascadeDoesNotFail captures the
// 2026-05-20 first-system-test hot-fix: BuildProviderRegistry MUST NOT
// run cascade-completeness against the "bypass" backend, because
// "bypass" is registered by buildOrchestrator (master C5) AFTER
// BuildProviderRegistry returns. Running the check inside
// BuildProviderRegistry crashes the daemon on any operator running the
// default profiles.toml (orchestrator cascade = ["bypass", "gemini-pro"])
// when bypass-config.json is absent (graceful-degradation path documented
// in main.go:239-246).
//
// The authoritative invariant gate lives in verifyCascadeCompleteness
// (orchestrator_wiring.go) which runs AFTER buildOrchestrator with the
// full registry. Asserting BuildProviderRegistry succeeds here keeps the
// two layers' responsibilities aligned with the C5 split.
func TestBuildProviderRegistry_BypassInCascadeDoesNotFail(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name             = "gemini-pro"
type             = "gemini"
endpoint         = "https://generativelanguage.googleapis.com"
model            = "gemini-2.0-pro"
family           = "gemini"
api_key_keychain = "zen-swarm/google-ai"
`)

	writeProvidersInitFile(t, filepath.Join(dir, "profiles.toml"), `
[profiles.orchestrator]
cascade = ["bypass", "gemini-pro"]
`)
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry must succeed when a profile cascade names "+
			"\"bypass\" — buildOrchestrator owns that backend per master C5: %v", err)
	}
	defer reg.Close()

	if _, gerr := reg.Get("gemini-pro"); gerr != nil {
		t.Errorf("gemini-pro not registered: %v", gerr)
	}
	// bypass MUST NOT be registered by BuildProviderRegistry — master C5
	// reserves that to buildOrchestrator.
	if _, gerr := reg.Get("bypass"); gerr == nil {
		t.Error("bypass MUST NOT be registered by BuildProviderRegistry (master C5: buildOrchestrator owns it)")
	}
}

func TestDisabledBackendProbe(t *testing.T) {
	d := &disabledBackend{
		name:   "test-disabled",
		tier:   providers.TierGenericOpenAICompat,
		reason: "key not found",
	}
	err := d.Probe(t.Context())
	if err == nil {
		t.Fatal("Probe returned nil, want error")
	}
	if !errors.Is(err, providers.ErrTierUnavailable) {
		t.Errorf("Probe error %q does not wrap ErrTierUnavailable", err)
	}
}

func TestDisabledBackendTierAndCapabilities(t *testing.T) {
	d := &disabledBackend{
		name:   "test-disabled",
		tier:   providers.TierGemini,
		reason: "key not found",
	}
	if got := d.Tier(); got != providers.TierGemini {
		t.Errorf("Tier() = %v, want TierGemini", got)
	}
	caps := d.Capabilities()

	if caps != (providers.TierCapabilities{}) {
		t.Errorf("Capabilities() = %+v, want zero value", caps)
	}
}

func TestTierForProviderType(t *testing.T) {
	cases := []struct {
		input string
		want  providers.Tier
	}{
		{"anthropic-paygo", providers.TierAnthropicPAYG},
		{"gemini", providers.TierGemini},
		{"ollama", providers.TierOllama},
		{"openai-compat", providers.TierGenericOpenAICompat},
		{"unknown-type", providers.TierPause},
		{"", providers.TierPause},
	}
	for _, tc := range cases {
		got := tierForProviderType(tc.input)
		if got != tc.want {
			t.Errorf("tierForProviderType(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestLogRegistrySummary(t *testing.T) {
	reg := providers.NewRegistry()
	defer reg.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))

	logRegistrySummary(logger, reg)
}

// TestBuildProviderRegistryAnthropicPaygoConstructor exercises the
// anthropic-paygo constructor closure path in registerConstructors.
// ZEN_KEYCHAIN_DISABLE + env var supply the key deterministically.
//
// NOTE(plan-15): AnthropicPaygoBackend.Name() is hardcoded to "anthropic-paygo"
// (not cfg.Name), so the providers.toml name MUST also be "anthropic-paygo"
// for registry.Register's name == backend.Name() check to pass.
func TestBuildProviderRegistryAnthropicPaygoConstructor(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_ANTHROPIC", "sk-ant-test-key")
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name             = "anthropic-paygo"
type             = "anthropic-paygo"
endpoint         = "https://api.anthropic.com"
model            = "claude-3-5-haiku-20241022"
family           = "claude"
api_key_keychain = "zen-swarm/anthropic"
`)
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry: %v", err)
	}
	defer reg.Close()
	got := reg.List()
	if len(got) != 1 || got[0] != "anthropic-paygo" {
		t.Errorf("List() = %v, want [anthropic-paygo]", got)
	}
}

func TestBuildProviderRegistryGeminiConstructor(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_GEMINI", "AIza-test-key")
	dir := t.TempDir()
	writeProvidersInitFile(t, filepath.Join(dir, "providers.toml"), `
[[providers]]
name             = "gemini-direct"
type             = "gemini"
endpoint         = "https://generativelanguage.googleapis.com"
model            = "gemini-2.0-flash"
family           = "gemini"
api_key_keychain = "zen-swarm/gemini"
`)
	reg, err := BuildProviderRegistry(dir)
	if err != nil {
		t.Fatalf("BuildProviderRegistry: %v", err)
	}
	defer reg.Close()
	got := reg.List()
	if len(got) != 1 || got[0] != "gemini-direct" {
		t.Errorf("List() = %v, want [gemini-direct]", got)
	}
}

func TestProvidersInit_OllamaConstructorRegistered(t *testing.T) {
	reg := providers.NewRegistry()
	defer reg.Close()
	if err := registerConstructors(reg, keychain.SystemResolver{}); err != nil {
		t.Fatalf("registerConstructors: %v", err)
	}
	err := reg.RegisterFromConfig(providers.ProviderConfig{
		Name:     "ollama-qwen-coder",
		Type:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "qwen2.5-coder:32b",
		Family:   "local-qwen",
	})
	if err != nil {
		t.Fatalf("RegisterFromConfig(ollama): %v", err)
	}
	b, err := reg.Get("ollama-qwen-coder")
	if err != nil {
		t.Fatalf("Get(ollama-qwen-coder): %v", err)
	}
	if b.Tier() != providers.TierOllama {
		t.Errorf("backend Tier() = %v, want TierOllama", b.Tier())
	}
}
