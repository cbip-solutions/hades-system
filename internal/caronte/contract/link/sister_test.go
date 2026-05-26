package link

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestSisterClaim_ConfidenceForMethodTupleIsLoadBearing(t *testing.T) {

	if c, err := confidenceForMethod(LinkArtifact, "proto_import"); err != nil || c != ConfExactProtoImport {
		t.Errorf("confidenceForMethod(LinkArtifact, proto_import) = (%q, %v); want (exact_proto_import, nil)", c, err)
	}
	if c, err := confidenceForMethod(LinkArtifact, "openapi"); err != nil || c != ConfSpecArtifact {
		t.Errorf("confidenceForMethod(LinkArtifact, openapi) = (%q, %v); want (spec_artifact, nil)", c, err)
	}
	if c, err := confidenceForMethod(LinkStatic, ""); err != nil || c != ConfStaticPath {
		t.Errorf("confidenceForMethod(LinkStatic, _) = (%q,%v); want (static_path, nil)", c, err)
	}
	if c, err := confidenceForMethod(LinkFuzzy, ""); err != nil || c != ConfFuzzyPath {
		t.Errorf("confidenceForMethod(LinkFuzzy, _) = (%q,%v); want (fuzzy_path, nil)", c, err)
	}

	if err := checkTierConsistency(LinkFuzzy, ConfExactProtoImport); !errors.Is(err, ErrConfidenceTierDowngrade) {
		t.Errorf("checkTierConsistency(LinkFuzzy, ConfExactProtoImport) = %v; want ErrConfidenceTierDowngrade", err)
	}
}

// TestSisterClaim_AmbiguousResolutionRefuses — sister to the
// resolveTargetRepo doc-comment claim (resolve.go §"never false-link;
// refuse on ambiguous"): two manifest entries pointing at DISTINCT
// target_repos for the same base_url_ref MUST collapse to
// ErrAmbiguousResolution, NEVER silently pick one.
//
// Bite-check: revert resolveTargetRepo's `default:` arm to
// `return uniq[0], LinkCaronteYAML, nil` (collapse-on-first instead of
// refuse) → this test FAILS with err==nil + repo=="repo-a". Restore →
// PASS. Pinned at sister-level (not just at the existing resolve_test.go
// coverage) because the doctrine claim is load-bearing: a regression
// here would silently downgrade inv-zen-265 + inv-zen-271's no-false-
// link guarantee, and the resolve_test.go variant uses the heavier
// pattern-collision path; a flat 2-entry collision keeps the bite
// surface minimal.
func TestSisterClaim_AmbiguousResolutionRefuses(t *testing.T) {
	m := &yaml.Manifest{
		SchemaVersion: 1,
		Services: []yaml.Service{
			{BaseURL: "http://api.example.com", TargetRepo: "repo-a"},
			{BaseURL: "http://api.example.com", TargetRepo: "repo-b"},
		},
		UnresolvedPolicy: yaml.PolicySurface,
	}
	_, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "http://api.example.com"}, m)
	if !errors.Is(err, ErrAmbiguousResolution) {
		t.Fatalf("ambiguous base_url should refuse with ErrAmbiguousResolution, got %v", err)
	}
}

func TestSisterClaim_ConsumersForImportBoundary(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "link.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse link.go: %v", err)
	}
	allowed := map[string]bool{
		"github.com/cbip-solutions/hades-system/internal/caronte/coordinated":      true,
		"github.com/cbip-solutions/hades-system/internal/caronte/store":            true,
		"github.com/cbip-solutions/hades-system/internal/caronte/store/federation": true,
		"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract": true,
		"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml":    true,
		"context":       true,
		"encoding/json": true,
		"fmt":           true,
		"time":          true,
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == "github.com/cbip-solutions/hades-system/internal/store" {
			t.Errorf("link.go MUST NOT import internal/store (inv-zen-031 + inv-zen-271)")
		}
		if !allowed[path] {
			t.Errorf("link.go has unexpected import %q; FIX-1 boundary allows only the configured set", path)
		}
	}
}
