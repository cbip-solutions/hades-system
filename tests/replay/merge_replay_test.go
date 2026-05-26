// tests/replay/merge_replay_test.go (Plan 6 Phase E Task E-3).
//
// Replay-tier validation of inv-zen-105 (replay-determinism) end-to-end at
// the merge.Cache.Rebuild boundary. Lives behind //go:build replay so the
// default `go test ./...` does not run it; CI invokes `go test -tags=replay`.
//
// Coverage:
//
//  1. TestReplay_TwoRebuildsIdenticalCache — same captured event stream
//     rebuilt twice produces identical caches (size + per-request lookup).
//     Direct inv-zen-105 assertion at the cache-state level.
//
//  2. TestReplay_RebuildEmitsErrorOnMalformedPayload — Drift-E enforcement:
//     a malformed EvtMergeCompleted payload triggers Cache.Rebuild's failure
//     path, which MUST emit EvtMergeCacheRebuilt with non-empty RebuildError
//     (NOT a separate EvtMergeCacheRebuildFailed event). Drift-E pinned the
//     EventType taxonomy at 16 by routing rebuild failures through the
//     payload's RebuildError field rather than a 17th event type.
//
//  3. TestReplay_CacheKeyDeterministic — 100 calls to CacheKey on the same
//     MergeRequest produce identical hashes; the candidate ordering is
//     intentionally reverse-sorted in the input to exercise CacheKey's
//     internal sort step (Q5 A: candidate SET, not sequence, is the key).
//
// The replayReader + replayEmitter helpers are local to this file (small,
// single-purpose; no value in promoting them to replay_helpers.go which is
// scoped to the eventlog-based JSONL fixture loader for the orchestrator
// suite).
//
//go:build replay
// +build replay

package replay_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type replayReader struct {
	events []merge.Event
	mu     sync.Mutex
}

func (r *replayReader) Each(ctx context.Context, fn func(e merge.Event) error) error {
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

type replayEmitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (e *replayEmitter) Append(_ context.Context, ev merge.Event) error {
	e.mu.Lock()
	e.ev = append(e.ev, ev)
	e.mu.Unlock()
	return nil
}

func (e *replayEmitter) Snapshot() []merge.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]merge.Event{}, e.ev...)
}

func TestReplay_TwoRebuildsIdenticalCache(t *testing.T) {
	reqs := []merge.MergeRequest{
		{TargetBranch: "main", BaseSHA: "b1", EngineVersion: "v1", Candidates: []merge.MergeCandidate{{HeadSHA: "h1"}}},
		{TargetBranch: "dev", BaseSHA: "b2", EngineVersion: "v1", Candidates: []merge.MergeCandidate{{HeadSHA: "h2"}}},
		{TargetBranch: "main", BaseSHA: "b3", EngineVersion: "v1", Candidates: []merge.MergeCandidate{{HeadSHA: "h3"}}},
	}
	events := make([]merge.Event, len(reqs))
	for i, req := range reqs {
		out := merge.MergeOutcome{
			Winner:         req.Candidates[0],
			IntegrationSHA: "int-" + req.Candidates[0].HeadSHA,
			TestsPassed:    true,
		}
		p, err := json.Marshal(merge.MergeCompletedPayload{
			WinnerCandidateID: out.Winner.HeadSHA,
			IntegrationSHA:    out.IntegrationSHA,
			RequestHash:       merge.CacheKey(req),
			Outcome:           out,
		})
		if err != nil {
			t.Fatalf("marshal payload[%d]: %v", i, err)
		}
		events[i] = merge.Event{
			Type:         merge.EvtMergeCompleted,
			GenerationID: int64(i + 1),
			RequestHash:  merge.CacheKey(req),
			Payload:      p,
			Timestamp:    time.Now(),
		}
	}

	c1 := merge.NewCache()
	if err := c1.Rebuild(context.Background(), &replayReader{events: events}, &replayEmitter{}); err != nil {
		t.Fatalf("c1 Rebuild: %v", err)
	}
	c2 := merge.NewCache()
	if err := c2.Rebuild(context.Background(), &replayReader{events: events}, &replayEmitter{}); err != nil {
		t.Fatalf("c2 Rebuild: %v", err)
	}
	if c1.Size() != c2.Size() {
		t.Fatalf("inv-zen-105 VIOLATION: sizes differ (%d vs %d)", c1.Size(), c2.Size())
	}
	if c1.Size() != len(reqs) {
		t.Fatalf("expected %d cache entries after rebuild, got %d", len(reqs), c1.Size())
	}
	for _, req := range reqs {
		o1, ok1 := c1.Lookup(req)
		o2, ok2 := c2.Lookup(req)
		if !ok1 || !ok2 {
			t.Errorf("rebuild miss for %s/%s (ok1=%v ok2=%v)", req.TargetBranch, req.BaseSHA, ok1, ok2)
			continue
		}
		if !reflect.DeepEqual(o1, o2) {
			t.Errorf("inv-zen-105 VIOLATION: rebuilt outcomes differ for %s/%s\n  c1=%+v\n  c2=%+v",
				req.TargetBranch, req.BaseSHA, o1, o2)
		}
	}
}

func TestReplay_RebuildEmitsErrorOnMalformedPayload(t *testing.T) {
	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, GenerationID: 1, Payload: []byte("malformed{{{")},
	}
	c := merge.NewCache()
	em := &replayEmitter{}
	err := c.Rebuild(context.Background(), &replayReader{events: events}, em)
	if err == nil {
		t.Fatal("expected error on malformed payload")
	}

	saw := false
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeCacheRebuilt {
			saw = true
			var p merge.MergeCacheRebuiltPayload
			if uerr := json.Unmarshal(e.Payload, &p); uerr != nil {
				t.Fatalf("decode MergeCacheRebuiltPayload: %v (raw=%s)", uerr, string(e.Payload))
			}
			if p.RebuildError == "" {
				t.Error("inv-zen-105/Drift-E: RebuildError empty on failure")
			}
		}

		if e.Type.String() == "MergeCacheRebuildFailed" {
			t.Errorf("Drift-E VIOLATION: forbidden event type emitted on rebuild failure: %v", e.Type)
		}
	}
	if !saw {
		t.Error("EvtMergeCacheRebuilt not emitted on rebuild failure")
	}
}

func TestReplay_CacheKeyDeterministic(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "abc",
		EngineVersion: "v1",
		Candidates:    []merge.MergeCandidate{{HeadSHA: "h2"}, {HeadSHA: "h1"}},
	}
	first := merge.CacheKey(req)
	for i := 0; i < 100; i++ {
		got := merge.CacheKey(req)
		if got != first {
			t.Fatalf("CacheKey drift on call %d: %s != %s", i, got, first)
		}
	}
}
