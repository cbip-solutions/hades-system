package main

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/loretrailer"
)

func TestPlan19AnalyzersRegistered(t *testing.T) {
	registered := Plan19RegisteredAnalyzers()
	found := false
	for _, ana := range registered {
		if ana.Name == "loretrailer" {
			found = true
		}
	}
	if !found {
		t.Error("loretrailer analyzer not found in Plan19RegisteredAnalyzers()")
	}
}

func TestPlan19AnalyzerIsCanonical(t *testing.T) {
	for _, ana := range Plan19RegisteredAnalyzers() {
		if ana.Name == "loretrailer" && ana != loretrailer.Analyzer {
			t.Error("registered loretrailer differs from canonical loretrailer.Analyzer — must use the exported var")
		}
	}
}

func TestPlan19RegisteredAnalyzersLen(t *testing.T) {
	if got := len(Plan19RegisteredAnalyzers()); got != 1 {
		t.Errorf("Plan19RegisteredAnalyzers() returned %d; want 1 (loretrailer)", got)
	}
}

func TestPlan19AnalyzerHasDocAndRun(t *testing.T) {
	for _, ana := range Plan19RegisteredAnalyzers() {
		if ana.Doc == "" {
			t.Errorf("analyzer %q has empty Doc", ana.Name)
		}
		if ana.Run == nil {
			t.Errorf("analyzer %q has nil Run", ana.Name)
		}
	}
}

func TestPlan8And9And14SurfacesUnchanged(t *testing.T) {
	if got := len(standaloneAnalyzers()); got != 4 {
		t.Errorf("standaloneAnalyzers() = %d; want 4 (conventional_commit, nostore, nostub, tierspertool) — Plan 19 must not modify it", got)
	}
	if got := len(RegisteredAnalyzers()); got != 3 {
		t.Errorf("RegisteredAnalyzers() (Plan 9) = %d; want 3 — Plan 19 must not modify it", got)
	}
	if got := len(Plan14RegisteredAnalyzers()); got != 1 {
		t.Errorf("Plan14RegisteredAnalyzers() = %d; want 1 — Plan 19 must not modify it", got)
	}
}
