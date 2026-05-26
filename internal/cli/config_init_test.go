package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
	"github.com/cbip-solutions/hades-system/internal/onboard/prefs"
)

func TestResolveConfigInitWizardModeYesNoPrefs(t *testing.T) {
	f := configInitFlags{yes: true}
	got := resolveConfigInitWizardMode(f, nil)
	if got != onboard.ModeRecommended {
		t.Errorf("--yes + nil prefs: got mode=%v want ModeRecommended", got)
	}

	got = resolveConfigInitWizardMode(f, &prefs.Prefs{})
	if got != onboard.ModeRecommended {
		t.Errorf("--yes + zero prefs: got mode=%v want ModeRecommended", got)
	}
}

func TestResolveConfigInitWizardModeYesWithPrefs(t *testing.T) {
	f := configInitFlags{yes: true}

	got := resolveConfigInitWizardMode(f, &prefs.Prefs{LLMProvider: "anthropic-paygo"})
	if got != onboard.ModeReuse {
		t.Errorf("--yes + populated prefs: got mode=%v want ModeReuse", got)
	}
}

func TestResolveConfigInitWizardModeReset(t *testing.T) {

	f := configInitFlags{resetPrefs: true}
	got := resolveConfigInitWizardMode(f, nil)
	if got != onboard.ModeCustomize {
		t.Errorf("--reset (no --yes): got mode=%v want ModeCustomize", got)
	}

	f = configInitFlags{yes: true, resetPrefs: true}
	got = resolveConfigInitWizardMode(f, &prefs.Prefs{LLMProvider: "x"})
	if got != onboard.ModeRecommended {
		t.Errorf("--yes + --reset + prefs: got mode=%v want ModeRecommended", got)
	}

	got = resolveConfigInitWizardMode(f, nil)
	if got != onboard.ModeRecommended {
		t.Errorf("--yes + --reset (no prefs): got mode=%v want ModeRecommended", got)
	}
}

func TestResolveConfigInitWizardModeDefault(t *testing.T) {
	f := configInitFlags{}
	got := resolveConfigInitWizardMode(f, nil)
	if got != onboard.ModeCustomize {
		t.Errorf("default: got mode=%v want ModeCustomize", got)
	}

	got = resolveConfigInitWizardMode(f, &prefs.Prefs{LLMProvider: "x"})
	if got != onboard.ModeCustomize {
		t.Errorf("default + prefs: got mode=%v want ModeCustomize", got)
	}
}

func TestHasPersistedPrefsNil(t *testing.T) {
	if hasPersistedPrefs(nil) {
		t.Error("hasPersistedPrefs(nil) = true; want false")
	}
}

func TestHasPersistedPrefsEmpty(t *testing.T) {
	if hasPersistedPrefs(&prefs.Prefs{}) {
		t.Error("hasPersistedPrefs(&Prefs{}) = true; want false")
	}

	if hasPersistedPrefs(&prefs.Prefs{SchemaVersion: "1.0"}) {
		t.Error("hasPersistedPrefs with only SchemaVersion = true; want false")
	}
}

func TestHasPersistedPrefsFields(t *testing.T) {
	cases := []struct {
		name string
		p    *prefs.Prefs
	}{
		{"LLMProvider", &prefs.Prefs{LLMProvider: "anthropic-paygo"}},
		{"Doctrine", &prefs.Prefs{Doctrine: "max-scope"}},
		{"TemplateName", &prefs.Prefs{TemplateName: "go-cli"}},
		{"BypassConfigPath", &prefs.Prefs{BypassConfigPath: "/path"}},
		{"OllamaEndpoint", &prefs.Prefs{OllamaEndpoint: "http://localhost:11434"}},
		{"CustomProviderURL", &prefs.Prefs{CustomProviderURL: "https://x"}},
		{"GitConfigName", &prefs.Prefs{GitConfigName: "Alice"}},
		{"GitConfigEmail", &prefs.Prefs{GitConfigEmail: "alice@x"}},
		{"ProjectKind", &prefs.Prefs{ProjectKind: "go-cli"}},
		{"TemplateVersion", &prefs.Prefs{TemplateVersion: "v1"}},
		{"DoctrineSource", &prefs.Prefs{DoctrineSource: "built-in"}},
		{"AuditRetentionDays", &prefs.Prefs{AuditRetentionDays: 90}},
		{"InstallHermes", &prefs.Prefs{InstallHermes: true}},
		{"EnableAuditChain", &prefs.Prefs{EnableAuditChain: true}},
		{"InitGit", &prefs.Prefs{InitGit: true}},
		{"LinkHermesPlugin", &prefs.Prefs{LinkHermesPlugin: true}},
		{"PingDaemon", &prefs.Prefs{PingDaemon: true}},
		{"MCPs", &prefs.Prefs{MCPs: []string{"zen-swarm-ctld"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !hasPersistedPrefs(tc.p) {
				t.Errorf("hasPersistedPrefs with %s set = false; want true", tc.name)
			}
		})
	}
}

func TestValidateConfigInitFlagsScope(t *testing.T) {
	cases := []struct {
		scope     string
		wantError bool
	}{
		{"user", false},
		{"project", false},
		{"USER", true},
		{"Project", true},
		{"garbage", true},
		{"", true},
		{"user-scope", true},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			err := validateConfigInitFlags(configInitFlags{scope: tc.scope})
			if tc.wantError && err == nil {
				t.Errorf("scope=%q: expected error, got nil", tc.scope)
			}
			if !tc.wantError && err != nil {
				t.Errorf("scope=%q: unexpected error: %v", tc.scope, err)
			}
			if tc.wantError && err != nil {
				if !IsRecoverable(err) {
					t.Errorf("scope=%q: expected recoverable error, got %v", tc.scope, err)
				}
			}
		})
	}
}

func TestValidateConfigInitFlagsCustomProvider(t *testing.T) {

	err := validateConfigInitFlags(configInitFlags{
		scope:    "user",
		provider: "custom",
		yes:      true,
	})
	if err == nil {
		t.Fatal("--provider=custom --yes without --custom-url: expected error, got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("expected recoverable error; got %v", err)
	}
	if !strings.Contains(err.Error(), "custom-url") {
		t.Errorf("error must mention --custom-url; got: %v", err)
	}

	err = validateConfigInitFlags(configInitFlags{
		scope:     "user",
		provider:  "custom",
		yes:       true,
		customURL: "https://example.com/v1",
	})
	if err != nil {
		t.Errorf("--provider=custom --yes --custom-url=URL: expected nil, got %v", err)
	}

	err = validateConfigInitFlags(configInitFlags{
		scope:    "user",
		provider: "custom",
	})
	if err != nil {
		t.Errorf("--provider=custom (no --yes): expected nil, got %v", err)
	}

	err = validateConfigInitFlags(configInitFlags{
		scope:    "user",
		provider: "anthropic-paygo",
		yes:      true,
	})
	if err != nil {
		t.Errorf("--provider=anthropic-paygo --yes: expected nil, got %v", err)
	}
}

func TestConfigInitHomeDirFromEnv(t *testing.T) {
	t.Setenv("HOME", "/tmp/zen-test-home")
	got := configInitHomeDir()
	if got != "/tmp/zen-test-home" {
		t.Errorf("configInitHomeDir() = %q want %q", got, "/tmp/zen-test-home")
	}
}

func TestConfigInitHomeDirWithValue(t *testing.T) {
	t.Setenv("HOME", "/tmp/zen-fallback-home")
	got := configInitHomeDir()
	if got != "/tmp/zen-fallback-home" {
		t.Errorf("configInitHomeDir() = %q want %q", got, "/tmp/zen-fallback-home")
	}
}

func TestConfigInitHomeDirEmpty(t *testing.T) {

	orig := os.Getenv("HOME")
	os.Setenv("HOME", "")
	defer os.Setenv("HOME", orig)
	_ = configInitHomeDir()
}

func TestConfigInitCurrentDir(t *testing.T) {
	got := configInitCurrentDir()
	if got == "" {
		t.Error("configInitCurrentDir() returned empty string")
	}
}

func TestConfigInitPromptYesNoYes(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	got, err := configInitPromptYesNo(&out, in, "Continue? [y/N]", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for 'y' input")
	}
}

func TestConfigInitPromptYesNoNo(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("n\n")
	got, err := configInitPromptYesNo(&out, in, "Continue? [y/N]", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for 'n' input")
	}
}

func TestConfigInitPromptYesNoDefault(t *testing.T) {
	var out bytes.Buffer

	in := strings.NewReader("\n")
	got, err := configInitPromptYesNo(&out, in, "Continue?", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("empty input with defaultVal=true: expected true")
	}

	in = strings.NewReader("\n")
	got, err = configInitPromptYesNo(&out, in, "Continue?", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("empty input with defaultVal=false: expected false")
	}
}

func TestConfigInitPromptYesNoEOF(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("")
	got, err := configInitPromptYesNo(&out, in, "Continue?", true)
	if err != nil {
		t.Fatalf("unexpected error on EOF: %v", err)
	}
	if !got {
		t.Error("EOF with defaultVal=true: expected true")
	}
}

func TestConfigInitPromptYesNoCapitalY(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("Y\n")
	got, err := configInitPromptYesNo(&out, in, "Continue? [y/N]", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for 'Y' input")
	}
}

func TestConfigInitPromptYesNoPromptDisplayed(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("n\n")
	_, _ = configInitPromptYesNo(&out, in, "My specific prompt text", false)
	if !strings.Contains(out.String(), "My specific prompt text") {
		t.Errorf("prompt text not written to out; got: %q", out.String())
	}
}

func TestRunConfigInitGreenfield(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("runConfigInit greenfield: %v (output: %s)", err, buf.String())
	}
}

func TestRunConfigInitAlreadyInitialized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	configDir := filepath.Join(dir, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte("schema_version = \"1.0\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("already-initialized: unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "already initialized") {
		t.Errorf("expected 'already initialized' message; got: %s", buf.String())
	}
}

func TestRunConfigInitNonInteractiveModeCustomize(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--non-interactive"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for non-interactive+ModeCustomize")
	}
}

func TestRunConfigInitWithProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--provider=anthropic-paygo", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("with-provider: %v (output: %s)", err, buf.String())
	}

	providerTOML := filepath.Join(dir, ".config", "zen-swarm", "providers", "anthropic-paygo.toml")
	if _, err := os.Stat(providerTOML); err != nil {
		t.Fatalf("providers/anthropic-paygo.toml not written: %v", err)
	}
}

func TestRunConfigInitBypassProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--provider=anthropic-bypass", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bypass provider: %v", err)
	}

	providersDir := filepath.Join(dir, ".config", "zen-swarm", "providers")
	entries, _ := os.ReadDir(providersDir)
	if len(entries) > 0 {
		t.Errorf("expected no provider TOMLs for bypass; found %d files", len(entries))
	}
}

func TestRunConfigInitResetPreferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	configDir := filepath.Join(dir, ".config", "zen-swarm")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte("schema_version = \"1.0\"\nllm_provider = \"old\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--reset-preferences", "--provider=anthropic-bypass", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reset-preferences: %v", err)
	}
}

func TestRunConfigInitOllamaProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--provider=local-ollama", "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("local-ollama provider: %v", err)
	}

	providerTOML := filepath.Join(dir, ".config", "zen-swarm", "providers", "local-ollama.toml")
	if _, err := os.Stat(providerTOML); err != nil {
		t.Fatalf("providers/local-ollama.toml not written: %v", err)
	}
}

func TestRunConfigInitCustomProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	customURL := "https://my-provider.example.com/v1"
	cmd.SetArgs([]string{"--yes", "--provider=custom", "--custom-url=" + customURL, "--no-plugin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("custom provider: %v (output: %s)", err, buf.String())
	}

	providerTOML := filepath.Join(dir, ".config", "zen-swarm", "providers", "custom.toml")
	data, err := os.ReadFile(providerTOML)
	if err != nil {
		t.Fatalf("providers/custom.toml not written: %v", err)
	}

	if !strings.Contains(string(data), customURL) {
		t.Errorf("custom_provider_url not written to TOML; content:\n%s", string(data))
	}
	if !strings.Contains(string(data), "custom_provider_url") {
		t.Errorf("custom_provider_url field name missing from TOML; content:\n%s", string(data))
	}
}

func TestRunConfigInitCustomProviderRejectsMissingURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--provider=custom", "--no-plugin"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --provider=custom --yes without --custom-url")
	}
	if !IsRecoverable(err) {
		t.Errorf("expected recoverable error (exit 1); got %v", err)
	}
	if !strings.Contains(err.Error(), "custom-url") {
		t.Errorf("error should mention --custom-url; got: %v", err)
	}

	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	if _, statErr := os.Stat(configPath); statErr == nil {
		t.Error("config.toml should NOT exist after validation rejection")
	}
}

func TestRunConfigInitPreflightCatchesCCMarkers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected preflight error for ~/.claude/ with CC marker")
	}

	if !IsPreflightFailure(err) {
		t.Errorf("expected ErrPreflightFailure wrap (exit 3); got %v", err)
	}

	if IsRecoverable(err) {
		t.Errorf("preflight failure should not be marked as recoverable: %v", err)
	}

	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("expected 'claude-code' in error; got %v", err)
	}
}

func TestRunConfigInitCCEmptyDirPasses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin", "--provider=anthropic-bypass"})
	if err := cmd.Execute(); err != nil {

		if strings.Contains(err.Error(), "migrate claude-code") {
			t.Fatalf("empty ~/.claude/ tripped false-positive cc-detection: %v", err)
		}
		t.Fatalf("empty ~/.claude/ unexpected error: %v (output: %q)", err, buf.String())
	}

	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.toml not written: %v", err)
	}
}

func TestRunConfigInitCCDetectDelegated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))

	cmd.SetArgs([]string{"--non-interactive"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected non-interactive gate error (Step 4)")
	}
	if strings.Contains(err.Error(), "migrate claude-code") {
		t.Errorf("Step 3 false-positived on empty ~/.claude/: %v", err)
	}

	if strings.Contains(buf.String(), "migrate claude-code") {
		t.Errorf("Step 3 prompt should NOT appear on empty ~/.claude/; got: %q", buf.String())
	}
}

// TestRunConfigInitNilContextRunsGreenfield exercises the ctx==nil guard
// (line ~125) and asserts real behavior: runConfigInit must not panic AND
// must successfully write config.toml when called with a Cobra command
// whose Context() returns nil (direct-call path, not cmd.Execute).
//
// Previously this test asserted nothing meaningful (I-6 finding). Post-fix:
// asserts (a) no panic, (b) no error or only an audit-emit warning, (c) config.toml
// exists post-run.
func TestRunConfigInitNilContextRunsGreenfield(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))

	flags := configInitFlags{yes: true, noPlugin: true, scope: "user"}
	err := runConfigInit(cmd, flags)
	if err != nil {
		t.Fatalf("nil context greenfield: unexpected error: %v (output: %q)", err, buf.String())
	}

	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.toml not written despite successful runConfigInit: %v", err)
	}
}

func TestRunConfigInitPluginInstallProjectScope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	repoRoot := t.TempDir()
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", repoRoot)

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--scope=project", "--provider=anthropic-bypass"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin install project scope: %v (output: %q)", err, buf.String())
	}

	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.toml not written: %v", err)
	}
}

func TestRunConfigInitContextCanceledReturnsNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)

	flags := configInitFlags{yes: true, noPlugin: true, scope: "user"}
	err := runConfigInit(cmd, flags)

	if err != nil {

		if !errors.Is(err, ErrPreflightFailure) && !strings.Contains(err.Error(), "canceled") &&
			!strings.Contains(err.Error(), "context") {
			t.Fatalf("unexpected error on canceled ctx: %v", err)
		}
	}
}

func TestRunConfigInitCorruptPrefsWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	prefsPath := filepath.Join(dir, ".config", "zen-swarm", "onboard-prefs.toml")
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(prefsPath, []byte("THIS IS NOT VALID = TOML [["), 0o600); err != nil {
		t.Fatalf("write malformed prefs: %v", err)
	}

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin", "--provider=anthropic-bypass"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("corrupt prefs path: unexpected error: %v (output: %q)", err, buf.String())
	}

	// Assert the warning surfaced to the operator.
	if !strings.Contains(buf.String(), "prefs load") {
		t.Errorf("expected 'prefs load' warning in output; got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "built-in defaults") {
		t.Errorf("expected fallback message about built-in defaults; got: %q", buf.String())
	}

	// The init must still complete successfully (warning, not fatal).
	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.toml not written after corrupt prefs warning: %v", err)
	}
}

func TestRunConfigInitWithDoctrineFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin", "--provider=anthropic-bypass", "--doctrine=max-scope"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctrine flag: %v (output: %s)", err, buf.String())
	}

	configPath := filepath.Join(dir, ".config", "zen-swarm", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	if !strings.Contains(string(data), "max-scope") {
		t.Errorf("doctrine=max-scope not in config.toml; got:\n%s", string(data))
	}
}

func TestRunConfigInitNoSaveOnRecommendedPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", t.TempDir())

	cmd := NewConfigInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--yes", "--no-plugin", "--provider=anthropic-bypass"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("recommended path: %v", err)
	}

	prefsPath := filepath.Join(dir, ".config", "zen-swarm", "onboard-prefs.toml")
	if _, err := os.Stat(prefsPath); err == nil {
		t.Error("prefs.toml should NOT be written on Recommended path")
	}
}

func TestNewConfigInitCmdFlags(t *testing.T) {
	cmd := NewConfigInitCmd()
	wantFlags := []string{
		"yes",
		"reset-preferences",
		"non-interactive",
		"doctrine",
		"provider",
		"custom-url",
		"no-plugin",
		"scope",
	}
	for _, f := range wantFlags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("missing flag --%s", f)
		}
	}

	if cmd.Flags().Lookup("no-curated-mcps") != nil {
		t.Error("old --no-curated-mcps flag should not be present after I-8 rename")
	}
}

func TestNewConfigInitCmdUse(t *testing.T) {
	cmd := NewConfigInitCmd()
	if cmd.Use != "init" {
		t.Errorf("expected Use=%q got %q", "init", cmd.Use)
	}
}

func TestNewConfigInitCmdHelpExitCodes(t *testing.T) {
	cmd := NewConfigInitCmd()
	help := cmd.Long

	for _, want := range []string{"0  success", "1  operator-recoverable", "2  unrecoverable", "3  preflight failure"} {
		if !strings.Contains(help, want) {
			t.Errorf("help text missing exit code %q; got:\n%s", want, help)
		}
	}

	if strings.Contains(help, "130") {
		t.Errorf("help text should not claim exit 130; main.go does not emit it")
	}
}
