package onboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/prefs"
)

func TestPrefsFromAnswersMapsAllPersistableFields(t *testing.T) {
	a := WizardAnswers{
		Kind:               WizardKindGlobal,
		Mode:               ModeCustomize,
		LLMProvider:        "anthropic-paygo",
		BypassConfigPath:   "/tmp/bypass.json",
		OllamaEndpoint:     "http://localhost:11434",
		CustomProviderURL:  "https://example.com/v1",
		AuditRetentionDays: 30,
		GitConfigName:      "testuser",
		GitConfigEmail:     "testuser@example.com",
		InstallHermes:      true,
		EnableAuditChain:   true,
		ProjectKind:        "go-cli",
		TemplateName:       "embedded://go-cli",
		TemplateVersion:    "v1.0.0",
		InitGit:            true,
		LinkHermesPlugin:   true,
		PingDaemon:         true,
		Doctrine:           "max-scope",
		DoctrineSource:     "built-in",
		MCPSelections:      []string{"zen-swarm-ctld", "playwright"},
		SavePreferences:    true,
	}
	p := PrefsFromAnswers(a)
	if p == nil {
		t.Fatal("PrefsFromAnswers returned nil")
	}
	if p.LLMProvider != "anthropic-paygo" {
		t.Errorf("LLMProvider: %q", p.LLMProvider)
	}
	if p.BypassConfigPath != "/tmp/bypass.json" {
		t.Errorf("BypassConfigPath: %q", p.BypassConfigPath)
	}
	if p.OllamaEndpoint != "http://localhost:11434" {
		t.Errorf("OllamaEndpoint: %q", p.OllamaEndpoint)
	}
	if p.CustomProviderURL != "https://example.com/v1" {
		t.Errorf("CustomProviderURL: %q", p.CustomProviderURL)
	}
	if p.AuditRetentionDays != 30 {
		t.Errorf("AuditRetentionDays: %d", p.AuditRetentionDays)
	}
	if p.GitConfigName != "testuser" || p.GitConfigEmail != "testuser@example.com" {
		t.Errorf("git config: %q %q", p.GitConfigName, p.GitConfigEmail)
	}
	if !p.InstallHermes || !p.EnableAuditChain {
		t.Errorf("bool flags: %+v", p)
	}
	if p.ProjectKind != "go-cli" {
		t.Errorf("ProjectKind: %q", p.ProjectKind)
	}
	if p.TemplateName != "embedded://go-cli" || p.TemplateVersion != "v1.0.0" {
		t.Errorf("template: %q %q", p.TemplateName, p.TemplateVersion)
	}
	if !p.InitGit || !p.LinkHermesPlugin || !p.PingDaemon {
		t.Errorf("project bools: %+v", p)
	}
	if p.Doctrine != "max-scope" || p.DoctrineSource != "built-in" {
		t.Errorf("doctrine: %q %q", p.Doctrine, p.DoctrineSource)
	}
	if len(p.MCPs) != 2 || p.MCPs[0] != "zen-swarm-ctld" {
		t.Errorf("MCPs: %v", p.MCPs)
	}
}

// TestPrefsFromAnswersStripsSecrets — load-bearing security invariant:
// AnthropicAPIKey + CustomProviderAuth are SECRET (live in Keychain via
// internal/secret/); they MUST NEVER appear in the persisted prefs
// shape. This test fails loudly if a future refactor inadvertently
// adds a secret field to Prefs. Relocated from
// internal/onboard/prefs/prefs_test.go on 2026-05-16.
func TestPrefsFromAnswersStripsSecrets(t *testing.T) {
	a := WizardAnswers{
		AnthropicAPIKey:    "sk-SECRET-MUST-NOT-PERSIST",
		CustomProviderAuth: "Bearer SECRET-MUST-NOT-PERSIST",
	}
	p := PrefsFromAnswers(a)
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := prefs.Save(path, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "SECRET-MUST-NOT-PERSIST") {
		t.Fatalf("secret leaked into persisted prefs: %s", string(data))
	}
	if strings.Contains(string(data), "sk-") {
		t.Errorf("secret prefix leaked into persisted prefs: %s", string(data))
	}
	if strings.Contains(string(data), "Bearer") {
		t.Errorf("auth header leaked into persisted prefs: %s", string(data))
	}
}

func TestPrefsFromAnswersCopiesMCPSelections(t *testing.T) {
	src := []string{"zen-swarm-ctld", "playwright"}
	a := WizardAnswers{MCPSelections: src}
	p := PrefsFromAnswers(a)
	if len(p.MCPs) != 2 {
		t.Fatalf("MCPs: got %d entries, want 2", len(p.MCPs))
	}

	src[0] = "MUTATED"
	if p.MCPs[0] == "MUTATED" {
		t.Errorf("MCPs slice-aliased to source; mutation leaked: %v", p.MCPs)
	}
}
