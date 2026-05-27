// SPDX-License-Identifier: MIT
package prefs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	ErrCorruptPrefs = errors.New("prefs: corrupt prefs file")

	ErrUnsupportedSchemaVersion = errors.New("prefs: unsupported schema_version")
)

const CurrentSchemaVersion = "1.0"

var currentMajor = mustParseMajor(CurrentSchemaVersion)

func mustParseMajor(v string) int {
	major, _, err := parseSchemaVersion(v)
	if err != nil {
		panic(fmt.Sprintf("prefs: CurrentSchemaVersion %q is malformed: %v", v, err))
	}
	return major
}

// Prefs is the on-disk shape of
// $XDG_CONFIG_HOME/zen-swarm/onboard-prefs.toml. Flat shape (C1+C3
// reconciliation 2026-05-14): mirrors the persistable subset of
// onboard.WizardAnswers — per-kind fields are zero when irrelevant.
//
// Per Q3=D Path 2 (Reuse): the wizard reads Prefs at start and uses
// each non-zero field as a default; the operator opts out via
// --reset-preferences (forces Path 3 Customize).
//
// SECURITY secret-bearing WizardAnswers fields (AnthropicAPIKey,
// CustomProviderAuth) are intentionally absent — they live in the OS
// keychain via internal/secret/. The conversion function
// onboard.PrefsFromAnswers (defined in the parent package, which owns
// WizardAnswers; cycle-break refactor 2026-05-16) performs the strip;
// TestPrefsFromAnswersStripsSecrets + TestPrefsStructHasNoSecretFields
// gate future refactors. See package godoc.
type Prefs struct {
	SchemaVersion string `toml:"schema_version"`

	LLMProvider        string `toml:"llm_provider,omitempty"`
	BypassConfigPath   string `toml:"bypass_config_path,omitempty"`
	OllamaEndpoint     string `toml:"ollama_endpoint,omitempty"`
	CustomProviderURL  string `toml:"custom_provider_url,omitempty"`
	AuditRetentionDays int    `toml:"audit_retention_days,omitempty"`
	GitConfigName      string `toml:"git_config_name,omitempty"`
	GitConfigEmail     string `toml:"git_config_email,omitempty"`
	InstallHermes      bool   `toml:"install_hermes,omitempty"`
	EnableAuditChain   bool   `toml:"enable_audit_chain,omitempty"`

	ProjectKind      string `toml:"project_kind,omitempty"`
	TemplateName     string `toml:"template_name,omitempty"`
	TemplateVersion  string `toml:"template_version,omitempty"`
	InitGit          bool   `toml:"init_git,omitempty"`
	LinkHermesPlugin bool   `toml:"link_hermes_plugin,omitempty"`
	PingDaemon       bool   `toml:"ping_daemon,omitempty"`

	Doctrine       string   `toml:"doctrine,omitempty"`
	DoctrineSource string   `toml:"doctrine_source,omitempty"`
	MCPs           []string `toml:"mcps,omitempty"`
}

func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "zen-swarm", "onboard-prefs.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {

		home = "."
	}
	return filepath.Join(home, ".config", "zen-swarm", "onboard-prefs.toml")
}

func Load(path string) (*Prefs, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Prefs{}, nil
		}
		return nil, fmt.Errorf("read prefs %q: %w", path, err)
	}

	var p Prefs
	if err := toml.Unmarshal(data, &p); err != nil {
		if backupErr := backupCorrupt(path, data); backupErr != nil {
			return nil, fmt.Errorf("%w: %v (backup failed: %v)", ErrCorruptPrefs, err, backupErr)
		}
		return nil, fmt.Errorf("%w: %v", ErrCorruptPrefs, err)
	}

	if err := validateSchemaVersion(p.SchemaVersion); err != nil {
		return nil, err
	}
	return &p, nil
}

func Save(path string, p *Prefs) error {
	if p == nil {
		return fmt.Errorf("prefs.Save: nil Prefs")
	}
	p.SchemaVersion = CurrentSchemaVersion

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		return fmt.Errorf("encode prefs: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir prefs dir %q: %w", dir, err)
	}

	// Atomic write per SOTA-2 #6 crash-only: WriteFile to <path>.tmp
	// (mode 0600 — operator-private; mirrors release + release doctrine
	// TOML pattern) then Rename. The rename is the atomicity
	// invariant; a crash mid-rename leaves the previous prefs file
	// intact, a crash mid-write leaves the.tmp staging file which
	// the next Save's O_TRUNC overwrites cleanly.
	//
	// Durability (fsync) is deliberately not invoked: onboarding
	// prefs are fully recoverable from defaults (Path 1 Recommended
	// re-derives every field) so the cost of an unsynced write is
	// at most one wizard re-run. The atomic-rename guarantee survives
	// crash; the unsynced bytes do not, but that's the same
	// trade-off `os.WriteFile` makes for the rest of the codebase.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp → %q: %w", path, err)
	}
	return nil
}

func validateSchemaVersion(v string) error {
	if v == "" {
		return nil
	}
	gotMajor, _, err := parseSchemaVersion(v)
	if err != nil {
		return fmt.Errorf("%w: invalid schema_version %q: %v", ErrCorruptPrefs, v, err)
	}
	if gotMajor != currentMajor {
		return fmt.Errorf("%w: file=%s code=%s", ErrUnsupportedSchemaVersion, v, CurrentSchemaVersion)
	}
	return nil
}

func parseSchemaVersion(v string) (major, minor int, err error) {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected MAJOR.MINOR; got %q", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("major %q: %w", parts[0], err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("minor %q: %w", parts[1], err)
	}
	return major, minor, nil
}

func backupCorrupt(path string, data []byte) error {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	backup := fmt.Sprintf("%s.corrupt-%s", path, stamp)
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return fmt.Errorf("backup write %q: %w", backup, err)
	}
	return nil
}
