// go:build chaos

// Package chaos — p13_daemon_disconnect_aggregator_test.go (
// F-imp IMPORTANT 7).
//
// Chaos: the doctor full aggregator MUST gracefully handle daemon
// disconnection mid-run. Per spec §3.4 + plan F10 line 7243-7247: the
// aggregator is best-effort emit (daemon-down → buffer to
// ~/.local/state/zen-swarm/audit-pending.jsonl, never block the
// diagnostic surface).
//
// Build tag `chaos` excludes this file from default CI; opt-in via
// `make test-chaos` or `go test -tags=chaos./tests/chaos/...`.
package chaos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type disconnectingEmitter struct{}

func (disconnectingEmitter) Emit(_ context.Context, _ string, _ []byte) (string, error) {
	return "", errors.New("daemon disconnected mid-emit")
}

type stubCheck struct {
	name string
}

func (s *stubCheck) Name() string                                 { return s.name }
func (s *stubCheck) Category() check.Category                     { return check.CategoryPreflight }
func (s *stubCheck) Description() string                          { return "stub" }
func (s *stubCheck) IsDestructive() bool                          { return false }
func (s *stubCheck) Fix(_ context.Context, _ check.FixMode) error { return nil }
func (s *stubCheck) Run(_ context.Context) check.DiagnosticResult {
	return check.DiagnosticResult{Name: s.name, Status: check.StatusPass}
}

func TestChaos_DaemonDisconnectMidAggregator(t *testing.T) {
	t.Parallel()
	checks := []check.Check{
		&stubCheck{name: "a"},
		&stubCheck{name: "b"},
		&stubCheck{name: "c"},
	}
	agg := aggregator.New(aggregator.Config{
		Checks:       checks,
		CheckTimeout: 5 * time.Second,
		Emitter:      disconnectingEmitter{},
	})
	report, err := agg.Run(context.Background())
	if err != nil {
		t.Fatalf("aggregator.Run with daemon-disconnect emitter: %v; report should still return clean", err)
	}
	if report == nil {
		t.Fatal("report nil; aggregator MUST surface diagnostic output even when audit emit fails")
	}
	if len(report.Diagnostics) != 3 {
		t.Errorf("diagnostic count = %d; want 3 (one per check, audit failure should not skip checks)",
			len(report.Diagnostics))
	}

	if report.AuditEventHash != "" {
		t.Errorf("AuditEventHash = %q; want empty (emit failed)", report.AuditEventHash)
	}
}
