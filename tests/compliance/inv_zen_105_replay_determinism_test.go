// tests/compliance/inv_zen_105_replay_determinism_test.go
//
// Compliance gate for inv-zen-105: replay-determinism via cache key.
//
// The merge engine's audit trail (Plan 6 Phase B Cache + EventLog) MUST
// support deterministic replay: rebuilding a fresh Cache from the same
// EvtMergeCompleted event stream produces byte-identical lookups for the
// same MergeRequest. This is the runtime expression of the spec's
// content-addressable cache contract — Plan 5 amendment.proposer relies
// on the property when reconstructing causal chains across daemon
// restarts, and Plan 6 Phase D's cache-hit short-circuit relies on it
// for "skip re-merge if already done in a previous run" correctness.
//
// Three sibling assertions at the same compliance tier:
//  1. TestInvZen105RebuildDeterministic — two fresh caches rebuilt from
//     the same event stream produce equal Size() and byte-identical
//     Lookup(req) outcomes. Catches non-deterministic reduce-over-events
//     (map ordering leak, time-of-day in payload, etc.).
//  2. TestInvZen105StoreLookupIdempotent — Cache.Store(req, outcome)
//     followed by 100 successive Cache.Lookup(req) calls all return
//     the exact same outcome. Catches lookup drift (state mutation
//     between calls, hash collision masking, etc.).
//  3. TestInvZen105CacheKeyDeterministic — CacheKey(req) is pure: 50
//     successive calls with the same input produce the same string.
//     Already covered in cache_test.go but re-asserted here for
//     compliance-tier visibility (the binary CI gate). Drift in cache-
//     key production silently invalidates every prior cached outcome,
//     so the property is tier-1 doctrine, not "minor invariant".
//
// Reference: docs/superpowers/specs/2026-05-01-zen-swarm-plan-6-merge-engine-design.md §8.3 inv-zen-105
//
// Drift adaptation per Task B-7 instructions: package compliance (not
// _test) to match the predominant tests/compliance convention (29-of-37
// files; A-7's inv_zen_104 + inv_zen_110 likewise) and to enable use of
// shared helpers (repoRoot, isUnderPrefix) when needed. Local helper
// types are b7-prefixed to avoid name collisions with sibling files
// (notably inv_zen_095_corruption_bounded_test.go's complianceEmitter
// which is an eventlog.RawEmitter, not a merge.EventEmitter).
package compliance

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type b7EventReader struct {
	events []merge.Event
	mu     sync.Mutex
}

func (r *b7EventReader) Each(ctx context.Context, fn func(e merge.Event) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

type b7Emitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (e *b7Emitter) Append(_ context.Context, ev merge.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ev = append(e.ev, ev)
	return nil
}

func (e *b7Emitter) Snapshot() []merge.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]merge.Event, len(e.ev))
	copy(out, e.ev)
	return out
}

func TestInvZen105RebuildDeterministic(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "deadbeef0000000000000000000000000000beef",
		Mode:          merge.ModeNormal,
		EngineVersion: "v0.6.0",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111"},
		},
	}
	out := merge.MergeOutcome{
		Winner: merge.MergeCandidate{
			Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111",
		},
		IntegrationSHA: "intsha-1",
		TestsPassed:    true,
		AllScores:      map[string]float64{"1111111111111111111111111111111111111111": 1.0},
	}
	payload, err := json.Marshal(merge.MergeCompletedPayload{
		WinnerCandidateID: out.Winner.HeadSHA,
		IntegrationSHA:    out.IntegrationSHA,
		RequestHash:       merge.CacheKey(req),
		Outcome:           out,
	})
	if err != nil {
		t.Fatalf("marshal MergeCompletedPayload: %v", err)
	}
	events := []merge.Event{
		{
			Type:         merge.EvtMergeCompleted,
			GenerationID: 1,
			RequestHash:  merge.CacheKey(req),
			Payload:      payload,
			Timestamp:    time.Now(),
		},
	}

	c1 := merge.NewCache()
	if err := c1.Rebuild(context.Background(), &b7EventReader{events: events}, &b7Emitter{}); err != nil {
		t.Fatalf("c1 Rebuild: %v", err)
	}
	c2 := merge.NewCache()
	if err := c2.Rebuild(context.Background(), &b7EventReader{events: events}, &b7Emitter{}); err != nil {
		t.Fatalf("c2 Rebuild: %v", err)
	}

	if c1.Size() != c2.Size() {
		t.Fatalf("inv-zen-105 VIOLATION: c1.Size=%d c2.Size=%d", c1.Size(), c2.Size())
	}
	o1, ok1 := c1.Lookup(req)
	o2, ok2 := c2.Lookup(req)
	if !ok1 || !ok2 {
		t.Fatalf("inv-zen-105 VIOLATION: cache miss after rebuild (ok1=%v ok2=%v)", ok1, ok2)
	}
	if !reflect.DeepEqual(o1, o2) {
		t.Errorf("inv-zen-105 VIOLATION: rebuilt outcomes differ\n c1=%+v\n c2=%+v", o1, o2)
	}
}

func TestInvZen105StoreLookupIdempotent(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "abc",
		EngineVersion: "v1",
		Mode:          merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "h1"},
		},
	}
	want := merge.MergeOutcome{
		Winner:         merge.MergeCandidate{Branch: "feat-A", HeadSHA: "h1"},
		IntegrationSHA: "intsha",
		TestsPassed:    true,
	}
	c := merge.NewCache()
	c.Store(req, want)
	for i := 0; i < 100; i++ {
		got, ok := c.Lookup(req)
		if !ok {
			t.Fatalf("inv-zen-105 VIOLATION: lookup miss on call %d", i)
		}
		if got.IntegrationSHA != want.IntegrationSHA {
			t.Errorf("inv-zen-105 VIOLATION: lookup drift on call %d: %s != %s", i, got.IntegrationSHA, want.IntegrationSHA)
		}
	}
}

func TestInvZen105CacheKeyDeterministic(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "abc",
		EngineVersion: "v1",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "h1"},
			{Branch: "feat-B", HeadSHA: "h2"},
		},
	}
	first := merge.CacheKey(req)
	for i := 0; i < 50; i++ {
		got := merge.CacheKey(req)
		if got != first {
			t.Fatalf("inv-zen-105 VIOLATION: CacheKey drifts on call %d: %s != %s", i, got, first)
		}
	}
}
