package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/mcp"
)

func TestAvailabilityCheckSatisfiesCheck(t *testing.T) {
	var _ check.Check = (*mcp.AvailabilityCheck)(nil)
}

func TestAvailabilityCheckPassAllAvailable(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "playwright", Tier: 2, PackageManager: "npm", PackageName: "@playwright/mcp", MinVersion: "1.45.0", RiskTier: "low"},
		{Name: "filesystem", Tier: 1, PackageManager: "binary", PackageName: "fs-mcp", MinVersion: "0.5.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs: specs,
		Prober: &stubProber{versions: map[string]string{
			"npm|@playwright/mcp": "1.50.0",
			"binary|fs-mcp":       "0.5.0",
		}},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass; detail=%q", got.Status, got.Detail)
	}
	if got.Name != "mcp.curated-availability" {
		t.Errorf("Name = %q, want mcp.curated-availability", got.Name)
	}
}

func TestAvailabilityCheckFailNotFound(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "missing-mcp", Tier: 4, PackageManager: "npm", PackageName: "@nope/nope", MinVersion: "1.0.0", RiskTier: "high"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &stubProber{errs: map[string]error{"npm|@nope/nope": mcp.ErrPackageNotFound}},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (package not found)", got.Status)
	}
	if !strings.Contains(got.Detail, "NOT FOUND") {
		t.Errorf("Detail missing NOT FOUND; got %q", got.Detail)
	}
}

func TestAvailabilityCheckWarnVersionDrift(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "playwright", Tier: 2, PackageManager: "npm", PackageName: "@playwright/mcp", MinVersion: "1.45.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &stubProber{versions: map[string]string{"npm|@playwright/mcp": "1.40.0"}},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusWarn {
		t.Errorf("Status = %v, want StatusWarn (version drift)", got.Status)
	}
	if !strings.Contains(got.Detail, "WARN") {
		t.Errorf("Detail missing WARN; got %q", got.Detail)
	}
}

func TestAvailabilityCheckSkipManagerNotInstalled(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "python-mcp", Tier: 3, PackageManager: "pip", PackageName: "py-mcp", MinVersion: "0.1.0", RiskTier: "medium"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &stubProber{errs: map[string]error{"pip|py-mcp": mcp.ErrManagerNotInstalled}},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (manager missing)", got.Status)
	}
	if !strings.Contains(got.Detail, "SKIP") {
		t.Errorf("Detail missing SKIP; got %q", got.Detail)
	}
}

func TestAvailabilityCheckCollapseToWorst(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "a", PackageManager: "npm", PackageName: "a", MinVersion: "1.0.0", RiskTier: "low"},
		{Name: "b", PackageManager: "npm", PackageName: "b", MinVersion: "1.0.0", RiskTier: "low"},
		{Name: "c", PackageManager: "npm", PackageName: "c", MinVersion: "1.0.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs: specs,
		Prober: &stubProber{
			versions: map[string]string{"npm|a": "1.0.0"},
			errs: map[string]error{
				"npm|b": mcp.ErrPackageNotFound,
				"npm|c": mcp.ErrManagerNotInstalled,
			},
		},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (worst across rows)", got.Status)
	}
}

func TestAvailabilityCheckEmptyCatalog(t *testing.T) {
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  nil,
		Prober: &stubProber{},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (empty catalog)", got.Status)
	}
}

func TestAvailabilityCheckCategoryIsConfiguration(t *testing.T) {
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{})
	if c.Category() != check.CategoryConfiguration {
		t.Errorf("Category = %v, want CategoryConfiguration", c.Category())
	}
}

func TestAvailabilityCheckNotDestructive(t *testing.T) {
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{})
	if c.IsDestructive() {
		t.Errorf("IsDestructive = true; want false (read-only probe)")
	}
}

func TestAvailabilityCheckFixNoopWithoutApplier(t *testing.T) {
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{})
	for _, mode := range []check.FixMode{check.FixModeReadOnly, check.FixModeInteractive, check.FixModeAutoSafe, check.FixModeYes} {
		if err := c.Fix(context.Background(), mode); err != nil {
			t.Errorf("Fix(mode=%v) = %v; want nil (no Applier configured)", mode, err)
		}
	}
}

func TestAvailabilityCheck_FixInvokesCuratedMCPFix(t *testing.T) {
	applier := &recordingApplier{name: "mcp.curated-availability", destructive: false}
	emitter := &recordingFixEmitter{}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		FixApplier: applier,
		Emitter:    emitter,
	})
	if err := c.Fix(context.Background(), check.FixModeYes); err != nil {
		t.Fatalf("Fix returned unexpected error: %v", err)
	}
	if applier.applyCount != 1 {
		t.Errorf("Applier.Apply count = %d; want 1", applier.applyCount)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("audit emit count = %d; want 1", len(emitter.events))
	}
	if emitter.events[0].eventType != "evt.doctor.full.fix.applied" {
		t.Errorf("eventType = %q; want evt.doctor.full.fix.applied", emitter.events[0].eventType)
	}
}

func TestAvailabilityCheckDescriptionNonEmpty(t *testing.T) {
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{})
	if c.Description() == "" {
		t.Errorf("Description empty")
	}
	if len(c.Description()) > 120 {
		t.Errorf("Description = %d chars; want ≤120", len(c.Description()))
	}
}

func TestAvailabilityCheckDetailIncludesRiskTier(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "test-mcp", Tier: 2, PackageManager: "npm", PackageName: "test", MinVersion: "1.0.0", RiskTier: "high"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &stubProber{versions: map[string]string{"npm|test": "1.0.0"}},
	})
	got := c.Run(context.Background())
	if !strings.Contains(got.Detail, "high") {
		t.Errorf("Detail missing risk-tier 'high'; got %q", got.Detail)
	}
}

func TestAvailabilityCheckProberError(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "broken", Tier: 4, PackageManager: "npm", PackageName: "x", MinVersion: "1.0.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &stubProber{errs: map[string]error{"npm|x": errProbeFailed}},
	})
	got := c.Run(context.Background())
	if got.Status != check.StatusFail {
		t.Errorf("Status = %v, want StatusFail (generic prober error)", got.Status)
	}
}

func TestAvailabilityCheckCancellationPropagation(t *testing.T) {
	specs := []mcp.MCPSpec{
		{Name: "slow-a", Tier: 2, PackageManager: "npm", PackageName: "@slow/a", MinVersion: "1.0.0", RiskTier: "low"},
		{Name: "slow-b", Tier: 2, PackageManager: "npm", PackageName: "@slow/b", MinVersion: "1.0.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &slowProber{delay: 50 * time.Millisecond},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	done := make(chan check.DiagnosticResult, 1)
	go func() {
		done <- c.Run(ctx)
	}()
	select {
	case got := <-done:
		if got.Status != check.StatusSkip {
			t.Errorf("Status = %v, want StatusSkip (ctx cancelled mid-probe)", got.Status)
		}
		if got.Message == "" {
			t.Errorf("Message empty; want cancellation surface")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Run blocked >500ms despite ctx cancellation; cancellation contract violated")
	}
}

func TestAvailabilityCheckPreCancelledContext(t *testing.T) {
	probedCount := 0
	specs := []mcp.MCPSpec{
		{Name: "should-not-probe", Tier: 2, PackageManager: "npm", PackageName: "x", MinVersion: "1.0.0", RiskTier: "low"},
	}
	c := mcp.NewAvailabilityCheck(mcp.AvailabilityCheckConfig{
		Specs:  specs,
		Prober: &countingProber{countPtr: &probedCount},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := c.Run(ctx)
	if got.Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (pre-cancelled ctx)", got.Status)
	}
	if probedCount != 0 {
		t.Errorf("probedCount = %d; want 0 (pre-cancel short-circuit)", probedCount)
	}
}

type stubProber struct {
	versions map[string]string
	errs     map[string]error
}

func (s *stubProber) ProbePackage(_ context.Context, manager, pkg string) (string, error) {
	key := manager + "|" + pkg
	if err, ok := s.errs[key]; ok {
		return "", err
	}
	if v, ok := s.versions[key]; ok {
		return v, nil
	}
	return "", mcp.ErrPackageNotFound
}

// slowProber simulates a Prober that ignores ctx and sleeps unconditionally.
// Used to stress the defensive parent ctx.Err() check post-Wait. Real
// Probers MUST honour ctx (exec.CommandContext, http.Request.WithContext);
// this fixture exists exclusively to verify the aggregator's defense.
type slowProber struct {
	delay time.Duration
}

func (s *slowProber) ProbePackage(_ context.Context, _, _ string) (string, error) {
	time.Sleep(s.delay)
	return "1.0.0", nil
}

type countingProber struct {
	countPtr *int
}

func (c *countingProber) ProbePackage(_ context.Context, _, _ string) (string, error) {
	*c.countPtr++
	return "1.0.0", nil
}

var errProbeFailed = errProbeFailedSentinel{}

type errProbeFailedSentinel struct{}

func (errProbeFailedSentinel) Error() string { return "simulated generic probe failure" }

type recordingApplier struct {
	name        string
	destructive bool
	applyErr    error
	applyCount  int
	lastMode    check.FixMode
}

func (r *recordingApplier) Name() string        { return r.name }
func (r *recordingApplier) IsDestructive() bool { return r.destructive }
func (r *recordingApplier) Apply(_ context.Context, mode check.FixMode) error {
	r.applyCount++
	r.lastMode = mode
	return r.applyErr
}

type recordedFixEvent struct {
	eventType string
	payload   []byte
}

type recordingFixEmitter struct {
	events []recordedFixEvent
}

func (e *recordingFixEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	e.events = append(e.events, recordedFixEvent{eventType: eventType, payload: payload})
	return "hash-" + eventType, nil
}
