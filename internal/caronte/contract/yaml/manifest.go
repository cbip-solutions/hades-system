// SPDX-License-Identifier: MIT
package yaml

import "regexp"

type Manifest struct {
	SchemaVersion    int              `yaml:"schema_version"`
	Services         []Service        `yaml:"services"`
	UnresolvedPolicy UnresolvedPolicy `yaml:"unresolved_policy,omitempty"`

	compiled []*regexp.Regexp `yaml:"-"`
}

func (m *Manifest) PatternFor(i int) *regexp.Regexp {
	if m == nil || i < 0 || i >= len(m.compiled) {
		return nil
	}
	return m.compiled[i]
}

func (m *Manifest) AddPatternService(pattern, targetRepo string) error {
	if err := validatePatternRunes(pattern); err != nil {
		return err
	}
	if err := validatePatternRegexDoS(pattern); err != nil {
		return err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	m.Services = append(m.Services, Service{BaseURLPattern: pattern, TargetRepo: targetRepo})
	m.compiled = append(m.compiled, re)
	return nil
}

// Service is one base-URL reference → target_repo mapping. Exactly one of
// BaseURL / BaseURLEnv / BaseURLPattern MUST be set (validateBaseURLExclusive
// in Task F-3 enforces this with ErrMultipleBaseURLVariants).
//
// - BaseURL: literal URL prefix the client hardcodes (e.g., `http://billing-svc`).
// Matched against api_calls.base_url_ref EXACTLY ( resolve.go treats
// base_url_ref as the prefix the extractor saw in the source code).
// - BaseURLEnv: env var NAME the client reads at runtime (e.g., `AUTH_SVC_URL`
// for a `os.Getenv("AUTH_SVC_URL")` call). The runtime env VALUE is
// irrelevant — only the NAME is the federation key (spec §6 base_url_env
// resolution semantics).
// - BaseURLPattern: regex pattern (e.g., `^https?://shipping-[a-z0-9]+\.internal/`)
// compiled at Load time AFTER MaxPatternRunes + regexp/syntax DoS probe.
// Useful for sharded/regional services.
// - TargetRepo: workspace member project_id (Workspace.Projects() roster). A
// non-member yields ErrUnknownTargetRepo at Load time.
// - Notes: optional human note (operator-only; not used by the linker).
type Service struct {
	BaseURL        string `yaml:"base_url,omitempty"`
	BaseURLEnv     string `yaml:"base_url_env,omitempty"`
	BaseURLPattern string `yaml:"base_url_pattern,omitempty"`
	TargetRepo     string `yaml:"target_repo"`
	Notes          string `yaml:"notes,omitempty"`
}

type UnresolvedPolicy string

const (
	PolicySurface UnresolvedPolicy = "surface"

	PolicyFail UnresolvedPolicy = "fail"

	PolicySilent UnresolvedPolicy = "silent"
)

const DefaultUnresolvedPolicy = PolicySurface
