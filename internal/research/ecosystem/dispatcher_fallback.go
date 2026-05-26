// SPDX-License-Identifier: MIT
// internal/research/ecosystem/dispatcher_fallback.go
//
//
// Live fallback path per spec §4.3 + inv-zen-203.
//
// # When invoked
//
// Triggered when the in-corpus dispatcher.Query path returns zero
// high-confidence results — i.e., the requested package or symbol is not in
// any ecosystem.db. Examples:
//   - Operator queries about a newly-released package
//     (`go get github.com/new/pkg@v0.1.0` before the next 6h cron tick)
//   - Symbol is in an obscure GitHub repo not in the top-1000
//
// # Flow (spec §4.3)
//
//  1. Audit: emit EvtRAGQuery with payload.fresh_dispatch=true FIRST
//     (inv-zen-203 ordering — chain captures dispatch reason BEFORE any
//     live-network operation, so an aborted-mid-flight fallback still
//     leaves an audit trail).
//  2. Source.FetchManifest force-refresh: the context is decorated with
//     contextWithForceRefresh; source impls call
//     internal/research/cache.Revalidator.Fetch with FetchOptions{
//     ForceRefresh true} when resolveForceRefresh(ctx) returns true.
//     This bypasses TTL + ETag conditionals (cache.revalidator §5).
//  3. If pkg in manifest: FetchPackageDoc + IndexerDeltaWriter.WriteDelta
//     (so the next query hits the corpus normally).
//  4. Else: research-MCP (Plan 4) synthesis. Cache finding in Plan 9 F
//     research_findings table with cache_hit_reason='fresh_dispatch'.
//  5. Return QueryResult{Provenance.FreshDispatch=true} in either branch.
//
// # inv-zen-203 enforcement
//
// EvtRAGQuery (with payload.FreshDispatch=true) emits BEFORE any
// live-network operation. The audit chain captures the dispatch reason
// even if subsequent steps fail. The early emit also acts as a circuit
// breaker: if the chain itself is failing (disk full, corrupted), we
// don't attempt the live-network work — fail-loud is preferable to
// silent un-audited network calls.
//
// # inv-zen-031 boundary
//
// This file does NOT import internal/store; it consumes only narrow
// interfaces (ResearchMCPSynthesizer, FindingsCache, IndexerDeltaWriter)
// satisfied by adapters at Phase F daemon-init.

package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type LiveFallbackRequest struct {
	Query       string
	Ecosystem   Ecosystem
	PackageHint string
	ProjectPath string
}

// ResearchMCPSynthesizer abstracts the Plan 4 research MCP. The daemon wires
// the concrete impl at startup (Phase F); tests inject a fake.
//
// Contract Synthesize returns the rendered answer text on success. ctx
// cancellation MUST be honored (long-running web fetches in production).
type ResearchMCPSynthesizer interface {
	Synthesize(ctx context.Context, query string, eco Ecosystem) (string, error)
}

// FindingsCache abstracts the Plan 9 F research_findings table writer.
//
// Contract Cache writes a row keyed by (key, query, answer, eco, reason).
// The dispatcher always passes reason="fresh_dispatch" from this path.
// Implementations MUST be idempotent on key — repeat Cache calls with the
// same key MUST NOT error and MUST NOT duplicate rows.
type FindingsCache interface {
	Cache(ctx context.Context, key, query, answer string, eco Ecosystem, reason string) error
}

type IndexerDeltaWriter interface {
	WriteDelta(ctx context.Context, eco Ecosystem, doc *PackageDoc) error
}

// LiveFallback runs the spec §4.3 live-fallback path. Returns a QueryResult
// with Provenance.FreshDispatch=true on success.
//
// Return semantics:
//   - Package-found branch: QueryResult.Chunks is empty (the next query will
//     hit the corpus via the standard Query path now that the package is
//     delta-indexed). DoctrineApplied="fresh-dispatch".
//   - Package-not-found branch (synthesis): QueryResult.Chunks contains one
//     synthetic QueryChunk with the MCP answer in ContentText. The synthesis
//     answer is also persisted to FindingsCache for future cache hits.
//   - On error: nil + non-nil error (wrapped with step name).
//
// Concurrency Dispatcher is safe for concurrent LiveFallback calls. The
// audit emitter serializes its writes; sources MUST be safe for concurrent
// calls per the Source contract (source.go §"Concurrency"). Two concurrent
// fallbacks for the same package may both fetch+index — the second
// IndexerDeltaWriter.WriteDelta is a no-op upsert at the storage layer
// (inv-zen-200 conflict-resolution: last write wins on identical chunk
// fingerprint).
//
// Pre ctx non-nil; req.Ecosystem identifies a wired Source; d.auditEmitter
// wired.
// Post on nil error: audit chain has one EvtRAGQuery row with
// fresh_dispatch=true (inv-zen-203); on package-found branch indexerDelta
// has one new entry; on synthesis branch findingsCache has one new entry.
func (d *Dispatcher) LiveFallback(ctx context.Context, req LiveFallbackRequest) (*QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.auditEmitter == nil {
		return nil, errors.New("dispatcher fallback: auditEmitter not wired (caller must set AuditChain in Options or assign d.auditEmitter before LiveFallback)")
	}

	doctrineName := "default"
	if d.doctrineResolver != nil {
		if prof, err := d.doctrineResolver.Resolve(ctx, req.ProjectPath); err == nil && prof != nil && prof.Name != "" {
			doctrineName = prof.Name
		}
	}

	// inv-zen-203: emit EvtRAGQuery FIRST. If the chain is failing (disk
	// full, corrupted), we MUST NOT proceed to live-network ops — better to
	// surface an un-audited dispatch as an error than to silently dispatch
	// without a chain link. This is the load-bearing ordering invariant.
	queryAuditSeq, err := d.auditEmitter.Emit(ctx, EvtRAGQuery, RAGQueryPayload{
		Query:         req.Query,
		Ecosystem:     req.Ecosystem,
		Doctrine:      doctrineName,
		FreshDispatch: true,
		ProjectPath:   req.ProjectPath,
	})
	if err != nil {
		return nil, fmt.Errorf("dispatcher fallback: emit query event: %w", err)
	}

	src, ok := d.sourceFor(req.Ecosystem)
	if !ok {
		return nil, fmt.Errorf("dispatcher fallback: no source for ecosystem %s", req.Ecosystem)
	}

	manifest, err := d.fetchManifestForceRefresh(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("dispatcher fallback: fetch manifest: %w", err)
	}

	pkg, found := findInManifest(manifest, req.PackageHint, req.Query)
	if found {

		pkg.Ecosystem = req.Ecosystem
		doc, err := src.FetchPackageDoc(ctx, pkg)
		if err != nil {
			return nil, fmt.Errorf("dispatcher fallback: fetch package doc: %w", err)
		}
		if d.indexerDelta != nil {
			if err := d.indexerDelta.WriteDelta(ctx, req.Ecosystem, doc); err != nil {
				return nil, fmt.Errorf("dispatcher fallback: indexer delta: %w", err)
			}
		}
		return &QueryResult{
			Chunks: []QueryChunk{},
			Provenance: QueryProvenance{
				FreshDispatch:     true,
				RoutingEcosystems: []Ecosystem{req.Ecosystem},
				RoutingMethod:     string(RoutingMethodSingle),
				DoctrineApplied:   "fresh-dispatch",
			},
			AuditChainSeq: queryAuditSeq,
		}, nil
	}

	if d.researchMCP == nil {
		return nil, errors.New("dispatcher fallback: research MCP not wired (Phase F daemon-init must assign d.researchMCP before LiveFallback synthesis path)")
	}
	answer, err := d.researchMCP.Synthesize(ctx, req.Query, req.Ecosystem)
	if err != nil {
		return nil, fmt.Errorf("dispatcher fallback: research mcp synth: %w", err)
	}
	if d.findingsCache != nil {
		key := computeFindingKey(req.Query, req.Ecosystem)
		if err := d.findingsCache.Cache(ctx, key, req.Query, answer, req.Ecosystem, "fresh_dispatch"); err != nil {
			return nil, fmt.Errorf("dispatcher fallback: cache finding: %w", err)
		}
	}
	return &QueryResult{
		Chunks: []QueryChunk{{
			ContentText: answer,
		}},
		Provenance: QueryProvenance{
			FreshDispatch:     true,
			RoutingEcosystems: []Ecosystem{req.Ecosystem},
			RoutingMethod:     string(RoutingMethodSingle),
			DoctrineApplied:   "fresh-dispatch",
		},
		AuditChainSeq: queryAuditSeq,
	}, nil
}

func (d *Dispatcher) sourceFor(eco Ecosystem) (Source, bool) {
	if d.sources == nil {
		return nil, false
	}
	sourcesPerEco, ok := d.sources[eco]
	if !ok {
		return nil, false
	}
	for _, s := range sourcesPerEco {
		return s, true
	}
	return nil, false
}

func (d *Dispatcher) fetchManifestForceRefresh(ctx context.Context, src Source) (*Manifest, error) {
	return src.FetchManifest(contextWithForceRefresh(ctx))
}

type fallbackForceRefreshKey struct{}

func contextWithForceRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, fallbackForceRefreshKey{}, true)
}

func resolveForceRefresh(ctx context.Context) bool {
	v := ctx.Value(fallbackForceRefreshKey{})
	b, _ := v.(bool)
	return b
}

func findInManifest(m *Manifest, hint, query string) (PackageRef, bool) {
	if m == nil {
		return PackageRef{}, false
	}
	target := hint
	if target == "" {
		target = extractPackageNameFromQuery(query)
	}
	if target == "" {
		return PackageRef{}, false
	}
	for _, p := range m.Packages {
		if p.Name == target {
			return PackageRef{
				Name:                p.Name,
				LatestStableVersion: p.LatestStableVersion,
				UpstreamURL:         p.UpstreamURL,
			}, true
		}
	}
	return PackageRef{}, false
}

func extractPackageNameFromQuery(q string) string {
	if q == "" {
		return ""
	}

	if i := strings.Index(q, "github.com/"); i >= 0 {
		tail := q[i:]

		end := len(tail)
		for j, r := range tail {
			if r == ' ' || r == '\t' || r == '\n' {
				end = j
				break
			}
		}
		token := tail[:end]

		if strings.Count(token, "/") >= 2 {
			return token
		}
	}

	if i := strings.Index(q, "@"); i >= 0 {
		tail := q[i:]
		end := len(tail)
		for j, r := range tail {
			if r == ' ' || r == '\t' || r == '\n' {
				end = j
				break
			}
		}
		token := tail[:end]
		if strings.Count(token, "/") >= 1 && len(token) > 2 {
			return token
		}
	}

	if i := strings.Index(q, "`"); i >= 0 {
		tail := q[i+1:]
		if j := strings.Index(tail, "`"); j > 0 {
			return tail[:j]
		}
	}
	return ""
}

func computeFindingKey(query string, eco Ecosystem) string {
	return string(eco) + ":" + sha256Hex(query)
}

// Note on the cache.Revalidator contract:
//
// The dispatcher's force-refresh hint flows to sources via
// resolveForceRefresh(ctx); source impls (sources/*.go) translate that hint
// into cache.FetchOptions{ForceRefresh: true} on their Revalidator.Fetch
// calls. We deliberately do NOT import internal/research/cache here:
//
//   - the dispatcher fallback path does not directly construct cache types,
//   - cache.revalidator_fetch.go is gated behind `//go:build cgo` so a
//     hard import would break GOOS=linux builds without CGO.
//
// The contract is enforced by integration tests in Phase F daemon-init that
// wire concrete Source impls; their tests verify resolveForceRefresh→
// FetchOptions{ForceRefresh: true} round-trip on real Revalidator instances.
