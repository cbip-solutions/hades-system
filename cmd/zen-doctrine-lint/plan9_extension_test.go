package main

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestPlan9AnalyzersRegistered(t *testing.T) {
	registered := RegisteredAnalyzers()
	expected := []string{"noWebInAggregator", "noAutoPromote", "noCrossProjectAtTessera"}
	for _, exp := range expected {
		found := false
		for _, ana := range registered {
			if ana.Name == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Plan 9 analyzer %q not found in RegisteredAnalyzers()", exp)
		}
	}
}

func TestPlan9AnalyzersAreCanonical(t *testing.T) {
	registered := RegisteredAnalyzers()

	canonical := map[string]interface{}{
		lint.NoWebInAggregatorAnalyzer.Name:       lint.NoWebInAggregatorAnalyzer,
		lint.NoAutoPromoteAnalyzer.Name:           lint.NoAutoPromoteAnalyzer,
		lint.NoCrossProjectAtTesseraAnalyzer.Name: lint.NoCrossProjectAtTesseraAnalyzer,
	}

	for _, ana := range registered {
		want, ok := canonical[ana.Name]
		if !ok {
			continue
		}
		if ana != want {
			t.Errorf("analyzer %q: registered instance pointer differs from canonical lint.%sAnalyzer — "+
				"must use the canonical exported var, not a copy", ana.Name, ana.Name)
		}
	}
}

func TestPlan9RegisteredAnalyzersLen(t *testing.T) {
	got := RegisteredAnalyzers()
	const want = 3
	if len(got) != want {
		t.Errorf("RegisteredAnalyzers() returned %d analyzers; want %d "+
			"(noWebInAggregator, noAutoPromote, noCrossProjectAtTessera)", len(got), want)
	}
}

func TestPlan9AnalyzersNamesUnique(t *testing.T) {
	seen := make(map[string]int)
	for _, ana := range RegisteredAnalyzers() {
		seen[ana.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("Plan 9 analyzer name %q appears %d times in RegisteredAnalyzers(); must be unique",
				name, count)
		}
	}
}

func TestPlan9AnalyzersHaveNonEmptyDoc(t *testing.T) {
	for _, ana := range RegisteredAnalyzers() {
		if ana.Doc == "" {
			t.Errorf("analyzer %q has empty Doc; golangci-lint requires non-empty Doc for --list-linters", ana.Name)
		}
	}
}

func TestPlan9AnalyzersHaveRunFn(t *testing.T) {
	for _, ana := range RegisteredAnalyzers() {
		if ana.Run == nil {
			t.Errorf("analyzer %q has nil Run function; analyzer would panic when invoked", ana.Name)
		}
	}
}
