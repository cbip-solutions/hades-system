package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newDifferAndRegen(t *testing.T) (*Differ, *Regenerator, string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegenerator(schema)
	d := NewDiffer(schema, r)
	return d, r, dir
}

func TestDiffer_NoDrift_NoFreshnessFail(t *testing.T) {
	d, r, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	fresh := makeFreshManifest()
	fresh.Provenance.LastRegenerate = time.Now().UTC()
	if err := r.RegenerateAndWrite(context.Background(), fresh, manifestPath); err != nil {
		t.Fatal(err)
	}
	report, err := d.Verify(context.Background(), fresh, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(report.AutoDriftPaths) != 0 {
		t.Errorf("AutoDriftPaths: got %v, want empty", report.AutoDriftPaths)
	}
	if report.FreshnessExceeded {
		t.Error("FreshnessExceeded: got true, want false (just-written file)")
	}
}

func TestDiffer_DetectsAutoDrift(t *testing.T) {
	d, r, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	stored := makeFreshManifest()
	stored.ZenSwarm.Version = "0.8.0"
	if err := r.RegenerateAndWrite(context.Background(), stored, manifestPath); err != nil {
		t.Fatal(err)
	}
	fresh := makeFreshManifest()
	report, err := d.Verify(context.Background(), fresh, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(report.AutoDriftPaths) == 0 {
		t.Error("expected AutoDriftPaths to be non-empty when stored version drifts from fresh")
	}
}

func TestDiffer_FreshnessExceededWithoutRecentEvents(t *testing.T) {
	d, r, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	stored := makeFreshManifest()
	stored.Provenance.LastRegenerate = time.Now().UTC().Add(-10 * 24 * time.Hour)
	if err := r.RegenerateAndWrite(context.Background(), stored, manifestPath); err != nil {
		t.Fatal(err)
	}
	report, err := d.Verify(context.Background(), stored, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.FreshnessExceeded {
		t.Error("FreshnessExceeded: got false, want true (10d old)")
	}
}

func TestDiffer_FreshnessCompensatedByRecentManualEvents(t *testing.T) {
	d, r, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	stored := makeFreshManifest()
	stored.Provenance.LastRegenerate = time.Now().UTC().Add(-10 * 24 * time.Hour)
	if err := r.RegenerateAndWrite(context.Background(), stored, manifestPath); err != nil {
		t.Fatal(err)
	}
	recent := []ChainAnchoredEvent{
		{Type: "state.manual_field_changed", Timestamp: time.Now().UTC().Add(-2 * 24 * time.Hour)},
	}
	report, err := d.Verify(context.Background(), stored, manifestPath, time.Now().UTC(), recent)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.FreshnessExceeded {
		t.Error("FreshnessExceeded: got true, want false (recent manual event compensates)")
	}
}

func TestDiffer_FailsCITargetSemantics(t *testing.T) {
	r := DiffReport{}
	if r.IsFailure() {
		t.Error("empty report is not failure")
	}
	r.AutoDriftPaths = []string{"zen-swarm.version"}
	if !r.IsFailure() {
		t.Error("drift should be failure")
	}
	r2 := DiffReport{FreshnessExceeded: true}
	if !r2.IsFailure() {
		t.Error("stale freshness should be failure")
	}
}

func TestDiffer_MissingManifest_ReturnsFileMissing(t *testing.T) {
	d, _, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "nonexistent-state.toml")
	fresh := makeFreshManifest()
	report, err := d.Verify(context.Background(), fresh, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("Verify with missing file: %v", err)
	}
	if len(report.AutoDriftPaths) == 0 {
		t.Error("expected AutoDriftPaths to contain <file-missing> sentinel")
	}
	if report.AutoDriftPaths[0] != "<file-missing>" {
		t.Errorf("expected <file-missing> sentinel, got %q", report.AutoDriftPaths[0])
	}
	if !report.IsFailure() {
		t.Error("missing file must be IsFailure()")
	}
}

func TestNewDiffer_NilSchemaPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewDiffer(nil, r) should panic")
		}
	}()
	_, _, dir := newDifferAndRegen(t)
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegenerator(schema)
	NewDiffer(nil, r)
}

func TestNewDiffer_NilRegeneratorPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewDiffer(schema, nil) should panic")
		}
	}()
	_, _, dir := newDifferAndRegen(t)
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	NewDiffer(schema, nil)
}

func TestEqualManifestPath_BothInvalid(t *testing.T) {
	var a, b Manifest

	got := equalManifestPath(a, b, "nonexistent-section.nonexistent-leaf")
	if !got {
		t.Error("both-invalid path should be treated as equal (absence == absence)")
	}
}

func TestEqualManifestPath_DifferingValues(t *testing.T) {
	a := makeFreshManifest()
	var b Manifest
	b.ZenSwarm.Version = "0.8.0"
	result := equalManifestPath(a, b, "zen-swarm.version")
	if result {
		t.Error("equalManifestPath should return false for differing version values")
	}
}

func TestEqualManifestPath_TopLevelNoDot(t *testing.T) {
	a := makeFreshManifest()
	b := makeFreshManifest()

	got := equalManifestPath(a, b, "nodot")
	if !got {
		t.Error("no-dot path: both zero Values are treated as equal absence")
	}
}

func TestDiffer_MalformedTOML_ReturnsError(t *testing.T) {
	d, _, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(manifestPath, []byte("this is not [[valid toml }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := makeFreshManifest()
	_, err := d.Verify(context.Background(), fresh, manifestPath, time.Now().UTC(), nil)
	if !errors.Is(err, ErrManifestInvalid) {
		t.Errorf("Verify with malformed TOML: got %v, want errors.Is ErrManifestInvalid", err)
	}
}

func TestDiffer_NonManualFieldDriftNotDetectedAsManual(t *testing.T) {

	d, r, dir := newDifferAndRegen(t)
	manifestPath := filepath.Join(dir, "system-state.toml")

	stored := makeFreshManifest()
	stored.ZenSwarm.SubstrateMinVersion = "0.7.1"
	stored.Doctrines.Default = "max-scope"
	stored.AutonomousMode.Status = "supervised"
	if err := r.RegenerateAndWrite(context.Background(), stored, manifestPath); err != nil {
		t.Fatal(err)
	}

	fresh := makeFreshManifest()

	report, err := d.Verify(context.Background(), fresh, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	for _, p := range report.AutoDriftPaths {
		if p == "zen-swarm.substrate_min_version" || p == "doctrines.default" || p == "autonomous-mode.status" {
			t.Errorf("manual field %q must not appear in AutoDriftPaths", p)
		}
	}
}
