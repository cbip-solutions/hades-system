// Package aggregator_test exercises the doctor
// aggregator orchestrator contract: parallel-bounded execution,
// per-check timeout, Tessera audit emit, JSON schemaVersion=1.0 output,
// context-cancel partial report.
//
// Per Task F1 (TDD): tests are written FIRST and MUST
// fail (no impl yet); subsequent steps wire aggregator.New + Run.
package aggregator_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestAggregatorRunsAllChecksInParallel(t *testing.T) {
	checks := []check.Check{
		&slowCheck{name: "test.a", delay: 50 * time.Millisecond, status: check.StatusPass},
		&slowCheck{name: "test.b", delay: 50 * time.Millisecond, status: check.StatusPass},
		&slowCheck{name: "test.c", delay: 50 * time.Millisecond, status: check.StatusPass},
		&slowCheck{name: "test.d", delay: 50 * time.Millisecond, status: check.StatusPass},
	}
	agg := aggregator.New(aggregator.Config{
		Checks:       checks,
		MaxParallel:  4,
		CheckTimeout: 5 * time.Second,
		Emitter:      &nopEmitter{},
	})
	start := time.Now()
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 150*time.Millisecond {
		t.Errorf("parallel run elapsed %v; expected <150ms (serial would be 200ms)", elapsed)
	}
	if len(report.Diagnostics) != 4 {
		t.Errorf("len(Diagnostics) = %d, want 4", len(report.Diagnostics))
	}
	if report.PassCount != 4 {
		t.Errorf("PassCount = %d, want 4", report.PassCount)
	}
}

func TestAggregatorPerCheckTimeout(t *testing.T) {
	checks := []check.Check{
		&slowCheck{name: "test.slow", delay: 200 * time.Millisecond, status: check.StatusPass},
	}
	agg := aggregator.New(aggregator.Config{
		Checks:       checks,
		MaxParallel:  1,
		CheckTimeout: 50 * time.Millisecond,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("len(Diagnostics) = %d, want 1", len(report.Diagnostics))
	}
	if report.Diagnostics[0].Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (timeout)", report.Diagnostics[0].Status)
	}
}

func TestAggregatorJSONSchemaVersion(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", status: check.StatusPass}},
		MaxParallel:  1,
		CheckTimeout: 1 * time.Second,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"schemaVersion":"1.0"`) {
		t.Errorf("JSON output missing schemaVersion=1.0: %s", string(body))
	}
}

func TestAggregatorEmitsAuditEvent(t *testing.T) {
	emit := &recordingEmitter{}
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", status: check.StatusPass}},
		MaxParallel:  1,
		CheckTimeout: 1 * time.Second,
		Emitter:      emit,
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := atomic.LoadInt32(&emit.count); got != 1 {
		t.Errorf("emitted events = %d, want 1", got)
	}
	if emit.lastType != aggregator.AuditEventType {
		t.Errorf("event type = %q, want %q", emit.lastType, aggregator.AuditEventType)
	}
	if report.AuditEventHash == "" {
		t.Errorf("AuditEventHash empty; want recorded hash")
	}
}

func TestAggregatorAbortsOnContextCancel(t *testing.T) {
	checks := []check.Check{
		&slowCheck{name: "test.a", delay: 200 * time.Millisecond, status: check.StatusPass},
	}
	agg := aggregator.New(aggregator.Config{
		Checks:       checks,
		MaxParallel:  1,
		CheckTimeout: 5 * time.Second,
		Emitter:      &nopEmitter{},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	report, err := agg.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.DeadlineExceeded or Canceled", err)
	}
	if report == nil {
		t.Errorf("report = nil; want partial report")
	}
}

func TestAggregatorDefaultMaxParallel(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", status: check.StatusPass}},
		MaxParallel:  0,
		CheckTimeout: 1 * time.Second,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(report.Diagnostics) != 1 {
		t.Errorf("len(Diagnostics) = %d, want 1", len(report.Diagnostics))
	}
}

func TestAggregatorDefaultCheckTimeout(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", delay: 1 * time.Millisecond, status: check.StatusPass}},
		MaxParallel:  1,
		CheckTimeout: 0,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if report.Diagnostics[0].Status != check.StatusPass {
		t.Errorf("Status = %v, want StatusPass", report.Diagnostics[0].Status)
	}
}

func TestAggregatorTallyAllStatuses(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks: []check.Check{
			&slowCheck{name: "p", status: check.StatusPass},
			&slowCheck{name: "w", status: check.StatusWarn},
			&slowCheck{name: "f", status: check.StatusFail},
			&slowCheck{name: "s", status: check.StatusSkip},
		},
		MaxParallel:  4,
		CheckTimeout: 1 * time.Second,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if report.PassCount != 1 || report.WarnCount != 1 || report.FailCount != 1 || report.SkipCount != 1 {
		t.Errorf("counts wrong: pass=%d warn=%d fail=%d skip=%d", report.PassCount, report.WarnCount, report.FailCount, report.SkipCount)
	}
}

func TestAggregatorReportDurationRecorded(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks: []check.Check{
			&slowCheck{name: "test.a", delay: 5 * time.Millisecond, status: check.StatusPass},
		},
		MaxParallel:  1,
		CheckTimeout: 1 * time.Second,
		Emitter:      &nopEmitter{},
	})
	report, _ := agg.Run(context.Background())
	if report.Diagnostics[0].DurationMs < 0 {
		t.Errorf("DurationMs = %d; want ≥0", report.Diagnostics[0].DurationMs)
	}
}

func TestAggregatorAuditEmitterFailure(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", status: check.StatusPass}},
		MaxParallel:  1,
		CheckTimeout: 1 * time.Second,
		Emitter:      &errorEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v; aggregator should tolerate emitter failure", err)
	}
	if report.AuditEventHash != "" {
		t.Errorf("AuditEventHash = %q; want empty on emitter failure", report.AuditEventHash)
	}
}

func TestAggregatorNilEmitterTolerated(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       []check.Check{&slowCheck{name: "test.a", status: check.StatusPass}},
		MaxParallel:  1,
		CheckTimeout: 1 * time.Second,
		Emitter:      nil,
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if report == nil {
		t.Errorf("report nil")
	}
}

func TestAggregatorEmptyChecks(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks:       nil,
		MaxParallel:  4,
		CheckTimeout: 1 * time.Second,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("len(Diagnostics) = %d, want 0", len(report.Diagnostics))
	}
}

// TestAggregatorDefensiveSkipOverrideForNoCtxCheck asserts the defensive
// post-Run timeout override in runWithTimeout (aggregator.go:213-219):
// when a Check ignores ctx and returns non-Skip status AFTER the per-check
// deadline has elapsed, the aggregator MUST override the result to
// StatusSkip with a "context deadline exceeded" message.
//
// This is the defense-in-depth contract documented in the Check interface
// godoc (check.go:47-48: "Run(ctx) returns the DiagnosticResult; honors
// ctx.Done()..."). All production checks honour ctx, but the
// aggregator must guarantee the post-Run override fires if any check
// implementation drifts. Without this test, the override branch is
// uncovered: existing TestAggregatorPerCheckTimeout uses slowCheck which
// honours ctx via <-ctx.Done() and returns StatusSkip itself, so the
// override path (line 213-219 `if ctx.Err() != nil && res.Status != Skip`)
// never fires.
func TestAggregatorDefensiveSkipOverrideForNoCtxCheck(t *testing.T) {
	agg := aggregator.New(aggregator.Config{
		Checks: []check.Check{
			&noCtxCheck{name: "test.no-ctx", delay: 100 * time.Millisecond},
		},
		MaxParallel:  1,
		CheckTimeout: 20 * time.Millisecond,
		Emitter:      &nopEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("len(Diagnostics) = %d, want 1", len(report.Diagnostics))
	}
	if report.Diagnostics[0].Status != check.StatusSkip {
		t.Errorf("Status = %v, want StatusSkip (defensive override forced by per-check timeout)", report.Diagnostics[0].Status)
	}

	if report.Diagnostics[0].Message == "" {
		t.Errorf("Message empty; want \"context deadline exceeded\" or similar from defensive override")
	}
}

// noCtxCheck is a deliberately-broken Check that IGNORES ctx and always
// returns StatusPass after a fixed delay. Exercises the aggregator's
// defense-in-depth contract that the per-check timeout deadline forces a
// Skip override even when the inner Check ignores ctx.Done().
//
// Production Checks MUST honour ctx (see check.go godoc). This type is
// strictly a test fixture for the defensive override; no production
// equivalent exists.
type noCtxCheck struct {
	name  string
	delay time.Duration
}

func (n *noCtxCheck) Name() string                                      { return n.name }
func (n *noCtxCheck) Category() check.Category                          { return check.CategoryRuntime }
func (n *noCtxCheck) Description() string                               { return "no-ctx test check" }
func (n *noCtxCheck) IsDestructive() bool                               { return false }
func (n *noCtxCheck) Fix(ctx context.Context, mode check.FixMode) error { return nil }
func (n *noCtxCheck) Run(_ context.Context) check.DiagnosticResult {

	time.Sleep(n.delay)
	return check.DiagnosticResult{Name: n.name, Status: check.StatusPass}
}

type slowCheck struct {
	name   string
	delay  time.Duration
	status check.Status
}

func (s *slowCheck) Name() string                                      { return s.name }
func (s *slowCheck) Category() check.Category                          { return check.CategoryRuntime }
func (s *slowCheck) Description() string                               { return "slow test check" }
func (s *slowCheck) IsDestructive() bool                               { return false }
func (s *slowCheck) Fix(ctx context.Context, mode check.FixMode) error { return nil }
func (s *slowCheck) Run(ctx context.Context) check.DiagnosticResult {
	if s.delay <= 0 {
		return check.DiagnosticResult{Name: s.name, Status: s.status}
	}
	select {
	case <-ctx.Done():
		return check.DiagnosticResult{Name: s.name, Status: check.StatusSkip, Message: "ctx cancelled"}
	case <-time.After(s.delay):
		return check.DiagnosticResult{Name: s.name, Status: s.status}
	}
}

type nopEmitter struct{}

func (n *nopEmitter) Emit(ctx context.Context, eventType string, payload []byte) (string, error) {
	return "audit-hash-stub", nil
}

type recordingEmitter struct {
	count    int32
	lastType string
}

func (r *recordingEmitter) Emit(ctx context.Context, eventType string, payload []byte) (string, error) {
	atomic.AddInt32(&r.count, 1)
	r.lastType = eventType
	return "audit-hash-recorded", nil
}

type errorEmitter struct{}

func (e *errorEmitter) Emit(ctx context.Context, eventType string, payload []byte) (string, error) {
	return "", errors.New("simulated emitter failure")
}
