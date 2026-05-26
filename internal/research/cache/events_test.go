//go:build cgo
// +build cgo

package cache

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type capturedEvent struct {
	eventType string
	payload   []byte
}

type fakeSink struct {
	events []capturedEvent
}

func (f *fakeSink) Emit(_ context.Context, eventType string, payload []byte) error {
	f.events = append(f.events, capturedEvent{eventType: eventType, payload: payload})
	return nil
}

type errorSink struct {
	err error
}

func (e *errorSink) Emit(_ context.Context, _ string, _ []byte) error {
	return e.err
}

func TestEmitDispatchInitiated(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	err := EmitDispatchInitiated(ctx, sink, 42, "proj-1", "sess-abc", "qhash-xyz", at)
	if err != nil {
		t.Fatalf("EmitDispatchInitiated returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchDispatchInitiated {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchDispatchInitiated)
	}

	var p DispatchInitiatedPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal DispatchInitiatedPayload: %v", err)
	}
	if p.DispatchID != 42 {
		t.Errorf("DispatchID = %d; want 42", p.DispatchID)
	}
	if p.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q; want %q", p.ProjectID, "proj-1")
	}
	if p.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q; want %q", p.SessionID, "sess-abc")
	}
	if p.QueryHash != "qhash-xyz" {
		t.Errorf("QueryHash = %q; want %q", p.QueryHash, "qhash-xyz")
	}
	if !p.At.Equal(at) {
		t.Errorf("At = %v; want %v", p.At, at)
	}
}

func TestEmitCacheHitExact(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 13, 0, 0, 0, time.UTC)

	err := EmitCacheHitExact(ctx, sink, 10, "proj-2", "sess-def", "qhash-exact", FreshnessFresh, at)
	if err != nil {
		t.Fatalf("EmitCacheHitExact returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchCacheHitExact {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchCacheHitExact)
	}

	var p CacheHitPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal CacheHitPayload: %v", err)
	}
	if p.DispatchID != 10 {
		t.Errorf("DispatchID = %d; want 10", p.DispatchID)
	}
	if p.HitReason != CacheHitExact {
		t.Errorf("HitReason = %q; want %q", p.HitReason, CacheHitExact)
	}
	if p.Freshness != FreshnessFresh {
		t.Errorf("Freshness = %q; want %q", p.Freshness, FreshnessFresh)
	}
	if !p.At.Equal(at) {
		t.Errorf("At = %v; want %v", p.At, at)
	}
}

func TestEmitCacheHitSemantic(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 14, 0, 0, 0, time.UTC)

	err := EmitCacheHitSemantic(ctx, sink, 20, "proj-3", "sess-ghi", "qhash-sem", FreshnessStale, at)
	if err != nil {
		t.Fatalf("EmitCacheHitSemantic returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchCacheHitSemantic {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchCacheHitSemantic)
	}

	var p CacheHitPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal CacheHitPayload: %v", err)
	}
	if p.HitReason != CacheHitSemantic {
		t.Errorf("HitReason = %q; want %q", p.HitReason, CacheHitSemantic)
	}
	if p.Freshness != FreshnessStale {
		t.Errorf("Freshness = %q; want %q", p.Freshness, FreshnessStale)
	}
}

func TestEmitRevalidatedFresh(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 15, 0, 0, 0, time.UTC)

	err := EmitRevalidatedFresh(ctx, sink, 99, "https://example.com/doc", 304,
		`"etag-abc"`, "Mon, 1 Jan 2026 00:00:00 GMT",
		"sha256oldhash", "sha256oldhash", at)
	if err != nil {
		t.Fatalf("EmitRevalidatedFresh returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchCacheRevalidatedFresh {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchCacheRevalidatedFresh)
	}

	var p RevalidatedPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal RevalidatedPayload: %v", err)
	}
	if p.FindingID != 99 {
		t.Errorf("FindingID = %d; want 99", p.FindingID)
	}
	if p.SourceURL != "https://example.com/doc" {
		t.Errorf("SourceURL = %q; want %q", p.SourceURL, "https://example.com/doc")
	}
	if p.HTTPStatus != 304 {
		t.Errorf("HTTPStatus = %d; want 304", p.HTTPStatus)
	}
	if !p.At.Equal(at) {
		t.Errorf("At = %v; want %v", p.At, at)
	}
}

func TestEmitRevalidatedStaleRefetched(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 16, 0, 0, 0, time.UTC)

	err := EmitRevalidatedStaleRefetched(ctx, sink, 77, "https://example.com/article", 200,
		`"etag-new"`, "Tue, 2 Jan 2026 00:00:00 GMT",
		"sha256oldhash", "sha256newhash", at)
	if err != nil {
		t.Fatalf("EmitRevalidatedStaleRefetched returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchCacheRevalidatedStaleRefetched {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchCacheRevalidatedStaleRefetched)
	}

	var p RevalidatedPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal RevalidatedPayload: %v", err)
	}
	if p.OldContentHash != "sha256oldhash" {
		t.Errorf("OldContentHash = %q; want %q", p.OldContentHash, "sha256oldhash")
	}
	if p.NewContentHash != "sha256newhash" {
		t.Errorf("NewContentHash = %q; want %q", p.NewContentHash, "sha256newhash")
	}
}

func TestEmitFindingsReturned(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()
	at := time.Date(2026, 5, 9, 17, 0, 0, 0, time.UTC)

	err := EmitFindingsReturned(ctx, sink, 55, "proj-4", "sess-jkl", "qhash-ret", 7,
		CacheHitExact, FreshnessFresh, at)
	if err != nil {
		t.Fatalf("EmitFindingsReturned returned unexpected error: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.eventType != EventResearchFindingsReturned {
		t.Errorf("event type = %q; want %q", ev.eventType, EventResearchFindingsReturned)
	}

	var p FindingsReturnedPayload
	if err := json.Unmarshal(ev.payload, &p); err != nil {
		t.Fatalf("cannot unmarshal FindingsReturnedPayload: %v", err)
	}
	if p.DispatchID != 55 {
		t.Errorf("DispatchID = %d; want 55", p.DispatchID)
	}
	if p.FindingCount != 7 {
		t.Errorf("FindingCount = %d; want 7", p.FindingCount)
	}
	if p.HitReason != CacheHitExact {
		t.Errorf("HitReason = %q; want %q", p.HitReason, CacheHitExact)
	}
	if p.Freshness != FreshnessFresh {
		t.Errorf("Freshness = %q; want %q", p.Freshness, FreshnessFresh)
	}
}

func TestEmitJSONMarshalError(t *testing.T) {
	sink := &fakeSink{}
	ctx := context.Background()

	ch := make(chan int)
	err := emitJSON(ctx, sink, "test.event", ch)
	if err == nil {
		t.Fatal("expected error from emitJSON with non-marshallable payload, got nil")
	}

	if len(sink.events) != 0 {
		t.Errorf("sink.Emit should not have been called on marshal failure; got %d calls", len(sink.events))
	}
}

func TestEmitErrorsPropagated(t *testing.T) {
	sentinel := errors.New("sink write failure")
	sink := &errorSink{err: sentinel}
	ctx := context.Background()
	at := time.Now()

	err := EmitDispatchInitiated(ctx, sink, 1, "p", "s", "q", at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitDispatchInitiated: expected sentinel error, got %v", err)
	}

	err = EmitCacheHitExact(ctx, sink, 1, "p", "s", "q", FreshnessFresh, at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitCacheHitExact: expected sentinel error, got %v", err)
	}

	err = EmitCacheHitSemantic(ctx, sink, 1, "p", "s", "q", FreshnessFresh, at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitCacheHitSemantic: expected sentinel error, got %v", err)
	}

	err = EmitRevalidatedFresh(ctx, sink, 1, "url", 200, "", "", "", "", at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitRevalidatedFresh: expected sentinel error, got %v", err)
	}

	err = EmitRevalidatedStaleRefetched(ctx, sink, 1, "url", 200, "", "", "", "", at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitRevalidatedStaleRefetched: expected sentinel error, got %v", err)
	}

	err = EmitFindingsReturned(ctx, sink, 1, "p", "s", "q", 0, CacheHitExact, FreshnessFresh, at)
	if !errors.Is(err, sentinel) {
		t.Errorf("EmitFindingsReturned: expected sentinel error, got %v", err)
	}
}
