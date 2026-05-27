// SPDX-License-Identifier: MIT
package onboard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/onboard/prefs"
)

// GlobalConfig is the on-disk shape of $XDG_CONFIG_HOME/hades-system/config.toml,
// authored by WriteGlobalConfig at the end of `hades config init`. The shape
// mirrors the release doctrine TOML pattern (schema-versioned per
// ADR-0050). All non-mandatory fields are `omitempty` so unselected
// answers do not pollute the file.
//
// Per invariant (schema_version required), task C-2 always
// populates SchemaVersion = CurrentConfigSchemaVersion before calling
// WriteGlobalConfig.
type GlobalConfig struct {
	SchemaVersion string `toml:"schema_version"`

	LLMProvider string `toml:"llm_provider,omitempty"`

	Doctrine string `toml:"doctrine,omitempty"`

	HermesScope string `toml:"hermes_scope,omitempty"`
}

const CurrentConfigSchemaVersion = "1.0"

type ProviderConfig struct {
	SchemaVersion string `toml:"schema_version"`
	Name          string `toml:"name"`

	BypassConfigPath string `toml:"bypass_config_path,omitempty"`

	OllamaEndpoint string `toml:"ollama_endpoint,omitempty"`

	CustomProviderURL string `toml:"custom_provider_url,omitempty"`
}

func WriteGlobalConfig(cfg *GlobalConfig) error {
	if cfg == nil {
		return errors.New("WriteGlobalConfig: nil cfg")
	}
	return writeAtomicTOML(GlobalConfigPath(), cfg)
}

func WriteProviderTOML(name string, cfg *ProviderConfig) error {
	if name == "" {
		return errors.New("WriteProviderTOML: empty provider name")
	}
	if cfg == nil {
		return errors.New("WriteProviderTOML: nil cfg")
	}
	return writeAtomicTOML(filepath.Join(GlobalProvidersDir(), name+".toml"), cfg)
}

func CloneBuiltinDoctrine(name string) error {
	if name == "" {
		return errors.New("CloneBuiltinDoctrine: empty name")
	}
	dir := GlobalDoctrinesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("CloneBuiltinDoctrine mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, name+".toml")
	if _, err := os.Stat(dst); err == nil {

		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("CloneBuiltinDoctrine stat %s: %w", dst, err)
	}

	stub := []byte("schema_version = \"" + CurrentConfigSchemaVersion + "\"\nname = \"" + name + "\"\n")
	if err := os.WriteFile(dst, stub, 0o644); err != nil {
		return fmt.Errorf("CloneBuiltinDoctrine write %s: %w", dst, err)
	}
	return nil
}

// DefaultsFromFlagsAndPrefs merges CLI flags + persisted prefs + built-in
// defaults into a WizardDefaults consumable by Path 1 (recommended) or as
// starting values for Path 3 (customize). Precedence per spec §7.3:
//
// flags > prefs > builtin
//
// Empty flag values do NOT count as "operator-supplied"; absent or empty
// flags fall through to prefs. Nil prefs falls through to builtin. Slice
// fields from prefs are copied into the result so the caller may mutate
// without aliasing the prefs cache.
//
// task C-2 calls this after parsing `hades config init` flags and
// loading prefs via `prefs.Load(onboard.OnboardPrefsPath())`.
//
// Builtin defaults source: `GetDefaults(WizardKindGlobal)` (single
// source of truth in defaults.go — spec §7.3 + §2.7 Q7=D Tier 1+2 MCP
// set). cross-phase review F-2: prior code duplicated those
// defaults inline as `globalBuiltinDefaults()` while A-2 was unmerged;
// post-A-2 the inline copy is dead code and a drift risk.
func DefaultsFromFlagsAndPrefs(flags map[string]string, p *prefs.Prefs) WizardDefaults {
	d := GetDefaults(WizardKindGlobal)

	if p != nil {
		if p.LLMProvider != "" {
			d.LLMProvider = p.LLMProvider
		}
		if p.Doctrine != "" {
			d.Doctrine = p.Doctrine
		}
		if len(p.MCPs) > 0 {
			d.MCPSelections = append([]string(nil), p.MCPs...)
		}
		if p.TemplateName != "" {
			d.TemplateName = p.TemplateName
		}
		if p.InitGit {
			d.InitGit = p.InitGit
		}
	}

	if v, ok := flags["llm-provider"]; ok && v != "" {
		d.LLMProvider = v
	}
	if v, ok := flags["doctrine"]; ok && v != "" {
		d.Doctrine = v
	}
	if v, ok := flags["template"]; ok && v != "" {
		d.TemplateName = v
	}
	return d
}

// writeAtomicTOML is the shared atomic-write primitive used by
// WriteGlobalConfig and WriteProviderTOML. Mode 0600 (defense-in-depth).
//
// Crash-only contract (SOTA-2 #6 + spec §3.6):
//
// 1. Encode to `<path>.tmp` with O_TRUNC + O_CREATE.
// 2. fsync the temp file.
// 3. Close.
// 4. os.Rename to final path (atomic on POSIX + NTFS).
// 5. On any failure: remove the.tmp and return wrapped error.
//
// Per invariant: callers do not pass arbitrary writer fns — the
// signature is `(path, v)` so the call site cannot accidentally smuggle
// a non-TOML encoder in.
func writeAtomicTOML(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writeAtomicTOML mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writeAtomicTOML open %s: %w", tmp, err)
	}
	if err := toml.NewEncoder(f).Encode(v); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writeAtomicTOML encode %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writeAtomicTOML fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writeAtomicTOML close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writeAtomicTOML rename %s → %s: %w", tmp, path, err)
	}
	return nil
}
