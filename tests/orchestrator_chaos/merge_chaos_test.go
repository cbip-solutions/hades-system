// tests/orchestrator_chaos/merge_chaos_test.go (Plan 6 Phase E Task E-5).
//
// Orchestrator-chaos tier validates the recovery flow per spec §3.6 — daemon
// SIGKILL mid-merge → restart → cache rebuilt from eventlog. For Phase E we
// test the merge-side contract abstractly: daemon-restart is simulated by
// reconstructing a fresh merge.Cache and invoking Cache.Rebuild on the
// captured pre-SIGKILL event stream. The full daemon-subprocess analog ships
// in orchestrator_chaos_test.go (Plan 5 Phase O Task O-7); this file focuses
// on the merge-package contract decoupled from the daemon transport.
//
//go:build orchestrator_chaos
// +build orchestrator_chaos

package orchestrator_chaos_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type ocReader struct {
	events []merge.Event
	mu     sync.Mutex
}

func (r *ocReader) Each(ctx context.Context, fn func(e merge.Event) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

type ocEmitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (e *ocEmitter) Append(_ context.Context, ev merge.Event) error {
	e.mu.Lock()
	e.ev = append(e.ev, ev)
	e.mu.Unlock()
	return nil
}

func (e *ocEmitter) Snapshot() []merge.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]merge.Event{}, e.ev...)
}

func TestOrchestratorChaos_RestartReconstitutesCache(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "deadbeef",
		Mode:          merge.ModeNormal,
		EngineVersion: "v0.6.0",
		Candidates:    []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h1"}},
	}
	out := merge.MergeOutcome{Winner: req.Candidates[0], IntegrationSHA: "ints", TestsPassed: true}
	completedPayload, _ := json.Marshal(merge.MergeCompletedPayload{
		WinnerCandidateID: out.Winner.HeadSHA,
		IntegrationSHA:    out.IntegrationSHA,
		RequestHash:       merge.CacheKey(req),
		Outcome:           out,
	})
	preSigkillEvents := []merge.Event{
		{Type: merge.EvtMergeStartedWithMode, GenerationID: 1, RequestHash: merge.CacheKey(req), Timestamp: time.Now()},
		{Type: merge.EvtBaselineComplete, GenerationID: 1, Timestamp: time.Now()},
		{Type: merge.EvtCandidateComplete, GenerationID: 1, Timestamp: time.Now()},
		{Type: merge.EvtScoringComplete, GenerationID: 1, Timestamp: time.Now()},
		{Type: merge.EvtMergeCompleted, GenerationID: 1, RequestHash: merge.CacheKey(req), Payload: completedPayload, Timestamp: time.Now()},
	}

	c := merge.NewCache()
	em := &ocEmitter{}
	if err := c.Rebuild(context.Background(), &ocReader{events: preSigkillEvents}, em); err != nil {
		t.Fatalf("Rebuild post-restart: %v", err)
	}
	cached, ok := c.Lookup(req)
	if !ok {
		t.Fatal("post-restart Cache miss for completed merge")
	}
	if cached.IntegrationSHA != out.IntegrationSHA {
		t.Errorf("IntegrationSHA = %s want %s", cached.IntegrationSHA, out.IntegrationSHA)
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeCacheRebuilt {
			saw = true
		}
	}
	if !saw {
		t.Error("EvtMergeCacheRebuilt not emitted post-restart")
	}
}

func TestOrchestratorChaos_RebuildIsIdempotent(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "abc",
		EngineVersion: "v1",
		Candidates:    []merge.MergeCandidate{{HeadSHA: "h1"}},
	}
	out := merge.MergeOutcome{Winner: req.Candidates[0], IntegrationSHA: "ints"}
	p, _ := json.Marshal(merge.MergeCompletedPayload{
		WinnerCandidateID: "h1", IntegrationSHA: "ints",
		RequestHash: merge.CacheKey(req), Outcome: out,
	})
	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, RequestHash: merge.CacheKey(req), Payload: p},
	}
	for i := 0; i < 3; i++ {
		c := merge.NewCache()
		_ = c.Rebuild(context.Background(), &ocReader{events: events}, &ocEmitter{})
		got, ok := c.Lookup(req)
		if !ok || got.IntegrationSHA != "ints" {
			t.Errorf("restart #%d: lookup mismatch (ok=%v sha=%s)", i, ok, got.IntegrationSHA)
		}
	}
}

func TestOrchestratorChaos_WorktreeGCObservation(t *testing.T) {
	t.Skip("worktree orphan GC is Plan 5 worktreepool.PruneOrphans; Plan 5 Phase Q owns")
}
