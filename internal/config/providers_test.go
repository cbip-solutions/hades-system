package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func writeProvidersTOML(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "providers.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write providers.toml: %v", err)
	}
	return p
}

func TestLoadProvidersValid(t *testing.T) {
	dir := t.TempDir()
	path := writeProvidersTOML(t, dir, `
[[providers]]
name             = "deepseek-direct"
type             = "openai-compat"
endpoint         = "https://api.deepseek.com"
model            = "deepseek-chat"
family           = "deepseek"
api_key_keychain = "zen-swarm/deepseek"

[[providers]]
name     = "ollama-qwen-coder"
type     = "ollama"
endpoint = "http://localhost:11434"
model    = "qwen2.5-coder:32b"
family   = "qwen"
`)
	got, err := config.LoadProviders(path)
	if err != nil {
		t.Fatalf("LoadProviders: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "deepseek-direct" || got[1].Name != "ollama-qwen-coder" {
		t.Errorf("order/names wrong: %q, %q", got[0].Name, got[1].Name)
	}
	if got[0].Family != "deepseek" || got[0].APIKeyKeychain != "zen-swarm/deepseek" {
		t.Errorf("deepseek entry fields wrong: %+v", got[0])
	}
}

func TestLoadProvidersRejectsInvalidEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeProvidersTOML(t, dir, `
[[providers]]
name     = "broken"
type     = "openai-compat"
endpoint = "https://api.example.com"
model    = "x"
# family + api_key_keychain deliberately omitted
`)
	_, err := config.LoadProviders(path)
	if err == nil {
		t.Fatal("LoadProviders accepted an invalid [[providers]] entry")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name the offending entry", err.Error())
	}
}

func TestLoadProvidersMissingFile(t *testing.T) {
	_, err := config.LoadProviders(filepath.Join(t.TempDir(), "absent.toml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestLoadProvidersDuplicateName(t *testing.T) {
	dir := t.TempDir()
	path := writeProvidersTOML(t, dir, `
[[providers]]
name             = "dupe"
type             = "openai-compat"
endpoint         = "https://a.example.com"
model            = "m"
family           = "f"
api_key_keychain = "zen-swarm/a"

[[providers]]
name             = "dupe"
type             = "openai-compat"
endpoint         = "https://b.example.com"
model            = "m"
family           = "f"
api_key_keychain = "zen-swarm/b"
`)
	_, err := config.LoadProviders(path)
	if err == nil {
		t.Fatal("LoadProviders accepted duplicate provider names")
	}
	if !strings.Contains(err.Error(), "dupe") {
		t.Errorf("error %q does not name the duplicate", err.Error())
	}
}
