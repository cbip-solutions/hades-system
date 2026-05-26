// SPDX-License-Identifier: MIT
// Package tierspertool ships the Plan 13 Phase F6 doctrine lint
// analyzer validating [capa_firewall.tiers] per-tool granularity per
// Q10=D + inv-zen-182.
//
// Validation rules:
//
//   - Each key in [capa_firewall.tiers] is "mcpName.toolName" or "mcpName"
//     (mcp-wide tier; resolves to all tools of that MCP).
//   - Each value is one of "high" | "medium" | "low".
//   - The mcpName MUST exist in the Phase A curated catalog (zero-length
//     catalog = skip the catalog check; tests can omit it).
//   - Empty key components rejected (e.g., ".toolName" or "mcp.").
//
// Analyzer is consumed by zen-doctrine-lint (existing Plan 8 lint stack
// loader) which loads TOML doctrine bundles + invokes ValidateDoctrineFile
// per file. Returns []Issue surfaced to operator output.
package tierspertool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	goanalysis "golang.org/x/tools/go/analysis"
)

var (
	filepathDir = filepath.Dir
	readFile    = os.ReadFile
)

func globTOMLs(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

type Severity int

const (
	SeverityError Severity = iota

	SeverityWarning
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "unknown"
	}
}

type Issue struct {
	Severity Severity
	Path     string
	Key      string
	Reason   string
}

type Validator struct {
	knownMCPs map[string]bool
}

func NewValidator(knownMCPs []string) *Validator {
	m := make(map[string]bool, len(knownMCPs))
	for _, name := range knownMCPs {
		m[name] = true
	}
	return &Validator{knownMCPs: m}
}

func (v *Validator) ValidateDoctrineFile(path string, body []byte) ([]Issue, error) {
	var raw struct {
		CapaFirewall struct {
			Tiers map[string]string `toml:"tiers"`
		} `toml:"capa_firewall"`
	}
	if err := toml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tierspertool: parse %s: %w", path, err)
	}

	validTiers := map[string]bool{"high": true, "medium": true, "low": true}
	var issues []Issue

	keys := sortedKeys(raw.CapaFirewall.Tiers)
	for _, key := range keys {
		value := raw.CapaFirewall.Tiers[key]

		if !validTiers[value] {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Key:      key,
				Reason:   fmt.Sprintf("invalid tier value %q; want one of high/medium/low", value),
			})
			continue
		}

		parts := strings.SplitN(key, ".", 2)
		mcpName := parts[0]
		if mcpName == "" {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Key:      key,
				Reason:   "empty MCP name in [capa_firewall.tiers] key",
			})
			continue
		}

		if len(v.knownMCPs) > 0 && !v.knownMCPs[mcpName] {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Key:      key,
				Reason:   fmt.Sprintf("unknown MCP name %q; not in curated catalog", mcpName),
			})
			continue
		}

		if len(parts) == 2 && parts[1] == "" {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Key:      key,
				Reason:   "empty tool name after '.' in [capa_firewall.tiers] key",
			})
		}
	}
	return issues, nil
}

var Analyzer = &goanalysis.Analyzer{
	Name: "tierspertool",
	Doc:  "validates [capa_firewall.tiers] per-tool granularity per inv-zen-182",
	Run:  runAnalyzer,
}

func runAnalyzer(pass *goanalysis.Pass) (any, error) {

	if len(pass.Files) == 0 {
		return nil, nil
	}
	pkgFile := pass.Fset.File(pass.Files[0].Pos())
	if pkgFile == nil {
		return nil, nil
	}
	pkgDir := filepathDir(pkgFile.Name())

	tomls, err := globTOMLs(pkgDir)
	if err != nil {
		return nil, nil
	}
	v := NewValidator(nil)
	for _, tomlPath := range tomls {
		body, err := readFile(tomlPath)
		if err != nil {
			continue
		}
		issues, err := v.ValidateDoctrineFile(tomlPath, body)
		if err != nil {

			continue
		}
		for _, iss := range issues {
			if iss.Severity != SeverityError {
				continue
			}
			pass.Report(goanalysis.Diagnostic{
				Pos:     pass.Files[0].Pos(),
				Message: fmt.Sprintf("inv-zen-182 [%s key %q]: %s", iss.Path, iss.Key, iss.Reason),
			})
		}
	}
	return nil, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
