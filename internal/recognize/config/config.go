// SPDX-License-Identifier: MIT
// Package config implements Tier 2 of hades recognize per design contract=B:
// framework-config detection (Next/Vite/Nuxt/Astro/Remix/Svelte/Angular/Gatsby).
//
// Vite is special-cased: vite.config.{js,mjs,ts} alone is ambiguous — could
// be powering Vue, React, Svelte, or Astro. Disambiguation reads package.json
// dependencies + devDependencies for framework signals. per design contract:
// - Canonical config file + matching dep: confidence 1.0
// - Only config file (no matching dep): confidence 0.7
//
// Table-driven Vercel-style detection; one row per framework with predicate.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

const ManifestReadCap int64 = 50 * 1024

type FrameworkConfig struct {
	Framework      string
	ConfigPath     string
	Confidence     float64
	Disambiguation []string
}

type pkgJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func readPackageJSON(fsys fs.FS) (pkgJSON, error) {
	var p pkgJSON
	f, err := fsys.Open("package.json")
	if err != nil {
		return p, nil
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, ManifestReadCap))
	if err != nil {
		return p, fmt.Errorf("package.json: %w", err)
	}
	if err := json.Unmarshal(buf, &p); err != nil {
		return p, fmt.Errorf("package.json parse: %w", err)
	}
	return p, nil
}

func hasDep(p pkgJSON, name string) bool {
	if _, ok := p.Dependencies[name]; ok {
		return true
	}
	_, ok := p.DevDependencies[name]
	return ok
}

func hasDepPrefix(p pkgJSON, prefix string) bool {
	for k := range p.Dependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	for k := range p.DevDependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func findConfigFile(fsys fs.FS, candidates []string) string {
	for _, c := range candidates {
		if _, err := fs.Stat(fsys, c); err == nil {
			return c
		}
	}
	return ""
}

type frameworkRule struct {
	Framework string
	Configs   []string
	DepCheck  func(p pkgJSON) bool
}

var simpleRules = []frameworkRule{
	{
		Framework: "next.js",
		Configs:   []string{"next.config.js", "next.config.mjs", "next.config.ts"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "next") },
	},
	{
		Framework: "nuxt",
		Configs:   []string{"nuxt.config.js", "nuxt.config.ts"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "nuxt") || hasDep(p, "nuxt3") },
	},
	{
		Framework: "astro",
		Configs:   []string{"astro.config.js", "astro.config.mjs", "astro.config.ts"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "astro") },
	},
	{
		Framework: "remix",
		Configs:   []string{"remix.config.js", "remix.config.mjs"},
		DepCheck:  func(p pkgJSON) bool { return hasDepPrefix(p, "@remix-run/") },
	},
	{
		Framework: "sveltekit",
		Configs:   []string{"svelte.config.js", "svelte.config.mjs"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "@sveltejs/kit") },
	},
	{
		Framework: "angular",
		Configs:   []string{"angular.json"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "@angular/core") },
	},
	{
		Framework: "gatsby",
		Configs:   []string{"gatsby-config.js", "gatsby-config.ts"},
		DepCheck:  func(p pkgJSON) bool { return hasDep(p, "gatsby") },
	},
}

var viteConfigs = []string{"vite.config.js", "vite.config.mjs", "vite.config.ts"}

func Detect(fsys fs.FS) ([]FrameworkConfig, error) {
	pkg, _ := readPackageJSON(fsys)
	var out []FrameworkConfig

	for _, rule := range simpleRules {
		cfg := findConfigFile(fsys, rule.Configs)
		if cfg == "" {
			continue
		}
		hasMatchingDep := rule.DepCheck(pkg)
		confidence := 1.0
		rationale := []string{cfg + " present"}
		if hasMatchingDep {
			rationale = append(rationale, "matching dep in package.json")
		} else {
			confidence = 0.7
			rationale = append(rationale, "config present but no matching dep (low-confidence)")
		}
		out = append(out, FrameworkConfig{
			Framework:      rule.Framework,
			ConfigPath:     cfg,
			Confidence:     confidence,
			Disambiguation: rationale,
		})
	}

	viteCfg := findConfigFile(fsys, viteConfigs)
	if viteCfg != "" {
		out = append(out, disambiguateVite(viteCfg, pkg)...)
	}

	return out, nil
}

func disambiguateVite(cfgPath string, p pkgJSON) []FrameworkConfig {
	hasReact := hasDep(p, "react") && hasDep(p, "react-dom")
	hasVue := hasDep(p, "vue")
	hasSvelte := hasDep(p, "svelte")
	hasAstro := hasDep(p, "astro")

	matches := []FrameworkConfig{}
	base := []string{cfgPath + " present"}
	if hasReact {
		matches = append(matches, FrameworkConfig{
			Framework:      "vite-react",
			ConfigPath:     cfgPath,
			Confidence:     1.0,
			Disambiguation: append(append([]string{}, base...), "react + react-dom in deps"),
		})
	}
	if hasVue {
		matches = append(matches, FrameworkConfig{
			Framework:      "vite-vue",
			ConfigPath:     cfgPath,
			Confidence:     1.0,
			Disambiguation: append(append([]string{}, base...), "vue in deps"),
		})
	}
	if hasSvelte && !hasDep(p, "@sveltejs/kit") {

		matches = append(matches, FrameworkConfig{
			Framework:      "vite-svelte",
			ConfigPath:     cfgPath,
			Confidence:     1.0,
			Disambiguation: append(append([]string{}, base...), "svelte in deps (no @sveltejs/kit)"),
		})
	}
	if hasAstro {

		matches = append(matches, FrameworkConfig{
			Framework:      "vite-astro",
			ConfigPath:     cfgPath,
			Confidence:     1.0,
			Disambiguation: append(append([]string{}, base...), "astro in deps"),
		})
	}

	if len(matches) > 1 {
		for i := range matches {
			matches[i].Confidence = 0.7
			matches[i].Disambiguation = append(matches[i].Disambiguation, "ambiguous (multiple framework deps)")
		}
	}
	if len(matches) == 0 {

		matches = append(matches, FrameworkConfig{
			Framework:      "vite-vanilla",
			ConfigPath:     cfgPath,
			Confidence:     0.7,
			Disambiguation: []string{cfgPath + " present", "no framework dep matched"},
		})
	}
	return matches
}
