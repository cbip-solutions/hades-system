package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func TestProvidersInitMaterializesFiles(t *testing.T) {
	dir := t.TempDir()
	cmd := newProvidersInitCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("providers init: %v", err)
	}
	for _, f := range []string{"providers.toml", "profiles.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("providers init did not create %s: %v", f, err)
		}
	}

	body, _ := os.ReadFile(filepath.Join(dir, "profiles.toml"))
	if !strings.Contains(string(body), "worker-code") {
		t.Errorf("profiles.toml missing worker-code role:\n%s", body)
	}
}

func TestProvidersInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "providers.toml")
	if err := os.WriteFile(existing, []byte("# operator edits\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersInitCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("providers init overwrote an existing providers.toml")
	}
	body, _ := os.ReadFile(existing)
	if !strings.Contains(string(body), "operator edits") {
		t.Error("providers init clobbered the existing file")
	}
}

func TestProvidersListReadsProvidersTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name             = "deepseek-direct"
type             = "openai-compat"
endpoint         = "https://api.deepseek.com"
model            = "deepseek-chat"
family           = "deepseek"
api_key_keychain = "zen-swarm/deepseek"
`), 0o644); err != nil {
		t.Fatalf("seed providers.toml: %v", err)
	}
	cmd := newProvidersListCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("providers list: %v", err)
	}
	got := out.String()
	for _, want := range []string{"deepseek-direct", "openai-compat", "deepseek"} {
		if !strings.Contains(got, want) {
			t.Errorf("providers list output missing %q:\n%s", want, got)
		}
	}
}

func TestProvidersListEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	cmd := newProvidersListCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("providers list (empty): %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "no providers") {
		t.Errorf("expected a 'no providers' message, got:\n%s", out.String())
	}
}

func TestProvidersAddAppendsEntry(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name     = "ollama-qwen-coder"
type     = "ollama"
endpoint = "http://localhost:11434"
model    = "qwen2.5-coder:32b"
family   = "qwen"
`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersAddCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Flags().Set("type", "openai-compat"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("endpoint", "https://api.deepseek.com"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("model", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("family", "deepseek"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("keychain", "zen-swarm/deepseek"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{"deepseek-direct"}); err != nil {
		t.Fatalf("providers add: %v", err)
	}

	declared, err := config.LoadProviders(filepath.Join(dir, "providers.toml"))
	if err != nil {
		t.Fatalf("providers.toml invalid after add: %v", err)
	}
	if len(declared) != 2 {
		t.Fatalf("len = %d after add, want 2", len(declared))
	}
}

func TestProvidersVerifyUnknownProvider(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name     = "ollama-qwen-coder"
type     = "ollama"
endpoint = "http://localhost:11434"
model    = "qwen2.5-coder:32b"
family   = "qwen"
`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersVerifyCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"ghost-provider"})
	if err == nil {
		t.Fatal("providers verify returned nil error for an unknown provider")
	}
	if !strings.Contains(err.Error(), "ghost-provider") {
		t.Errorf("error %q does not name the unknown provider", err.Error())
	}
}

func TestProvidersSetupGuidesOperator(t *testing.T) {
	dir := t.TempDir()
	cmd := newProvidersSetupCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("providers setup: %v", err)
	}
	got := out.String()
	for _, want := range []string{"init", "verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("providers setup output missing %q:\n%s", want, got)
		}
	}
}

func TestProvidersSetupWithExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte("# existing\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersSetupCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("providers setup: %v", err)
	}
	if !strings.Contains(out.String(), "providers.toml") {
		t.Errorf("providers setup did not report providers.toml presence:\n%s", out.String())
	}
}

func TestProvidersRotateRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name             = "deepseek-direct"
type             = "openai-compat"
endpoint         = "https://api.deepseek.com"
model            = "deepseek-chat"
family           = "deepseek"
api_key_keychain = "zen-swarm/deepseek"
`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersRotateCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	err := cmd.RunE(cmd, []string{"deepseek-direct"})
	if err == nil {
		t.Fatal("rotate with empty stdin should error")
	}
	if !strings.Contains(err.Error(), "empty key") {
		t.Errorf("error %q should mention 'empty key'", err.Error())
	}
}

func TestProvidersRotateMissingKeychainSlot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name     = "ollama-qwen-coder"
type     = "ollama"
endpoint = "http://localhost:11434"
model    = "qwen2.5-coder:32b"
family   = "qwen"
`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersRotateCmd(func() string { return dir })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("some-key\n"))
	err := cmd.RunE(cmd, []string{"ollama-qwen-coder"})
	if err == nil {
		t.Fatal("rotate on provider with no api_key_keychain should error")
	}
	if !strings.Contains(err.Error(), "no api_key_keychain") {
		t.Errorf("error %q should mention 'no api_key_keychain'", err.Error())
	}
}

func TestProvidersAddRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "providers.toml"), []byte(`
[[providers]]
name     = "ollama-qwen-coder"
type     = "ollama"
endpoint = "http://localhost:11434"
model    = "qwen2.5-coder:32b"
family   = "qwen"
`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newProvidersAddCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Flags().Set("type", "ollama"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("endpoint", "http://localhost:11434"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("model", "qwen2.5-coder:32b"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("family", "qwen"); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, []string{"ollama-qwen-coder"})
	if err == nil {
		t.Fatal("add duplicate name should error")
	}
	if !strings.Contains(err.Error(), "already declared") {
		t.Errorf("error %q should mention 'already declared'", err.Error())
	}
}

func TestProvidersAddCreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()

	cmd := newProvidersAddCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Flags().Set("type", "ollama"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("endpoint", "http://localhost:11434"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("model", "qwen2.5-coder:32b"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("family", "qwen"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{"ollama-local"}); err != nil {
		t.Fatalf("providers add (no prior file): %v", err)
	}
	declared, err := config.LoadProviders(filepath.Join(dir, "providers.toml"))
	if err != nil {
		t.Fatalf("providers.toml invalid after add: %v", err)
	}
	if len(declared) != 1 {
		t.Fatalf("len = %d after add to new file, want 1", len(declared))
	}
}

func TestProvidersAddRejectsInvalidFlags(t *testing.T) {
	dir := t.TempDir()
	cmd := newProvidersAddCmd(func() string { return dir })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Flags().Set("endpoint", "https://api.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("model", "some-model"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("family", "some-family"); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, []string{"bad-provider"})
	if err == nil {
		t.Fatal("add with missing --type should error from Validate()")
	}
}

func TestAppendProviderEntryPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.toml")
	seed := "# hand-crafted comment by operator\n[[providers]]\nname=\"existing\"\ntype=\"ollama\"\nendpoint=\"http://localhost:11434\"\nmodel=\"qwen\"\nfamily=\"qwen\"\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	entry := providers.ProviderConfig{
		Name:     "new-entry",
		Type:     "ollama",
		Endpoint: "http://localhost:11434",
		Model:    "llama3",
		Family:   "llama",
	}
	if err := appendProviderEntry(path, entry); err != nil {
		t.Fatalf("appendProviderEntry: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "hand-crafted comment by operator") {
		t.Error("appendProviderEntry truncated existing content")
	}
	if !strings.Contains(string(body), "new-entry") {
		t.Error("appendProviderEntry did not append new entry")
	}
}
