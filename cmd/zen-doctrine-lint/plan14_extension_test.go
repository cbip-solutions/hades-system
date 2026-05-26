package main

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestPlan14AnalyzersRegistered(t *testing.T) {
	registered := Plan14RegisteredAnalyzers()
	expected := []string{"noWebInEcosystem"}
	for _, exp := range expected {
		found := false
		for _, ana := range registered {
			if ana.Name == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Plan 14 analyzer %q not found in Plan14RegisteredAnalyzers()", exp)
		}
	}
}

func TestPlan14AnalyzersAreCanonical(t *testing.T) {
	registered := Plan14RegisteredAnalyzers()

	canonical := map[string]interface{}{
		lint.NoWebInEcosystemAnalyzer.Name: lint.NoWebInEcosystemAnalyzer,
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

// TestPlan14RegisteredAnalyzersLen verifies the slice length is exactly 1.
// A length mismatch indicates either a duplicate was added or one was
// omitted. Subsequent Plan 14 phases that ship additional analyzers
// MUST update this assertion to match.
func TestPlan14RegisteredAnalyzersLen(t *testing.T) {
	got := Plan14RegisteredAnalyzers()
	const want = 1
	if len(got) != want {
		t.Errorf("Plan14RegisteredAnalyzers() returned %d analyzers; want %d "+
			"(noWebInEcosystem)", len(got), want)
	}
}

func TestPlan14AnalyzersNamesUnique(t *testing.T) {
	seen := make(map[string]int)
	for _, ana := range Plan14RegisteredAnalyzers() {
		seen[ana.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("Plan 14 analyzer name %q appears %d times in Plan14RegisteredAnalyzers(); must be unique",
				name, count)
		}
	}
}

func TestPlan14AnalyzersHaveNonEmptyDoc(t *testing.T) {
	for _, ana := range Plan14RegisteredAnalyzers() {
		if ana.Doc == "" {
			t.Errorf("analyzer %q has empty Doc; golangci-lint requires non-empty Doc for --list-linters", ana.Name)
		}
	}
}

func TestPlan14AnalyzersHaveRunFn(t *testing.T) {
	for _, ana := range Plan14RegisteredAnalyzers() {
		if ana.Run == nil {
			t.Errorf("analyzer %q has nil Run function; analyzer would panic when invoked", ana.Name)
		}
	}
}

func TestPlan14AnalyzersReturnCopy(t *testing.T) {
	first := Plan14RegisteredAnalyzers()
	if len(first) == 0 {
		t.Skip("no Plan 14 analyzers to test copy semantics against")
	}

	saved := first[0]
	first[0] = nil

	second := Plan14RegisteredAnalyzers()
	if second[0] == nil {
		t.Error("Plan14RegisteredAnalyzers() returned a slice that shares backing array with package-level state")
	}
	if second[0] != saved {
		t.Error("Plan14RegisteredAnalyzers() second call differs from first — non-deterministic registration")
	}
}
