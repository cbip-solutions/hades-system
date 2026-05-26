package manifest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/state/manifest/walkers"
)

func staticWalkerCfg(t *testing.T) WalkerConfig {
	t.Helper()
	dir := t.TempDir()
	idx := filepath.Join(dir, "_index.json")
	gomod := filepath.Join(dir, "go.mod")
	stamp := filepath.Join(dir, "autonomy_check.json")
	root := dir

	if err := os.WriteFile(idx, []byte(`{"adrs":[{"id":"ADR-0001"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gomod, []byte("module github.com/cbip-solutions/hades-system\n\ngo 1.25.6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte(`{"prerequisites_met":true,"last_check_at":"2026-05-06T08:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	return WalkerConfig{
		GitRepoRoot:        dir,
		ADRIndexPath:       idx,
		GoModPath:          gomod,
		InvariantGrepRoot:  root,
		AutonomyStampPath:  stamp,
		ZenSwarmVersion:    "0.9.0",
		DoctrineRegistryFn: func() []string { return []string{"max-scope", "default", "capa-firewall"} },
	}
}

func TestWalker_AggregatesAllSubWalkers(t *testing.T) {
	cfg := staticWalkerCfg(t)
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Manifest.ZenSwarm.Version != "0.9.0" {
		t.Errorf("ZenSwarm.Version = %q", res.Manifest.ZenSwarm.Version)
	}
	if res.Manifest.ZenSwarm.Substrate != "openclaude" {
		t.Errorf("Substrate = %q, want openclaude", res.Manifest.ZenSwarm.Substrate)
	}
	if res.Manifest.ADR.Count != 1 {
		t.Errorf("ADR.Count = %d", res.Manifest.ADR.Count)
	}
	if !res.Manifest.AutonomousMode.PrerequisitesMet {
		t.Error("PrerequisitesMet should be true from stamp")
	}
}

func TestWalker_AggregatesMissingSources(t *testing.T) {
	cfg := WalkerConfig{
		GitRepoRoot:        "/nonexistent/repo",
		ADRIndexPath:       "/nonexistent/_index.json",
		GoModPath:          "/nonexistent/go.mod",
		InvariantGrepRoot:  "/nonexistent/internal",
		AutonomyStampPath:  "/nonexistent/stamp.json",
		ZenSwarmVersion:    "0.9.0",
		DoctrineRegistryFn: nil,
	}
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v (expected nil; degradation reported via MissingSources)", err)
	}
	if len(res.MissingSources) == 0 {
		t.Error("MissingSources should be non-empty when all walkers degrade")
	}
}

func TestWalker_StaticFieldsAlwaysSet(t *testing.T) {
	cfg := staticWalkerCfg(t)
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Manifest.ZenSwarm.Substrate != "openclaude" {
		t.Errorf("Substrate should always be openclaude, got %q", res.Manifest.ZenSwarm.Substrate)
	}
	if res.Manifest.ADR.Location != "docs/decisions/" {
		t.Errorf("ADR.Location: got %q, want docs/decisions/", res.Manifest.ADR.Location)
	}
	if res.Manifest.Invariants.VerifyCmd != "make verify-invariants" {
		t.Errorf("VerifyCmd: got %q", res.Manifest.Invariants.VerifyCmd)
	}
}

func TestWalker_DoctrinesDeclaredSorted(t *testing.T) {
	cfg := staticWalkerCfg(t)
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(res.Manifest.Doctrines.Declared) != 3 {
		t.Fatalf("Declared count = %d, want 3", len(res.Manifest.Doctrines.Declared))
	}
	if res.Manifest.Doctrines.Declared[0] != "capa-firewall" {
		t.Errorf("Declared[0] = %q, want capa-firewall", res.Manifest.Doctrines.Declared[0])
	}
}

func TestWalker_MCPEntriesEmpty(t *testing.T) {
	cfg := staticWalkerCfg(t)
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Manifest.MCPs.Entries == nil {
		t.Error("MCPs.Entries should be non-nil (empty-map shape) for TOML emit stability")
	}
	if len(res.Manifest.MCPs.Entries) != 0 {
		t.Errorf("MCPs.Entries count = %d, want 0 (post-ADR-0080 Hermes registry)",
			len(res.Manifest.MCPs.Entries))
	}
}

func TestWalker_ProvenanceRecorded(t *testing.T) {
	cfg := staticWalkerCfg(t)
	w := NewWalker(cfg)
	res, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if res.Manifest.Provenance.LastRegenerate.IsZero() {
		t.Error("Provenance.LastRegenerate should be set")
	}
}

var _ = walkers.NewGitWalker
