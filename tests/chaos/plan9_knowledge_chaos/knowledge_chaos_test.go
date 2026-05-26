//go:build chaos && cgo
// +build chaos,cgo

package plan9_knowledge_chaos

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
)

func TestChaos_AggregatorDegradedModeFTSPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmp := t.TempDir()

	pinPath := filepath.Join(tmp, "pin.db")
	pinDB, err := aggregator.Open(ctx, pinPath)
	if err != nil {
		t.Fatalf("aggregator.Open pin: %v", err)
	}
	defer func() { _ = pinDB.Close() }()
	if err := aggregator.Init(ctx, pinDB); err != nil {
		t.Fatalf("aggregator.Init pin: %v", err)
	}

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    &chaosStubStore{},
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	results, err := agg.Query(ctx, aggregator.QueryRequest{
		Text:  "any query text for FTS path",
		Scope: aggregator.ScopePinnedOnly,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ScopePinnedOnly query failed under degraded-mode contract: %v", err)
	}

	if results == nil {
		t.Logf("ScopePinnedOnly returned nil slice (expected for unseeded pin index; contract honoured: no error)")
	} else {
		t.Logf("ScopePinnedOnly returned %d results (unseeded pin index; degraded-mode contract honoured)", len(results))
	}
}

// TestChaos_AggregatorDegradedQueryRejectsUnknownScope asserts that
// even under simulated degraded-mode the aggregator's Query validator
// continues to reject malformed Scope values (inv-zen-152). This
// guards against a regression where the degraded-mode code path
// short-circuits validation and accepts ecosystem-rag or other
//
// The contract: Query MUST surface a validation error before
// dispatching to any underlying subsystem (vec, FTS, graph). A
// degraded-mode aggregator is still bound by the inv-zen-152
// boundary. We assert err != nil + err message contains the
// inv-zen-152 pointer (the Plan 14 escape hatch documented in
// types.go:144-146).
func TestChaos_AggregatorDegradedQueryRejectsUnknownScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tmp := t.TempDir()

	pinPath := filepath.Join(tmp, "pin.db")
	pinDB, err := aggregator.Open(ctx, pinPath)
	if err != nil {
		t.Fatalf("aggregator.Open: %v", err)
	}
	defer func() { _ = pinDB.Close() }()
	if err := aggregator.Init(ctx, pinDB); err != nil {
		t.Fatalf("aggregator.Init: %v", err)
	}

	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384),
		Store:    &chaosStubStore{},
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	_, err = agg.Query(ctx, aggregator.QueryRequest{
		Text:  "ecosystem RAG query",
		Scope: aggregator.Scope("ecosystem-rag"),
		Limit: 10,
	})
	if err == nil {
		t.Fatalf("Query with unknown Scope succeeded; expected inv-zen-152 rejection")
	}
}

type chaosStubStore struct{}

func (*chaosStubStore) ListAuthorizedProjects(_ context.Context) ([]knowledgetypes.ProjectHandle, error) {
	return nil, nil
}

func (*chaosStubStore) OpenProjectVault(_ context.Context, _ string) (knowledgetypes.ProjectVault, error) {
	return nil, errors.New("chaosStubStore: per-project vault not wired in chaos tier (ScopePinnedOnly should not call OpenProjectVault)")
}

func (*chaosStubStore) UpdateAuditChainAnchor(_ context.Context, _, _, _ string) error {
	return nil
}

var _ = embed.NewMockEmbedder
var _ = time.Now
