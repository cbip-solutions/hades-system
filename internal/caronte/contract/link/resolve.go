// SPDX-License-Identifier: MIT
package link

import (
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type LinkMethod string

const (
	LinkArtifact LinkMethod = "artifact"

	LinkCaronteYAML LinkMethod = "caronte_yaml"

	LinkStatic LinkMethod = "static"

	LinkFuzzy LinkMethod = "fuzzy"
)

type Confidence string

const (
	ConfExactProtoImport Confidence = "exact_proto_import"

	ConfSpecArtifact Confidence = "spec_artifact"

	ConfStaticPath Confidence = "static_path"

	ConfFuzzyPath Confidence = "fuzzy_path"
)

// confidenceForMethod returns the canonical Confidence tier for a given
// LinkMethod + artifact source hint. The mapping is the load-bearing
// tier-precedence contract (master C-5):
//
//	(LinkArtifact, "proto_import")      → ConfExactProtoImport
//	(LinkArtifact, "openapi"|"sdl"|"proto_spec") → ConfSpecArtifact
//	(LinkStatic, _)                     → ConfStaticPath
//	(LinkFuzzy, _)                      → ConfFuzzyPath
//	(LinkCaronteYAML, _)                → ErrConfidenceTierDowngrade
//	                                      (caronte_yaml MUST be paired with
//	                                      a static / fuzzy path-match before
//	                                      becoming a confidence-bearing tier)
//
// ErrConfidenceTierDowngrade is returned when the caller passes a
// (Confidence, LinkMethod) tuple inconsistent with this table — the guard
// against the spec §13.4 adversarial "forged contract_links" path
// (confidence='exact_proto_import' with link_method='fuzzy' MUST refuse).
func confidenceForMethod(m LinkMethod, artifactSource string) (Confidence, error) {
	switch m {
	case LinkArtifact:
		switch artifactSource {
		case "proto_import":
			return ConfExactProtoImport, nil
		case "openapi", "sdl", "proto_spec":
			return ConfSpecArtifact, nil
		default:
			return "", fmt.Errorf("%w: artifact with unknown source %q", ErrConfidenceTierDowngrade, artifactSource)
		}
	case LinkStatic:
		return ConfStaticPath, nil
	case LinkCaronteYAML:

		return "", fmt.Errorf("%w: caronte_yaml alone is not a confidence-bearing tier (must be paired with static or fuzzy path match)", ErrConfidenceTierDowngrade)
	case LinkFuzzy:
		return ConfFuzzyPath, nil
	default:
		return "", fmt.Errorf("%w: unknown LinkMethod %q", ErrConfidenceTierDowngrade, m)
	}
}

// resolveTargetRepo resolves an api_calls.base_url_ref to a target_repo via
// the manifest. Returns (target_repo, LinkCaronteYAML, nil) on a unique hit
// across all three sub-tiers (env / literal / pattern, declaration order),
// (_, _, ErrNoManifestEntry) on zero hits, (_, _, ErrAmbiguousResolution)
// on two-or-more hits TO DISTINCT REPOS.
//
// Sub-tier precedence is declaration order — the spec §6 does not promise
// any tier wins over another (the operator MUST keep the manifest
// unambiguous; the linker's job is to enforce it, not to silently prefer
// one over the other). An env-name hit + a literal-URL hit on the same
// base_url_ref string ⇒ ErrAmbiguousResolution (unless the same TargetRepo,
// in which case the dedupe collapses both to one hit).
func resolveTargetRepo(call store.APICall, m *yaml.Manifest) (string, LinkMethod, error) {
	if m == nil {
		return "", "", ErrNoManifestEntry
	}
	ref := call.BaseURLRef
	if ref == "" {

		return "", "", ErrNoManifestEntry
	}
	matches := make([]string, 0, 1)
	for i, svc := range m.Services {
		switch {
		case svc.BaseURLEnv != "" && svc.BaseURLEnv == ref:
			matches = append(matches, svc.TargetRepo)
		case svc.BaseURL != "" && svc.BaseURL == ref:
			matches = append(matches, svc.TargetRepo)
		case svc.BaseURLPattern != "":
			if re := m.PatternFor(i); re != nil && re.MatchString(ref) {
				matches = append(matches, svc.TargetRepo)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", "", ErrNoManifestEntry
	case 1:
		return matches[0], LinkCaronteYAML, nil
	default:

		uniq := dedupStrings(matches)
		if len(uniq) == 1 {
			return uniq[0], LinkCaronteYAML, nil
		}
		return "", "", fmt.Errorf("%w: %d entries match (%v)", ErrAmbiguousResolution, len(uniq), uniq)
	}
}

func dedupStrings(xs []string) []string {
	seen := make(map[string]bool, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
