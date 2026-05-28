// SPDX-License-Identifier: MIT
// internal/config/resolver.go
//
// ProfileResolver materializes invariant: it merges the four config
// layers of HADES design's routing model into a single ordered provider
// cascade, deterministically.
//
// built-in defaults → profiles.toml → projects.toml[orchestrator] →.hades-system.toml
//
// Merge is per-field replacement: a later layer that sets a cascade
// wholly replaces the earlier one (it does not append or splice).
// invariant is the guarantee that the merge result depends only on
// the layer *contents*, never on map-iteration order — a dedicated test
// (resolver_test.go) asserts determinism across layer permutations.
//
// The built-in defaults encode the operator's v1.0 roster in Go so the
// daemon resolves every core profile out-of-box, before the operator
// writes profiles.toml. providers.toml + the Keychain keys are still
// required for the cascade to actually forward — the defaults only
// supply the role→provider-name mapping.
//
// adds the ProfileResolver type + its Resolve method.
package config

import (
	"fmt"
	"sort"
)

func BuiltinProfileDefaults() map[string]ProfileConfig {
	raw := []ProfileConfig{
		{
			Name:        "orchestrator",
			Description: "Roster S/S' — top-level planner: sidecar Tier 1 then Gemini Pro",
			Cascade:     []string{"bypass-sidecar", "gemini-pro"},
		},
		{
			Name:        "worker-code",
			Description: "Roster A1a — DeepSeek-V3 code worker: direct then aggregator fallback",
			Cascade:     []string{"deepseek-direct", "siliconflow-deepseek", "openrouter-deepseek"},
		},
		{
			Name:        "worker-reasoning",
			Description: "Roster A1b — Kimi K2 reasoning worker",
			Cascade:     []string{"moonshot-kimi", "openrouter-kimi"},
		},
		{
			Name:        "tactical",
			Description: "Roster B — fast tactical tier: Gemini Flash then GLM",
			Cascade:     []string{"gemini-flash", "zhipu-glm-flash", "openrouter-glm"},
		},
		{
			Name:        "local-code",
			Description: "Roster C2 — local Ollama coder, zero-cost",
			Cascade:     []string{"ollama-qwen-coder"},
		},
	}
	out := make(map[string]ProfileConfig, len(raw))
	for _, p := range raw {

		cascade := append([]string(nil), p.Cascade...)
		p.Cascade = cascade
		out[p.Name] = p
	}
	return out
}

type ProfileResolverLayers struct {
	Profiles map[string]ProfileConfig

	Orchestrators map[string]OrchestratorConfig

	CheckoutCascade []string

	CheckoutProfile string
}

type ProfileResolver struct {
	defaults map[string]ProfileConfig
	layers   ProfileResolverLayers
}

func NewProfileResolver(layers ProfileResolverLayers) *ProfileResolver {
	return &ProfileResolver{
		defaults: BuiltinProfileDefaults(),
		layers:   layers,
	}
}

func (r *ProfileResolver) Resolve(profile, project string) ([]string, error) {

	name := profile
	if name == "" && project != "" {
		if oc, ok := r.layers.Orchestrators[project]; ok && oc.Default != "" {
			name = oc.Default
		}
	}
	if name == "" {
		name = r.layers.CheckoutProfile
	}
	if name == "" {
		return nil, fmt.Errorf("config.ProfileResolver.Resolve: no profile name (profile arg empty, project %q has no default)", project)
	}

	var cascade []string
	known := false
	if def, ok := r.defaults[name]; ok {
		cascade = def.Cascade
		known = true
	}
	if p, ok := r.layers.Profiles[name]; ok && len(p.Cascade) > 0 {
		cascade = p.Cascade
		known = true
	}
	if project != "" {
		if oc, ok := r.layers.Orchestrators[project]; ok && len(oc.FallbackChain) > 0 {
			cascade = oc.FallbackChain

			known = true
		}
	}
	if len(r.layers.CheckoutCascade) > 0 {
		cascade = r.layers.CheckoutCascade
		known = true
	}
	if !known {
		return nil, fmt.Errorf("config.ProfileResolver.Resolve: unknown profile %q (no built-in default, no profiles.toml entry, no project override)", name)
	}

	if len(cascade) == 0 {
		return nil, fmt.Errorf("config.ProfileResolver.Resolve: profile %q resolved to an empty cascade", name)
	}

	return append([]string(nil), cascade...), nil
}

func (r *ProfileResolver) ProfileNames() []string {
	seen := make(map[string]struct{})
	for n := range r.defaults {
		seen[n] = struct{}{}
	}
	for n := range r.layers.Profiles {
		seen[n] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *ProfileResolver) OperatorProfileNames() []string {
	out := make([]string, 0, len(r.layers.Profiles))
	for n := range r.layers.Profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *ProfileResolver) ProjectFallbackChain(project string) ([]string, error) {
	oc, ok := r.layers.Orchestrators[project]
	if !ok {
		return nil, fmt.Errorf("config.ProfileResolver.ProjectFallbackChain: no orchestrator config for project %q", project)
	}
	if len(oc.FallbackChain) == 0 {
		return nil, fmt.Errorf("config.ProfileResolver.ProjectFallbackChain: project %q FallbackChain is empty", project)
	}
	return append([]string(nil), oc.FallbackChain...), nil
}

func (r *ProfileResolver) OperatorOrchestratorProjects() []string {
	out := make([]string, 0, len(r.layers.Orchestrators))
	for proj, oc := range r.layers.Orchestrators {
		if len(oc.FallbackChain) > 0 {
			out = append(out, proj)
		}
	}
	sort.Strings(out)
	return out
}
