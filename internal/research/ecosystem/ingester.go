// SPDX-License-Identifier: MIT
// Package ecosystem — ingester.go orchestrates bulk + delta source ingestion.
//
// Per spec §4.1 ingestion pipeline + §0.4 doctrine (no stubs, código completo).
// Concurrent per-package goroutines + per-package failure isolation +
// resumability via ecosystem_packages.last_indexed_at + audit emit
// EvtRAGIngestPackage = 98 (per-package) + EvtRAGIngestJoinKey = 99 (B-11
// vault-note join keys; B-10 ships pipeline that B-11 wraps).
//
//
// Pipeline (one goroutine per package, errgroup-bounded by WorkerCount):
//
//	Source.FetchManifest
//	  → for each pkg → resumability check (Indexer.PackageLastIndexedAt + DeltaOnly)
//	       → if newer manifest OR delta-only=false:
//	            Source.FetchPackageDoc
//	            Chunker.Chunk(doc) → []Chunk   (or synthesizeFallbackChunks when Chunker nil)
//	            for each chunk: SymbolIndex.Register (when configured)
//	            Indexer.WriteChunks (single atomic per-package txn; chunks + symbols + changes)
//	            Indexer.UpdatePackageLastIndexedAt
//	            Source.FetchChangelog (Phase E consumes; B-10 just dispatches)
//	            AuditChain.Append(EvtRAGIngestPackage = 98, succeeded=true)
//	     on per-package error:
//	            AuditChain.Append(EvtRAGIngestPackage = 98, succeeded=false, error=...)
//	            atomic.AddInt64(&failed, 1)
//	            DO NOT propagate — continue with next pkg job.
//
// Per-package failure isolation guarantees one crash never propagates;
// IngestResult.PackagesFailed surfaces the per-package failure count
// (Plan 14 B-10 additive amendment to Phase A IngestResult; spec §4.1).
//
// Resumability per-package check of Indexer.PackageLastIndexedAt BEFORE
// fetching; if manifest's LastUpdated ≤ last_indexed_at AND req.DeltaOnly,
// skip. On ctx.Cancel(), in-flight goroutines drain via errgroup; pending
// jobs abandoned; next Ingest() resumes naturally (last_indexed_at unchanged
// for skipped + in-flight packages).
//
// Race-clean: errgroup + atomic counters + per-worker no shared mutable
// state except channel + Indexer (Phase C concrete *Indexer is internally
// synchronized via SQLite transactions). TestIngester_Ingest_ConcurrentSafety
// passes -race -count=10 (security/correctness invariant).
//
// Doctrine compliance:
//   - inv-zen-031: ingester does NOT import internal/store / internal/daemon/*
//     directly; IndexerWriter + SymbolIndexRegistrar narrow interfaces live HERE.
//   - inv-zen-191: no direct net/http (Source impls own their HTTP via cache.Revalidator).
//   - EventType slots 98/99 declared canonically at Phase A A-1 in
//     internal/orchestrator/eventlog/events.go; Phase B consumes by name
//     (no local uint32 redeclarations).
//   - Per spec §4.1 per-package atomicity: chunks + symbols + changes flow
//     through a SINGLE IndexerWriter.WriteChunks call (Phase C concrete
//     *Indexer wraps that in a single SQLite transaction).

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// IndexerWriter is the minimum surface Ingester needs from Phase C indexer impl.
// B-10 uses this interface; Phase C ships indexerImpl (concrete *Indexer)
// satisfying it.
//
// Stage 2 amendment 2026-05-15 (C3 reconciliation):
// Renamed from `Indexer` → `IndexerWriter` to avoid collision with Phase C's
// concrete `*Indexer` struct. Six-parameter `WriteChunks` aligns with Phase C
// concrete signature (chunks + symbols + changes — single atomic write per
// package, not three separate calls).
//
// PackageLastIndexedAt + UpdatePackageLastIndexedAt are methods on Phase C's
// `*Indexer` struct (canonical owner); Phase B consumes via this narrow
// interface to preserve inv-zen-031 boundary discipline (ingester package
// does NOT import internal/store).
//
// Concurrency implementations MUST be safe for concurrent WriteChunks /
// PackageLastIndexedAt / UpdatePackageLastIndexedAt calls from N goroutines
// (Phase B ingester fan-out N). Phase C concrete *Indexer relies on SQLite
// txn serialization for safety.
type IndexerWriter interface {
	WriteChunks(ctx context.Context, pkg PackageRef, version string, chunks []Chunk, symbols []SymbolRef, changes []ChangeNode) error

	PackageLastIndexedAt(ctx context.Context, pkg PackageRef) (time.Time, error)

	UpdatePackageLastIndexedAt(ctx context.Context, pkg PackageRef, t time.Time) error
}

// SymbolIndexRegistrar is the minimum surface Ingester needs for symbol
// registration (verify-at-answer-time cache; Phase F surface).
//
// Concurrency implementations MUST be safe for concurrent Register /
// Lookup calls from N goroutines (Phase B ingester fan-out N).
type SymbolIndexRegistrar interface {
	Register(ctx context.Context, sym SymbolRef) error

	Lookup(ctx context.Context, symPath string) (SymbolRef, bool, error)
}

// VaultWriter is the minimum surface ProcessVaultNote needs from Plan 7's
// vault.db. Phase F wires the real impl backed by Plan 7's vault hook +
// sqlite transaction layer. NIL OK at unit-test boundary (ingester silent-
// skips the write step; audit emit + return remain functional).
//
// Declared HERE in ingester.go per inv-zen-031 boundary — Phase B does NOT
// import internal/store / internal/daemon; narrow interfaces live in
// internal/research/ecosystem and are wired at daemon-init time.
//
// Concurrency implementations MUST be safe for concurrent
// UpdateEcosystemJoinKeys calls from N goroutines (Phase F real impl relies
// on SQLite txn serialization; tests' recordingVault uses a mutex).
type VaultWriter interface {
	UpdateEcosystemJoinKeys(ctx context.Context, noteID int64, joinKeys []string) error
}

type IngesterOptions struct {
	Sources map[Ecosystem]map[SourceType]Source

	Indexer IndexerWriter

	SymbolIndex SymbolIndexRegistrar

	VaultWriter VaultWriter

	Chunker *Chunker

	WorkerCount int

	AuditChain RAGAuditChainEmitter

	DoctrineName string
}

type Ingester struct {
	opts IngesterOptions
}

func NewIngester(opts IngesterOptions) (*Ingester, error) {
	if opts.Sources == nil {
		return nil, errors.New("ingester: nil Sources")
	}
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = runtime.NumCPU()
	}
	if opts.DoctrineName == "" {
		opts.DoctrineName = "default"
	}
	return &Ingester{opts: opts}, nil
}

func (ing *Ingester) Ingest(ctx context.Context, req IngestRequest) (*IngestResult, error) {
	start := time.Now()
	res := &IngestResult{StartedAt: start}

	srcMap, ok := ing.opts.Sources[req.Ecosystem]
	if !ok {
		return nil, fmt.Errorf("ingester: no sources registered for ecosystem %s", req.Ecosystem)
	}

	srcKinds := req.Sources
	if len(srcKinds) == 0 {

		for k := range srcMap {
			srcKinds = append(srcKinds, k)
		}
	}

	type pkgJob struct {
		eco       Ecosystem
		src       Source
		pkg       ManifestPackage
		deltaOnly bool
	}

	jobs := make(chan pkgJob, 256)

	var ingested int64
	var failed int64

	g, gctx := errgroup.WithContext(ctx)
	for w := 0; w < ing.opts.WorkerCount; w++ {
		g.Go(func() error {
			for {
				select {
				case <-gctx.Done():
					return gctx.Err()
				case j, ok := <-jobs:
					if !ok {
						return nil
					}

					startedAt := time.Now()
					if err := ing.processPackage(gctx, j.eco, j.src, j.pkg, j.deltaOnly); err != nil {
						if errors.Is(err, errSkipResumability) {

							continue
						}

						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							continue
						}
						atomic.AddInt64(&failed, 1)
						ing.emitFailureAudit(gctx, j.eco, j.pkg, err, startedAt)
						continue
					}
					atomic.AddInt64(&ingested, 1)
				}
			}
		})
	}

	// Producer enumerate manifests per requested source.
	// Runs in its own goroutine so the workers can start draining concurrently.
	// MUST close(jobs) when done so workers exit cleanly.
	//
	// COVERAGE three defensive branches in this producer are intentionally
	// not exercised by unit tests:
	//   - `if !ok { continue }` (line ~310): srcKinds default-populated from
	//     srcMap iteration → kind is ALWAYS in srcMap on the default path; only
	//     reachable when caller passes IngestRequest.Sources with an unregistered
	//     SourceType for the ecosystem (operator-API misuse; defensive guard).
	//   - `if manifest == nil { continue }`: Source impls return either a non-nil
	//     manifest or an error (mock + concrete impls both honour the contract);
	//     defensive guard against future impl drift.
	//   - producer `case <-ctx.Done(): return`: ctx-cancel mid-enumerate is
	//     timing-dependent — TestIngester_Ingest_ContextCancelMidIngest tries
	//     to exercise it but the producer typically completes before the
	//     cancel fires on small manifests. Defensive guard against large-manifest
	//     cancel-mid-enumerate paths in production.
	go func() {
		defer close(jobs)
		for _, kind := range srcKinds {
			src, ok := srcMap[kind]
			if !ok {
				continue
			}
			manifest, err := src.FetchManifest(ctx)
			if err != nil {

				continue
			}
			if manifest == nil {
				continue
			}
			for _, p := range manifest.Packages {
				select {
				case <-ctx.Done():
					return
				case jobs <- pkgJob{eco: req.Ecosystem, src: src, pkg: p, deltaOnly: req.DeltaOnly}:
				}
			}
		}
	}()

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return res, err
	}

	res.PackagesIngested = int(atomic.LoadInt64(&ingested))
	res.PackagesFailed = int(atomic.LoadInt64(&failed))
	res.CompletedAt = time.Now()
	return res, nil
}

var errSkipResumability = errors.New("ingester: skip per resumability check")

// processPackage runs the per-package pipeline.
//
// Pipeline
//  1. resumability check: PackageLastIndexedAt → skip if LastUpdated ≤ last_indexed_at AND deltaOnly
//  2. fetch doc: Source.FetchPackageDoc
//  3. chunk: Chunker.Chunk OR synthesizeFallbackChunks (when Chunker nil)
//  4. symbol register: SymbolIndex.Register per chunk (when SymbolIndex non-nil)
//  5. write atomically: IndexerWriter.WriteChunks(chunks + symbols + changes)
//  6. bookkeeping: IndexerWriter.UpdatePackageLastIndexedAt
//  7. fetch changelog: Source.FetchChangelog (Phase E consumes; B-10 dispatches only)
//  8. emit audit: AuditChain.Append(EvtRAGIngestPackage, succeeded=true)
//
// Returns nil on full success. Returns errSkipResumability when the
// package is up-to-date AND deltaOnly==true (caller distinguishes via
// errors.Is). Returns a wrapped error on any other failure (caller
// increments PackagesFailed and emits failure audit).
//
// Doctrine ctx is checked at every external call boundary (fetch / write /
// register / changelog) so cancellation propagates promptly without
// half-committing per-package state.
//
// startedAt is captured here (at processPackage entry) and threaded to
// emit* helpers so audit payloads distinguish processing-start from emit
// time (Plan 14 B-10 fix-cycle 2026-05-18 / IMPORTANT-2; pre-fix the
// emit-time time.Now() served BOTH started_at AND completed_at, which
// made Phase G ops dashboards lose per-package latency signal).
//
// deltaOnly carries IngestRequest.DeltaOnly through the worker channel
// (Plan 14 B-10 fix-cycle 2026-05-18 / CRITICAL-1): force-refresh
// (DeltaOnly=false) MUST re-ingest "up-to-date" packages — the
// resumability skip path is reachable ONLY when deltaOnly==true.
func (ing *Ingester) processPackage(ctx context.Context, eco Ecosystem, src Source, pkg ManifestPackage, deltaOnly bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	startedAt := time.Now()

	pkgRef := PackageRef{
		Ecosystem:           eco,
		Name:                pkg.Name,
		CanonicalNamespace:  pkg.Name,
		UpstreamURL:         pkg.UpstreamURL,
		LatestStableVersion: pkg.LatestStableVersion,
	}

	if ing.opts.Indexer != nil && deltaOnly {
		last, err := ing.opts.Indexer.PackageLastIndexedAt(ctx, pkgRef)

		if err == nil && !last.IsZero() && !pkg.LastUpdated.After(last) {
			return errSkipResumability
		}
	}

	doc, err := src.FetchPackageDoc(ctx, pkgRef)
	if err != nil {
		return fmt.Errorf("fetch doc %s: %w", pkg.Name, err)
	}
	if doc == nil {
		return fmt.Errorf("fetch doc %s: nil doc", pkg.Name)
	}

	var chunks []Chunk
	if ing.opts.Chunker != nil {
		chunks, err = ing.opts.Chunker.Chunk(ctx, doc)
		if err != nil {
			return fmt.Errorf("chunk %s: %w", pkg.Name, err)
		}
	} else {

		chunks = synthesizeFallbackChunks(doc)
	}

	var symbols []SymbolRef
	if ing.opts.SymbolIndex != nil {
		for _, ch := range chunks {
			if ch.SymbolPath == "" {
				continue
			}
			sym := SymbolRef{Ecosystem: eco, SymbolPath: ch.SymbolPath, Version: doc.Version}
			symbols = append(symbols, sym)

			_ = ing.opts.SymbolIndex.Register(ctx, sym)
		}
	}

	var changes []ChangeNode

	if ing.opts.Indexer != nil {
		if err := ing.opts.Indexer.WriteChunks(ctx, pkgRef, doc.Version, chunks, symbols, changes); err != nil {
			return fmt.Errorf("write chunks %s: %w", pkg.Name, err)
		}

		_ = ing.opts.Indexer.UpdatePackageLastIndexedAt(ctx, pkgRef, time.Now())
	}

	_, _ = src.FetchChangelog(ctx, pkgRef, doc.Version)

	ing.emitSuccessAudit(ctx, eco, pkgRef, doc, chunks, startedAt)
	return nil
}

func synthesizeFallbackChunks(doc *PackageDoc) []Chunk {
	out := make([]Chunk, 0, len(doc.Sections))
	for _, sec := range doc.Sections {
		out = append(out, Chunk{
			PackageID:         doc.Package.ID,
			VersionIntroduced: doc.Version,
			ContentText:       sec.Body,
			SourceType:        SrcPackageDoc,
			SymbolPath:        sec.SymbolPath,
			Kind:              sec.Kind,
			SourceURL:         sec.SourceURL,
		})
	}
	return out
}

func (ing *Ingester) emitSuccessAudit(ctx context.Context, eco Ecosystem, pkg PackageRef, doc *PackageDoc, chunks []Chunk, startedAt time.Time) {
	if ing.opts.AuditChain == nil {
		return
	}
	body := marshalAuditPayload(map[string]interface{}{
		"package_id":         pkg.ID,
		"package_name":       pkg.Name,
		"ecosystem":          string(eco),
		"version":            doc.Version,
		"chunks_count":       len(chunks),
		"symbols_count":      countSymbols(chunks),
		"change_nodes_count": 0,
		"started_at":         startedAt.UTC().Format(time.RFC3339Nano),
		"completed_at":       time.Now().UTC().Format(time.RFC3339Nano),
		"succeeded":          true,
	})

	_, _ = ing.opts.AuditChain.Append(ctx, eventlog.EvtRAGIngestPackage, body, ing.opts.DoctrineName)
}

func (ing *Ingester) emitFailureAudit(ctx context.Context, eco Ecosystem, pkg ManifestPackage, err error, startedAt time.Time) {
	if ing.opts.AuditChain == nil {
		return
	}
	body := marshalAuditPayload(map[string]interface{}{
		"package_name": pkg.Name,
		"ecosystem":    string(eco),
		"error":        err.Error(),
		"started_at":   startedAt.UTC().Format(time.RFC3339Nano),
		"completed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"succeeded":    false,
	})
	_, _ = ing.opts.AuditChain.Append(ctx, eventlog.EvtRAGIngestPackage, body, ing.opts.DoctrineName)
}

func marshalAuditPayload(payload map[string]interface{}) []byte {
	body, err := json.Marshal(payload)
	if err != nil {
		return []byte("{}")
	}
	return body
}

func countSymbols(chunks []Chunk) int {
	seen := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		if c.SymbolPath != "" {
			seen[c.SymbolPath] = true
		}
	}
	return len(seen)
}

var (
	goSymbolRegex = regexp.MustCompile("`([a-zA-Z0-9_/]+\\.[A-Z][a-zA-Z0-9_]*)`")

	pythonSymbolRegex = regexp.MustCompile("`([a-z][a-zA-Z0-9_]*\\.[a-z][a-zA-Z0-9_.]+)`")

	tsSymbolRegex = regexp.MustCompile("`([a-z][a-zA-Z0-9_]*\\.[a-z][a-zA-Z0-9_]+)`")

	rustSymbolRegex = regexp.MustCompile("`([a-z][a-zA-Z0-9_]*::[a-zA-Z][a-zA-Z0-9_:]+)`")
)

func (ing *Ingester) ProcessVaultNote(ctx context.Context, note Note) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ing.opts.SymbolIndex == nil {
		return errors.New("ingester: ProcessVaultNote: nil SymbolIndex")
	}

	candidates := detectSymbolCandidates(note.Content)
	var joinKeys []string
	for _, cand := range candidates {

		if err := ctx.Err(); err != nil {
			return err
		}
		resolved, found, err := ing.opts.SymbolIndex.Lookup(ctx, cand)
		if err != nil || !found {

			continue
		}
		key := fmt.Sprintf("%s:%s:%s", resolved.Ecosystem, resolved.Version, resolved.SymbolPath)
		joinKeys = append(joinKeys, key)
	}

	if len(joinKeys) == 0 {

		return nil
	}

	joinKeys = dedupStrings(joinKeys)

	if ing.opts.VaultWriter != nil {
		if err := ing.opts.VaultWriter.UpdateEcosystemJoinKeys(ctx, note.ID, joinKeys); err != nil {
			return fmt.Errorf("ingester: ProcessVaultNote: vault write: %w", err)
		}
	}

	if ing.opts.AuditChain != nil {
		for _, key := range joinKeys {
			body := marshalAuditPayload(map[string]interface{}{
				"note_id":  note.ID,
				"join_key": key,
				"symbols":  len(joinKeys),
			})

			_, _ = ing.opts.AuditChain.Append(ctx, eventlog.EvtRAGIngestJoinKey, body, ing.opts.DoctrineName)
		}
	}

	return nil
}

func detectSymbolCandidates(content string) []string {
	var out []string

	for _, re := range []*regexp.Regexp{goSymbolRegex, pythonSymbolRegex, tsSymbolRegex, rustSymbolRegex} {
		matches := re.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				out = append(out, m[1])
			}
		}
	}
	return dedupStrings(out)
}

func dedupStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
