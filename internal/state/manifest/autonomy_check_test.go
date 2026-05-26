package manifest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newAutonomyValidator(t *testing.T) (*AutonomyValidator, *Regenerator, string) {
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
	v := NewAutonomyValidator(d, schema)
	return v, r, dir
}

func TestAutonomyValidator_StateFreshOK(t *testing.T) {
	v, r, dir := newAutonomyValidator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	fresh := makeFreshManifest()
	fresh.Provenance.LastRegenerate = time.Now().UTC()
	if err := r.RegenerateAndWrite(context.Background(), fresh, manifestPath); err != nil {
		t.Fatal(err)
	}
	res, err := v.ValidateStateFreshness(context.Background(), manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass {
		t.Errorf("expected Pass=true on fresh state.toml, got %+v", res)
	}
}

func TestAutonomyValidator_StateStaleFails(t *testing.T) {
	v, r, dir := newAutonomyValidator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	stale := makeFreshManifest()
	stale.Provenance.LastRegenerate = time.Now().UTC().Add(-10 * 24 * time.Hour)
	if err := r.RegenerateAndWrite(context.Background(), stale, manifestPath); err != nil {
		t.Fatal(err)
	}
	res, err := v.ValidateStateFreshness(context.Background(), manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass {
		t.Error("expected Pass=false on 10d stale state.toml")
	}
	if res.Reason == "" {
		t.Error("Reason should describe the failure")
	}
}

func TestAutonomyValidator_StaleCompensatedByManualEvent(t *testing.T) {
	v, r, dir := newAutonomyValidator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	stale := makeFreshManifest()
	stale.Provenance.LastRegenerate = time.Now().UTC().Add(-10 * 24 * time.Hour)
	if err := r.RegenerateAndWrite(context.Background(), stale, manifestPath); err != nil {
		t.Fatal(err)
	}
	recent := []ChainAnchoredEvent{
		{Type: "state.manual_field_changed", Timestamp: time.Now().UTC().Add(-2 * 24 * time.Hour)},
	}
	res, err := v.ValidateStateFreshness(context.Background(), manifestPath, time.Now().UTC(), recent)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pass {
		t.Errorf("expected Pass=true with recent manual event compensation, got %+v", res)
	}
}

func TestAutonomyValidator_AggregateAllPlan9Prereqs(t *testing.T) {
	v, _, _ := newAutonomyValidator(t)
	stub := &fakePrereqProbes{
		ChainOK: true, BackupOK: true, KnowledgeOK: true, ResearchOK: true,
		WitnessOK: true, ADRsOK: true,
	}
	report := v.ValidateAll(context.Background(), Plan9PrereqInputs{
		StatePath:    "/nonexistent/state.toml",
		Now:          time.Now().UTC(),
		RecentEvents: nil,
		Probes:       stub,
	})
	if report.AllPass {
		t.Error("expected AllPass=false due to missing state.toml")
	}
	if len(report.Failures) == 0 {
		t.Error("Failures slice should be non-empty")
	}
}

func TestAutonomyValidator_FailingProbesSurfacedInFailures(t *testing.T) {
	v, r, dir := newAutonomyValidator(t)
	manifestPath := dir + "/system-state.toml"
	fresh := makeFreshManifest()
	fresh.Provenance.LastRegenerate = time.Now().UTC()
	if err := r.RegenerateAndWrite(context.Background(), fresh, manifestPath); err != nil {
		t.Fatal(err)
	}

	stub := &fakePrereqProbes{
		ChainOK: false, BackupOK: false, KnowledgeOK: false, ResearchOK: false,
		WitnessOK: false, ADRsOK: false,
	}
	report := v.ValidateAll(context.Background(), Plan9PrereqInputs{
		StatePath:    manifestPath,
		Now:          time.Now().UTC(),
		RecentEvents: nil,
		Probes:       stub,
	})
	if report.AllPass {
		t.Error("expected AllPass=false when external probes fail")
	}

	if len(report.Failures) != 6 {
		t.Errorf("expected 6 failures (6 external probes), got %d", len(report.Failures))
	}
	if len(report.Results) != 7 {
		t.Errorf("expected 7 results total, got %d", len(report.Results))
	}
}

func TestAutonomyValidator_NewPanicsOnNilDiffer(t *testing.T) {
	_, _, dir := newAutonomyValidator(t)
	schemaPath := dir + "/schema.json"
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil differ")
		}
	}()
	NewAutonomyValidator(nil, schema)
}

func TestAutonomyValidator_NewPanicsOnNilSchema(t *testing.T) {
	_, _, dir := newAutonomyValidator(t)
	schemaPath := dir + "/schema.json"
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegenerator(schema)
	d := NewDiffer(schema, r)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil schema")
		}
	}()
	NewAutonomyValidator(d, nil)
}

func TestAutonomyValidator_ProbeErrorSurfacedAsFailure(t *testing.T) {
	v, _, _ := newAutonomyValidator(t)
	errProbes := &errorPrereqProbes{}
	report := v.ValidateAll(context.Background(), Plan9PrereqInputs{
		StatePath:    "/nonexistent/state.toml",
		Now:          time.Now().UTC(),
		RecentEvents: nil,
		Probes:       errProbes,
	})
	if report.AllPass {
		t.Error("expected AllPass=false when probes error")
	}
	for _, res := range report.Results {
		if res.Pass {
			t.Errorf("expected all checks to fail when probes error, but %q passed", res.Check)
		}
	}
}

func TestFormatAutonomyReport_AllPass(t *testing.T) {
	report := AutonomyReport{
		AllPass: true,
		Results: []AutonomyResult{
			{Check: "state.freshness", Pass: true},
			{Check: "chain.integrity", Pass: true},
		},
	}
	out := FormatAutonomyReport(report)
	if !contains(out, "ALL PASS") {
		t.Errorf("expected ALL PASS in output, got: %s", out)
	}
	if !contains(out, "state.freshness") {
		t.Errorf("expected state.freshness in output, got: %s", out)
	}
}

func TestFormatAutonomyReport_WithFailures(t *testing.T) {
	report := AutonomyReport{
		AllPass: false,
		Results: []AutonomyResult{
			{Check: "state.freshness", Pass: false, Reason: "stale by 10d"},
			{Check: "chain.integrity", Pass: true},
		},
		Failures: []AutonomyResult{
			{Check: "state.freshness", Pass: false, Reason: "stale by 10d"},
		},
	}
	out := FormatAutonomyReport(report)
	if !contains(out, "FAIL") {
		t.Errorf("expected FAIL in output, got: %s", out)
	}
	if !contains(out, "stale by 10d") {
		t.Errorf("expected failure reason in output, got: %s", out)
	}
}

func TestAutonomyValidator_PanicingProbeRecovered(t *testing.T) {
	v, _, _ := newAutonomyValidator(t)
	stub := &panicPrereqProbes{}
	report := v.ValidateAll(context.Background(), Plan9PrereqInputs{
		StatePath:    "/nonexistent/state.toml",
		Now:          time.Now().UTC(),
		RecentEvents: nil,
		Probes:       stub,
	})
	if report.AllPass {
		t.Error("expected AllPass=false after probe panics")
	}

	if len(report.Results) != 7 {
		t.Errorf("expected 7 results, got %d", len(report.Results))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type fakePrereqProbes struct {
	ChainOK     bool
	BackupOK    bool
	KnowledgeOK bool
	ResearchOK  bool
	WitnessOK   bool
	ADRsOK      bool
}

func (f *fakePrereqProbes) ChainIntegrityFresh(ctx context.Context, threshold time.Duration) (bool, string, error) {
	if f.ChainOK {
		return true, "", nil
	}
	return false, "chain stale", nil
}

func (f *fakePrereqProbes) BackupHealthy(ctx context.Context) (bool, string, error) {
	if f.BackupOK {
		return true, "", nil
	}
	return false, "litestream lag exceeded", nil
}

func (f *fakePrereqProbes) KnowledgeAggregatorReady(ctx context.Context) (bool, string, error) {
	if f.KnowledgeOK {
		return true, "", nil
	}
	return false, "sqlite-vec not loaded", nil
}

func (f *fakePrereqProbes) ResearchCacheReady(ctx context.Context) (bool, string, error) {
	if f.ResearchOK {
		return true, "", nil
	}
	return false, "revalidation queue too deep", nil
}

func (f *fakePrereqProbes) WitnessKeyValid(ctx context.Context) (bool, string, error) {
	if f.WitnessOK {
		return true, "", nil
	}
	return false, "witness key past rotation cadence", nil
}

func (f *fakePrereqProbes) ADRsValid(ctx context.Context) (bool, string, error) {
	if f.ADRsOK {
		return true, "", nil
	}
	return false, "ADR validator failed", nil
}

type errorPrereqProbes struct{}

func (e *errorPrereqProbes) ChainIntegrityFresh(ctx context.Context, threshold time.Duration) (bool, string, error) {
	return false, "", fmt.Errorf("chain probe unavailable")
}
func (e *errorPrereqProbes) BackupHealthy(ctx context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("backup probe unavailable")
}
func (e *errorPrereqProbes) KnowledgeAggregatorReady(ctx context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("knowledge probe unavailable")
}
func (e *errorPrereqProbes) ResearchCacheReady(ctx context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("research probe unavailable")
}
func (e *errorPrereqProbes) WitnessKeyValid(ctx context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("witness probe unavailable")
}
func (e *errorPrereqProbes) ADRsValid(ctx context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("adrs probe unavailable")
}

type panicPrereqProbes struct{}

func (p *panicPrereqProbes) ChainIntegrityFresh(ctx context.Context, threshold time.Duration) (bool, string, error) {
	panic("chain probe panic")
}
func (p *panicPrereqProbes) BackupHealthy(ctx context.Context) (bool, string, error) {
	panic("backup probe panic")
}
func (p *panicPrereqProbes) KnowledgeAggregatorReady(ctx context.Context) (bool, string, error) {
	panic("knowledge probe panic")
}
func (p *panicPrereqProbes) ResearchCacheReady(ctx context.Context) (bool, string, error) {
	panic("research probe panic")
}
func (p *panicPrereqProbes) WitnessKeyValid(ctx context.Context) (bool, string, error) {
	panic("witness probe panic")
}
func (p *panicPrereqProbes) ADRsValid(ctx context.Context) (bool, string, error) {
	panic("adrs probe panic")
}
