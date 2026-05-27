// SPDX-License-Identifier: MIT
// internal/research/ecosystem/version_detector.go
//
// VersionDetector — 5-layer version-context detection cascade.
//
// Invariant invariant: VersionDetector.Detect is deterministic for the same
// (req.Query, req.Version, req.ProjectPath) triple — no random elements, no
// per-call state mutation. Layer 4 (Haiku classifier) is gated by
// VersionDetectorOptions.SkipLLMDetection; when enabled the cascade falls
// through to Layer 5 ("latest_stable") for queries where layers 1-3 return "".
//
// Layer priority (first-match-wins):
//
// Layer 1 — explicit req.Version != "" → return as-is (deterministic)
// Layer 2 — project-file parser (go.mod / pyproject.toml / package.json / Cargo.toml)
// Layer 3 — in-query regex /(go|python|node|react|rust)\s+\d+(\.\d+)+/i
// Layer 4 — Claude Haiku classifier dispatcher (confidence > 0.7)
// Layer 5 — default "latest_stable"
package ecosystem

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
)

type HaikuVersionClassifier interface {
	Classify(ctx context.Context, query string, ecosystem Ecosystem) (version string, confidence float64, err error)
}

const haikuConfidenceThreshold = 0.7

type VersionDetectorOptions struct {
	SkipLLMDetection bool
	HaikuClassifier  HaikuVersionClassifier
}

type VersionDetector struct {
	opts VersionDetectorOptions

	inQueryRe *regexp.Regexp
}

func NewVersionDetector(opts VersionDetectorOptions) *VersionDetector {
	return &VersionDetector{
		opts: opts,

		inQueryRe: regexp.MustCompile(`(?i)\b(go|python|node|react|rust)\s+(\d+(?:\.\d+)*)\b`),
	}
}

func (vd *VersionDetector) Detect(ctx context.Context, req QueryRequest) (version string, layer int, err error) {

	if req.Version != "" {
		return req.Version, 1, nil
	}

	if req.ProjectPath != "" {
		v, fileErr := vd.detectFromProjectFile(req.ProjectPath, req.Ecosystem)
		if fileErr != nil {

			return "", 0, fmt.Errorf("version_detector layer 2 project-file parse: %w", fileErr)
		}
		if v != "" {
			return v, 2, nil
		}
	}

	if v := vd.detectFromQueryRegex(req.Query); v != "" {
		return v, 3, nil
	}

	if !vd.opts.SkipLLMDetection && vd.opts.HaikuClassifier != nil {
		v, confidence, classifyErr := vd.opts.HaikuClassifier.Classify(ctx, req.Query, req.Ecosystem)
		if classifyErr != nil {
			// Absorbed Layer 4 is best-effort. The audit emitter in
			// captures these via the RAGAuditChainEmitter; we do not surface
			// the error to the caller — connectivity loss must not break the
			// query path (spec §2.5 Q5=A: "graceful degradation").
			_ = classifyErr
		} else if v != "" && confidence > haikuConfidenceThreshold {
			return v, 4, nil
		}
	}

	return "latest_stable", 5, nil
}

func (vd *VersionDetector) detectFromProjectFile(projectPath string, eco Ecosystem) (string, error) {
	switch eco {
	case EcoGo:
		return vd.parseGoMod(projectPath)
	case EcoPython:
		return vd.parsePythonProjectFile(projectPath)
	case EcoTypeScript:
		return vd.parsePackageJSON(projectPath)
	case EcoRust:
		return vd.parseCargoToml(projectPath)
	default:

		return "", nil
	}
}

func (vd *VersionDetector) parseGoMod(projectPath string) (string, error) {
	path := filepath.Join(projectPath, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	f, parseErr := modfile.Parse(path, data, nil)
	if parseErr != nil {
		// Malformed go.mod: treat as no-detect; do not surface parse error.
		// The cascade continues; the operator will see a Layer 5 result.
		return "", nil
	}
	if f.Go == nil || f.Go.Version == "" {
		return "", nil
	}
	return f.Go.Version, nil
}

func (vd *VersionDetector) parsePythonProjectFile(projectPath string) (string, error) {

	pyprojectPath := filepath.Join(projectPath, "pyproject.toml")
	if data, readErr := os.ReadFile(pyprojectPath); readErr == nil {
		if v := parsePyprojectRequiresPython(data); v != "" {
			return v, nil
		}
	}

	setupCfgPath := filepath.Join(projectPath, "setup.cfg")
	if data, readErr := os.ReadFile(setupCfgPath); readErr == nil {
		if v := parseSetupCfgPythonRequires(data); v != "" {
			return v, nil
		}
	}

	return "", nil
}

func parsePyprojectRequiresPython(data []byte) string {

	var cfg struct {
		Project struct {
			RequiresPython string `toml:"requires-python"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies map[string]interface{} `toml:"dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if _, decodeErr := toml.Decode(string(data), &cfg); decodeErr != nil {
		return ""
	}

	if v := extractVersionFromConstraint(cfg.Project.RequiresPython); v != "" {
		return v
	}

	if pyVal, ok := cfg.Tool.Poetry.Dependencies["python"]; ok {
		switch pv := pyVal.(type) {
		case string:
			if v := extractVersionFromConstraint(pv); v != "" {
				return v
			}
		}
	}

	return ""
}

func parseSetupCfgPythonRequires(data []byte) string {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "python_requires") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return extractVersionFromConstraint(strings.TrimSpace(parts[1]))
			}
		}
	}
	return ""
}

func extractVersionFromConstraint(s string) string {
	s = strings.TrimSpace(s)

	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)

	s = strings.TrimLeft(s, ">=<~^!=")

	if i := strings.IndexAny(s, ", \t"); i != -1 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)

	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return ""
	}
	return s
}

func (vd *VersionDetector) parsePackageJSON(projectPath string) (string, error) {
	path := filepath.Join(projectPath, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read package.json: %w", err)
	}

	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if jsonErr := json.Unmarshal(data, &pkg); jsonErr != nil {

		return "", nil
	}

	if pkg.Engines.Node != "" {
		if v := extractVersionFromConstraint(pkg.Engines.Node); v != "" {
			return v, nil
		}
	}

	for _, deps := range []map[string]string{pkg.DevDependencies, pkg.Dependencies} {
		if nodeV, ok := deps["@types/node"]; ok {
			if v := extractVersionFromConstraint(nodeV); v != "" {
				return v, nil
			}
		}
	}

	return "", nil
}

func (vd *VersionDetector) parseCargoToml(projectPath string) (string, error) {
	path := filepath.Join(projectPath, "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read Cargo.toml: %w", err)
	}

	var cargo struct {
		Package struct {
			RustVersion string `toml:"rust-version"`
			Edition     string `toml:"edition"`
		} `toml:"package"`
	}
	if _, decodeErr := toml.Decode(string(data), &cargo); decodeErr != nil {

		return "", nil
	}

	if cargo.Package.RustVersion != "" {
		return extractVersionFromConstraint(cargo.Package.RustVersion), nil
	}

	switch cargo.Package.Edition {
	case "2024":
		return "1.85", nil
	case "2021":
		return "1.56", nil
	case "2018":
		return "1.31", nil
	}

	return "", nil
}

func (vd *VersionDetector) detectFromQueryRegex(query string) string {
	m := vd.inQueryRe.FindStringSubmatch(query)
	if len(m) < 3 {
		return ""
	}

	return m[2]
}
