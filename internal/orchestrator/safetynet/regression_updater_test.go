// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestRegressionUpdaterRecordsApplyFixSuccessIntoSubstrateHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writer := &regressionUpdaterWriter{}
	log := eventlog.NewMemory(clock.Real{})
	regression := NewRegression(writer, regressionUpdaterNoopEmitter{}, 0.8)
	updater, err := NewRegressionUpdater(RegressionUpdaterConfig{
		Regression: regression,
		EventLog:   log,
		Clock:      clock.Real{},
	})
	if err != nil {
		t.Fatalf("NewRegressionUpdater: %v", err)
	}
	go updater.Run(ctx)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, _ = log.Append(context.Background(), eventlog.Event{
			Type:      eventlog.EvtApplyFixSucceeded,
			SessionID: "session-1",
			ProjectID: "project-1",
			Payload: map[string]any{
				"commit_sha": "abc123",
				"branch":     "worker/one",
				"fix_id":     "fix-1",
			},
		})
		if recs := writer.snapshot(); len(recs) > 0 {
			got := recs[0]
			if got.CommitSHA != "abc123" {
				t.Fatalf("CommitSHA = %q, want abc123", got.CommitSHA)
			}
			if got.AuthoredBy != "substrate" {
				t.Fatalf("AuthoredBy = %q, want substrate", got.AuthoredBy)
			}
			if got.TestTotal != 1 || got.TestPassed != 1 || got.TestPassRate != 1 {
				t.Fatalf("test summary = total %d passed %d rate %.2f, want 1/1/1.0", got.TestTotal, got.TestPassed, got.TestPassRate)
			}
			if !got.DoctrineLintPass {
				t.Fatal("DoctrineLintPass = false, want true for successful apply default")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("regression updater did not persist substrate health record")
}

type regressionUpdaterWriter struct {
	mu   sync.Mutex
	recs []HealthRecord
}

func (w *regressionUpdaterWriter) Insert(_ context.Context, r HealthRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recs = append(w.recs, r)
	return nil
}

func (w *regressionUpdaterWriter) Recent(context.Context, string, time.Time) ([]HealthRecord, error) {
	return nil, nil
}

func (w *regressionUpdaterWriter) snapshot() []HealthRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]HealthRecord, len(w.recs))
	copy(out, w.recs)
	return out
}

type regressionUpdaterNoopEmitter struct{}

func (regressionUpdaterNoopEmitter) Emit(context.Context, Event) error { return nil }
