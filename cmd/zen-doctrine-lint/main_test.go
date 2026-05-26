// Package main_test verifies the cmd/zen-doctrine-lint golangci-lint module
// plugin contract per spec §1 Q4 B. The plugin's New constructor MUST return
// a register.LinterPlugin whose BuildAnalyzers() yields the 3 Plan 8 analyzers
// (nostub, nostore, conventional_commit) in stable order.
package main

import (
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/golangci/plugin-module-register/register"
)

func TestPluginNewReturnsLinterPlugin(t *testing.T) {
	p, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) returned error: %v", err)
	}
	if p == nil {
		t.Fatal("New(nil) returned (nil, nil); want non-nil LinterPlugin")
	}

	var _ register.LinterPlugin = p
}

func TestPluginBuildAnalyzersReturnsAll(t *testing.T) {
	p, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	analyzers, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers: %v", err)
	}
	if len(analyzers) != 4 {
		t.Fatalf("BuildAnalyzers returned %d analyzers; want 4 (nostub, nostore, conventional_commit, tierspertool)", len(analyzers))
	}
	gotNames := make([]string, len(analyzers))
	for i, a := range analyzers {
		if a == nil {
			t.Errorf("analyzers[%d] is nil", i)
			continue
		}
		if a.Name == "" {
			t.Errorf("analyzers[%d].Name is empty", i)
		}
		if a.Doc == "" {
			t.Errorf("analyzers[%d].Doc is empty (golangci-lint requires non-empty Doc)", i)
		}
		if a.Run == nil {
			t.Errorf("analyzers[%d].Run is nil", i)
		}
		gotNames[i] = a.Name
	}
	wantNames := []string{"conventional_commit", "nostore", "nostub", "tierspertool"}
	sort.Strings(gotNames)
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Errorf("analyzers name list = %v; want %v (sorted)", gotNames, wantNames)
			break
		}
	}
}

func TestPluginGetLoadModeTypesInfo(t *testing.T) {
	p, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.GetLoadMode(); got != register.LoadModeTypesInfo {
		t.Errorf("GetLoadMode = %q; want %q", got, register.LoadModeTypesInfo)
	}
}

func TestPluginRegisteredByInit(t *testing.T) {
	npf, err := register.GetPlugin("zen-doctrine-lint")
	if err != nil {
		t.Fatalf("GetPlugin(zen-doctrine-lint): %v", err)
	}
	if npf == nil {
		t.Fatal("GetPlugin returned nil NewPlugin; init() did not register")
	}

	p, err := npf(nil)
	if err != nil {
		t.Fatalf("registered NewPlugin(nil): %v", err)
	}
	if p == nil {
		t.Fatal("registered factory returned nil plugin")
	}
}

func TestPluginAnalyzerNamesUnique(t *testing.T) {
	p, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	analyzers, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers: %v", err)
	}
	seen := make(map[string]int)
	for _, a := range analyzers {
		seen[a.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("analyzer name %q appears %d times; must be unique", name, count)
		}
	}
}

func TestStandaloneAnalyzersMatchesPluginSet(t *testing.T) {
	standalone := standaloneAnalyzers()
	if len(standalone) != 4 {
		t.Fatalf("standaloneAnalyzers returned %d; want 4", len(standalone))
	}
	p, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plug, err := p.BuildAnalyzers()
	if err != nil {
		t.Fatalf("BuildAnalyzers: %v", err)
	}
	if len(plug) != len(standalone) {
		t.Fatalf("plugin set size %d != standalone set size %d", len(plug), len(standalone))
	}
	for i := range plug {
		if plug[i] != standalone[i] {
			t.Errorf("position %d: plugin %q != standalone %q", i, plug[i].Name, standalone[i].Name)
		}
	}
}

func TestMainBinaryRunsHelpAndExitsZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows path semantics differ; skipping subprocess smoke")
	}
	cmd := exec.Command("go", "run", ".", "-V=full")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -V=full failed: %v\n%s", err, out)
	}

	s := strings.TrimSpace(string(out))
	if s == "" {
		t.Errorf("expected non-empty -V=full output; got empty")
	}
}
