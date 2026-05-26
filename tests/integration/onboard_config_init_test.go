package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/cli"
)

func sandboxConfigHome(t *testing.T) (homeDir string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	prevHome := os.Getenv("HOME")
	prevXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir, func() {
		if prevHome == "" {
			os.Unsetenv("HOME")
		} else {
			os.Setenv("HOME", prevHome)
		}
		if prevXDG == "" {
			os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			os.Setenv("XDG_CONFIG_HOME", prevXDG)
		}
	}
}

func execConfigInit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := cli.NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func readTOMLField(t *testing.T, path, field string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readTOMLField: read %s: %v", path, err)
	}
	var m map[string]interface{}
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatalf("readTOMLField: parse %s: %v", path, err)
	}
	v, ok := m[field]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func TestConfigInitGreenfield(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	out, err := execConfigInit(t, "--yes", "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	configPath := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Fatalf("expected config.toml at %s but stat failed: %v", configPath, statErr)
	}

	sv := readTOMLField(t, configPath, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: config.toml schema_version=%q want %q", sv, "1.0")
	}

	if !strings.Contains(out, "Global config initialized") {
		t.Errorf("expected success message in output, got: %s", out)
	}
}

func TestConfigInitCCDetected(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	_, err := execConfigInit(t, "--non-interactive")
	if err == nil {
		t.Fatal("expected error when ~/.claude/ detected in non-interactive mode")
	}

	configPath := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	if _, statErr := os.Stat(configPath); statErr == nil {
		t.Error("config.toml should NOT be written when operator is in non-interactive cc-detected path")
	}
}

func TestConfigInitAnthropicBypass(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	out, err := execConfigInit(t, "--yes", "--provider=anthropic-bypass", "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	configPath := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	provider := readTOMLField(t, configPath, "llm_provider")
	if provider != "anthropic-bypass" {
		t.Errorf("config.toml llm_provider=%q want %q", provider, "anthropic-bypass")
	}

	sv := readTOMLField(t, configPath, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: schema_version=%q want 1.0", sv)
	}

	providersDir := filepath.Join(home, ".config", "zen-swarm", "providers")
	entries, _ := os.ReadDir(providersDir)
	if len(entries) > 0 {
		t.Errorf("expected no provider TOMLs for bypass; found %d files", len(entries))
	}
}

func TestConfigInitPaygoKeychain(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	out, err := execConfigInit(t, "--yes", "--provider=anthropic-paygo", "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	providerTOML := filepath.Join(home, ".config", "zen-swarm", "providers", "anthropic-paygo.toml")
	if _, statErr := os.Stat(providerTOML); statErr != nil {
		t.Fatalf("expected providers/anthropic-paygo.toml at %s: %v", providerTOML, statErr)
	}

	sv := readTOMLField(t, providerTOML, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: providers/anthropic-paygo.toml schema_version=%q want 1.0", sv)
	}
}

func TestConfigInitLocalOllama(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	out, err := execConfigInit(t, "--yes", "--provider=local-ollama", "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	providerTOML := filepath.Join(home, ".config", "zen-swarm", "providers", "local-ollama.toml")
	if _, statErr := os.Stat(providerTOML); statErr != nil {
		t.Fatalf("expected providers/local-ollama.toml: %v", statErr)
	}

	sv := readTOMLField(t, providerTOML, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: local-ollama.toml schema_version=%q want 1.0", sv)
	}
}

func TestConfigInitCustomProvider(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	customURL := "https://my-custom-llm.example.com/v1"
	out, err := execConfigInit(t, "--yes", "--provider=custom", "--custom-url="+customURL, "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	providerTOML := filepath.Join(home, ".config", "zen-swarm", "providers", "custom.toml")
	if _, statErr := os.Stat(providerTOML); statErr != nil {
		t.Fatalf("expected providers/custom.toml: %v", statErr)
	}

	sv := readTOMLField(t, providerTOML, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: custom.toml schema_version=%q want 1.0", sv)
	}

	gotURL := readTOMLField(t, providerTOML, "custom_provider_url")
	if gotURL != customURL {
		t.Errorf("custom_provider_url=%q want %q", gotURL, customURL)
	}
}

func TestConfigInitCustomProviderRejectsMissingURL(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	_, err := execConfigInit(t, "--yes", "--provider=custom", "--no-plugin")
	if err == nil {
		t.Fatal("expected error for --provider=custom --yes without --custom-url")
	}

	configPath := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	if _, statErr := os.Stat(configPath); statErr == nil {
		t.Error("config.toml should NOT exist after validation rejection")
	}
	providerTOML := filepath.Join(home, ".config", "zen-swarm", "providers", "custom.toml")
	if _, statErr := os.Stat(providerTOML); statErr == nil {
		t.Error("providers/custom.toml should NOT exist after validation rejection")
	}
}

func TestConfigInitSchemaVersionInvariant(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	out, err := execConfigInit(t, "--yes", "--no-plugin")
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	configDir := filepath.Join(home, ".config", "zen-swarm")
	err = filepath.Walk(configDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || filepath.Ext(path) != ".toml" {
			return nil
		}
		sv := readTOMLField(t, path, "schema_version")
		if sv != "1.0" {
			t.Errorf("inv-zen-188: TOML file %s missing or invalid schema_version (got %q)", path, sv)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk config dir: %v", err)
	}
}

func TestConfigInitAlreadyInitialized(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	configDir := filepath.Join(home, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	sentinel := `schema_version = "1.0"
llm_provider = "sentinel-value"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	out, err := execConfigInit(t, "--yes", "--no-plugin")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, out)
	}

	if !strings.Contains(out, "already initialized") {
		t.Errorf("expected 'already initialized' message; got: %s", out)
	}

	provider := readTOMLField(t, configPath, "llm_provider")
	if provider != "sentinel-value" {
		t.Errorf("config.toml was overwritten; llm_provider=%q want %q", provider, "sentinel-value")
	}
}

func TestConfigInitResetPreferences(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	configDir := filepath.Join(home, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	old := `schema_version = "1.0"
llm_provider = "old-value"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(old), 0o600); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	out, err := execConfigInit(t, "--yes", "--reset-preferences", "--provider=anthropic-bypass", "--no-plugin")
	if err != nil {
		t.Fatalf("config init --reset-preferences failed: %v\noutput: %s", err, out)
	}

	provider := readTOMLField(t, configPath, "llm_provider")
	if provider != "anthropic-bypass" {
		t.Errorf("expected llm_provider=%q after reset; got %q", "anthropic-bypass", provider)
	}
}

func TestConfigInitNonInteractiveModeCustomize(t *testing.T) {
	_, cleanup := sandboxConfigHome(t)
	defer cleanup()

	_, err := execConfigInit(t, "--non-interactive")
	if err == nil {
		t.Fatal("expected error for --non-interactive without --yes (ModeCustomize gate)")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("expected 'interactive' in error; got: %v", err)
	}
}

// TestConfigInitWithPluginInstall verifies that when --no-plugin is NOT
// passed, the plugin install step runs (user-scope path). A warning may appear
// in the output if the install dir creation has issues, but the command must
// still exit 0 (plugin install is non-fatal per runConfigInit §Step 11).
func TestConfigInitWithPluginInstall(t *testing.T) {
	home, cleanup := sandboxConfigHome(t)
	defer cleanup()

	repoRoot := t.TempDir()
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", repoRoot)

	out, err := execConfigInit(t, "--yes", "--scope=user")
	if err != nil {
		t.Fatalf("config init with plugin install failed: %v\noutput: %s", err, out)
	}

	configPath := filepath.Join(home, ".config", "zen-swarm", "config.toml")
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Fatalf("config.toml not written: %v", statErr)
	}

	sv := readTOMLField(t, configPath, "schema_version")
	if sv != "1.0" {
		t.Errorf("inv-zen-188: schema_version=%q want 1.0", sv)
	}
}

func TestConfigInitCmdExists(t *testing.T) {
	cmd := cli.NewConfigInitCmd()
	if cmd == nil {
		t.Fatal("NewConfigInitCmd() returned nil")
	}
	if cmd.Use != "init" {
		t.Errorf("expected Use=%q got %q", "init", cmd.Use)
	}
}
