// internal/daemon/orchestrator/tier_health_sink_test.go
//
// Tests for the TierHealthSink interface + the orchestrator-local
// TierHealthSampleRow mirror.
//
// Package orchestrator_test — external access only, matching the rest of
// the orchestrator test files. fakeTierHealthSink is defined here in the
// external test package so that recovery_scheduler_test.go (same package)
// can reuse it for the probe-write-path test.
//
// Boundary: this file imports stdlib + internal/providers
// only. It MUST NOT import internal/store — TierHealthSampleRow is the
// orchestrator-local mirror; the dispatcheradapter (Task 14) bridges to
// store.TierHealthSampleRow.

package orchestrator_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

type fakeTierHealthSink struct {
	mu      sync.Mutex
	samples []orchestrator.TierHealthSampleRow
}

func (f *fakeTierHealthSink) RecordHealthSample(row orchestrator.TierHealthSampleRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples = append(f.samples, row)
	return nil
}

func (f *fakeTierHealthSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.samples)
}

func TestTierHealthSink_Contract(t *testing.T) {
	var _ orchestrator.TierHealthSink = (*fakeTierHealthSink)(nil)
	f := &fakeTierHealthSink{}
	err := f.RecordHealthSample(orchestrator.TierHealthSampleRow{
		TS: time.Now(), Provider: "gemini-flash", Tier: "gemini", Success: true, LatencyMS: 88,
	})
	if err != nil {
		t.Fatalf("RecordHealthSample: %v", err)
	}
	if f.count() != 1 {
		t.Errorf("recorded %d samples, want 1", f.count())
	}
}
