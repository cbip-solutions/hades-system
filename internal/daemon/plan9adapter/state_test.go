// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/state/manifest"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestStateAdapterShowPinHistoryRegenerateAndVerify(t *testing.T) {
	dir := t.TempDir()
	schemaPath := copySystemStateSchema(t, dir)
	manifestPath := filepath.Join(dir, "system-state.toml")
	now := time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC)
	writeStateManifest(t, schemaPath, manifestPath, testStateManifest("0.9.0", "", now))
	st := openMigratedStateStore(t)
	walker := fakeStateWalker{res: manifest.WalkResult{Manifest: testStateManifest("1.0.0", "", now)}}

	a, err := NewStateAdapter(StateAdapterDeps{
		ManifestPath: manifestPath,
		SchemaPath:   schemaPath,
		Store:        st,
		Walker:       walker,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewStateAdapter: %v", err)
	}

	shown, err := a.Show(context.Background())
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if shown.ManualFieldCount != 3 || !strings.Contains(shown.TomlContent, `version = "0.9.0"`) {
		t.Fatalf("Show = %+v", shown)
	}

	diff, err := a.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify before regenerate: %v", err)
	}
	if diff.Match || !strings.Contains(diff.Diff, "zen-swarm.version") {
		t.Fatalf("Verify before regenerate = %+v", diff)
	}

	dry, err := a.Regenerate(context.Background(), true)
	if err != nil {
		t.Fatalf("Regenerate dry-run: %v", err)
	}
	if !dry.DryRun || !containsString(dry.ChangedFields, "zen-swarm.version") || dry.Diff == "" {
		t.Fatalf("Regenerate dry-run = %+v", dry)
	}
	raw, _ := os.ReadFile(manifestPath)
	if strings.Contains(string(raw), `version = "1.0.0"`) {
		t.Fatal("dry-run rewrote system-state.toml")
	}

	if err := a.Pin(context.Background(), "doctrines.default", "max-scope", "operator selected default doctrine", "operator-test"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	history, err := a.History(context.Background(), "doctrines.default")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 || history[0].Field != "doctrines.default" || history[0].NewValue != "max-scope" || history[0].OperatorID != "operator-test" {
		t.Fatalf("History = %+v", history)
	}

	if err := a.Pin(context.Background(), "autonomous-mode.status", "enabled", "operator left context empty", ""); err != nil {
		t.Fatalf("Pin anonymous: %v", err)
	}
	anonymous, err := a.History(context.Background(), "autonomous-mode.status")
	if err != nil {
		t.Fatalf("History anonymous: %v", err)
	}
	if len(anonymous) != 1 || anonymous[0].OperatorID != "anonymous" {
		t.Fatalf("anonymous history = %+v", anonymous)
	}

	written, err := a.Regenerate(context.Background(), false)
	if err != nil {
		t.Fatalf("Regenerate write: %v", err)
	}
	if written.DryRun || !containsString(written.ChangedFields, "zen-swarm.version") {
		t.Fatalf("Regenerate write = %+v", written)
	}
	after, _ := os.ReadFile(manifestPath)
	if !strings.Contains(string(after), `version = "1.0.0"`) || !strings.Contains(string(after), `default = "max-scope"`) {
		t.Fatalf("written manifest:\n%s", after)
	}

	clean, err := a.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify after regenerate: %v", err)
	}
	if !clean.Match || clean.Diff != "" {
		t.Fatalf("Verify after regenerate = %+v", clean)
	}
}

func TestStateAdapterPinRejectsSchemaInvalidValueBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	schemaPath := copySystemStateSchema(t, dir)
	manifestPath := filepath.Join(dir, "system-state.toml")
	now := time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC)
	writeStateManifest(t, schemaPath, manifestPath, testStateManifest("1.0.0", "", now))
	st := openMigratedStateStore(t)

	a, err := NewStateAdapter(StateAdapterDeps{
		ManifestPath: manifestPath,
		SchemaPath:   schemaPath,
		Store:        st,
		Walker:       fakeStateWalker{res: manifest.WalkResult{Manifest: testStateManifest("1.0.0", "", now)}},
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewStateAdapter: %v", err)
	}

	err = a.Pin(context.Background(), "autonomous-mode.status", "surface", "invalid enum must not persist", "operator-test")
	if err == nil {
		t.Fatal("Pin accepted schema-invalid autonomous-mode.status")
	}

	shown, showErr := a.Show(context.Background())
	if showErr != nil {
		t.Fatalf("Show after failed Pin: %v", showErr)
	}
	if strings.Contains(shown.TomlContent, `status = "surface"`) {
		t.Fatalf("invalid status persisted:\n%s", shown.TomlContent)
	}
	history, histErr := a.History(context.Background(), "autonomous-mode.status")
	if histErr != nil {
		t.Fatalf("History after failed Pin: %v", histErr)
	}
	if len(history) != 0 {
		t.Fatalf("invalid pin emitted history rows: %+v", history)
	}
}

type fakeStateWalker struct {
	res manifest.WalkResult
	err error
}

func (f fakeStateWalker) Walk(context.Context) (manifest.WalkResult, error) {
	return f.res, f.err
}

func copySystemStateSchema(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "system-state.schema.json"))
	if err != nil {
		t.Fatalf("read system-state schema: %v", err)
	}
	path := filepath.Join(dir, "system-state.schema.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write system-state schema: %v", err)
	}
	return path
}

func writeStateManifest(t *testing.T, schemaPath, manifestPath string, m manifest.Manifest) {
	t.Helper()
	schema, err := manifest.LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	body, err := manifest.NewRegenerator(schema).Emit(m)
	if err != nil {
		t.Fatalf("Emit manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func testStateManifest(version, defaultDoctrine string, ts time.Time) manifest.Manifest {
	return manifest.Manifest{
		ZenSwarm: manifest.ZenSwarmSection{
			Version:             version,
			Substrate:           "openclaude",
			SubstrateMinVersion: "0.1.0",
		},
		Plans: manifest.PlansSection{
			Released:          []string{"plan-9@v0.9.0"},
			InProgress:        []string{},
			BrainstormPending: []string{},
		},
		Invariants: manifest.InvariantsSection{Count: 150, VerifyCmd: "make verify-invariants"},
		Doctrines: manifest.DoctrinesSection{
			Declared: []string{"default", "max-scope"},
			Default:  defaultDoctrine,
		},
		MCPs: manifest.MCPsSection{Entries: map[string]manifest.MCPEntry{}},
		ADR:  manifest.ADRSection{Count: 100, Location: "docs/decisions/"},
		AutonomousMode: manifest.AutonomousModeSection{
			Status:           "disabled",
			PrerequisitesMet: true,
			LastCheck:        ts,
		},
		Provenance: manifest.Provenance{LastRegenerate: ts},
	}
}

func openMigratedStateStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
