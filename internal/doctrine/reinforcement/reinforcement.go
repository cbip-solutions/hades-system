// SPDX-License-Identifier: MIT
// Package reinforcement implements the doctrine reinforcement template engine
// design spec §1 design choice C and §7.1 T5.
//
// The engine renders role-aware system-prompt blocks for LLM worker subprocesses,
// sourcing Go text/template content from a closed //go:embed embed/*.md.tmpl set
// (3 built-in doctrines: max-scope, default, capa-firewall) with operator-override
// fallthrough at <overrideDir>/<doctrineName>.system-prompt.md.tmpl.
//
// Safety contract (T5 mitigation):
// - text/template (NOT html/template) — fewer auto-escape sinks; intentional plain-text output.
// - No Sprig functions registered — only stdlib template functions (eq/ne/and/or/index/len/not/printf).
// - Var allowlist: only Vars struct fields are accessible to templates; any
// template referring to a field outside Vars returns ErrReinforcementTemplateExec.
// Enforcement is structural: text/template's reflect-based field lookup
// rejects unknown struct fields automatically with no opt-in needed.
// Option("missingkey=error") is set as defense-in-depth for any future
// Vars-via-map paths; Vars is currently a struct so reflect rejects
// unknown fields automatically.
// - Operator override file is loaded via os.ReadFile and parsed by the same
// text/template parser; identical safety guarantees apply.
// - doctrineName is validated up front (no path separators, no traversal)
// and the resolved override path is confined to overrideDir — see
// validateOverridePath for the CWE-22 defense-in-depth.
//
// Boundary discipline (invariant): imports stdlib + internal/doctrine/schema/v1
// + internal/doctrine/errors only. No internal/store, no internal/orchestrator,
// no internal/daemon, no internal/redact, no tier1-sidecar.
//
// Concurrency Engine.cache is sync.Map (lock-free read after first Parse).
// Render is goroutine-safe; concurrent calls on different doctrines load
// templates concurrently (each into its own cache slot). reload
// extends the package with InvalidateCache(name) for operator-override file
// changes — leaves the cache structurally compatible.
package reinforcement

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	derrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type Vars struct {
	DoctrineName       string
	ProjectAlias       string
	ProjectID          string
	CurrentStage       string
	CurrentPhase       string
	TaskKind           string
	TaskComplexityTier string
	PlanID             string
	TransverseAxioms   []string
}

type Engine struct {
	cache       sync.Map
	overrideDir string
}

func New(overrideDir string) *Engine {
	return &Engine{overrideDir: overrideDir}
}

func (e *Engine) Render(s *v1.Schema, vars *Vars) (string, error) {
	if vars == nil {
		return "", errors.New("reinforcement: vars must not be nil")
	}
	tmpl, err := e.loadTemplate(vars.DoctrineName)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, vars); err != nil {
		return "", fmt.Errorf("reinforcement: execute template %q: %w",
			vars.DoctrineName,
			errors.Join(err, derrors.ErrReinforcementTemplateExec),
		)
	}
	return sb.String(), nil
}

// loadTemplate resolves + parses + caches the template for doctrineName.
// Resolution order is operator-override-first then embedded, per design contract
//
// loadTemplate is exported via lowercase-package-internal name only; callers
// MUST go through Render. reload extends loadTemplate's caller surface
// via InvalidateCache(name); F-2 + F-3 fill in the actual lookup logic. F-1
// stub returns ErrTemplateNotFound for any name not pre-injected via
// InjectTemplateForTest (which the test fixtures use).
func (e *Engine) loadTemplate(doctrineName string) (*template.Template, error) {
	if cached, ok := e.cache.Load(doctrineName); ok {
		return cached.(*template.Template), nil
	}

	overridePath, err := validateOverridePath(e.overrideDir, doctrineName)
	if err != nil {
		return nil, err
	}

	if overridePath != "" {
		data, err := os.ReadFile(overridePath)
		switch {
		case err == nil:
			tmpl, perr := template.New(doctrineName).Option("missingkey=error").Parse(string(data))
			if perr != nil {
				return nil, fmt.Errorf("reinforcement: parse operator override %q: %w",
					overridePath,
					errors.Join(perr, derrors.ErrReinforcementTemplateExec),
				)
			}
			e.cache.Store(doctrineName, tmpl)
			return tmpl, nil
		case os.IsNotExist(err):

		default:
			return nil, fmt.Errorf("reinforcement: read operator override %q: %w",
				overridePath,
				errors.Join(err, derrors.ErrReinforcementTemplateExec),
			)
		}
	}

	if data, ok := embeddedTemplate(doctrineName); ok {
		tmpl, perr := template.New(doctrineName).Option("missingkey=error").Parse(data)
		if perr != nil {
			return nil, fmt.Errorf("reinforcement: parse embedded template %q: %w",
				doctrineName,
				errors.Join(perr, derrors.ErrReinforcementTemplateExec),
			)
		}
		e.cache.Store(doctrineName, tmpl)
		return tmpl, nil
	}
	return nil, fmt.Errorf("reinforcement: no template for doctrine %q (checked overrideDir=%q + embed): %w",
		doctrineName,
		e.overrideDir,
		derrors.ErrTemplateNotFound,
	)
}

func validateOverridePath(overrideDir, doctrineName string) (string, error) {
	if strings.ContainsAny(doctrineName, `/\`) || strings.Contains(doctrineName, "..") {
		return "", fmt.Errorf("reinforcement: invalid doctrine name %q (contains path separator or traversal): %w",
			doctrineName,
			derrors.ErrTemplateNotFound,
		)
	}
	if overrideDir == "" {
		return "", nil
	}
	overridePath := filepath.Join(overrideDir, doctrineName+".system-prompt.md.tmpl")
	cleanOverrideDir := filepath.Clean(overrideDir)
	cleanPath := filepath.Clean(overridePath)
	if !strings.HasPrefix(cleanPath, cleanOverrideDir+string(filepath.Separator)) {
		return "", fmt.Errorf("reinforcement: override path %q escapes overrideDir %q: %w",
			cleanPath,
			cleanOverrideDir,
			derrors.ErrTemplateNotFound,
		)
	}
	return overridePath, nil
}

func InjectTemplateForTest(e *Engine, name string, tmpl *template.Template) {
	e.cache.Store(name, tmpl)
}

func ValidateOverridePathForTest(overrideDir, doctrineName string) (string, error) {
	return validateOverridePath(overrideDir, doctrineName)
}

//go:embed embed/*.md.tmpl
var embeddedFS embed.FS

func embeddedTemplate(doctrineName string) (string, bool) {
	path := "embed/" + doctrineName + ".system-prompt.md.tmpl"
	data, err := embeddedFS.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}
