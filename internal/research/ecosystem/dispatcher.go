// SPDX-License-Identifier: MIT
// internal/research/ecosystem/dispatcher.go
//
//
// This is the load-bearing centerpiece of : every D-task (D-1..D-8 +
// D-10..D-12 + audit emitter + indexer + version
// detector + HADES design doctrine accessor) is wired here into a single
// goroutine-safe Query method.
//
// Goroutine model:
// - Step 5 fans out 1..4 parallel goroutines (per ecosystem); all other steps
// execute in the caller goroutine.
// - All Dispatcher dependencies are injected via Options at construction and
// never mutated post-init; the Dispatcher is safe for concurrent Query calls.
// - Per-call state (latency map, fused candidates, validation result) lives on
// the call's stack — no shared mutable state between concurrent Query calls.
//
// 14-step orchestration (verbatim spec §4.2 + plan-file §D-9):
//
// 1. version-context detection cascade
// 2. doctrine resolve → DoctrineProfile (HADES design Accessor; req.Doctrine
// override applied per-call without mutating package-level state)
// 3. router.Classify(query) → single | top-2 | broadcast (D-1+D-2)
// 4. audit.Emit(EvtRAGQuery, 92)
// 5. parallel fan-out: per-eco BinaryTop200 ∥ FTS5Top200 → local RRF top-50
// 6. cross-eco RRF k=60 weighted-by-confidence (D-10 FuseWeighted)
// 7. audit.Emit(EvtRAGRetrieval, 93)
// 8. BGE-reranker-v2-m3 → top-K per profile.MaxResults (D-3; fallback D-4)
// 9. Bayesian abstention μ−λσ (D-6); if abstain → emit EvtRAGAbstain + return
// 10. hydrate chunks + AnswerGenerator + citation grammar validation 3-retry
// (D-7); on persistent failure → emit EvtRAGAbstain + return.
// On accept: emit EvtRAGCitation (gated by doctrine).
// 11. verifier.Verify() 3-stage cascade (D-5); emit EvtRAGVerify
// 12. capa-firewall refuse-on-unverified gate (RefuseOnUnverified + !Strict);
// if fire → emit EvtRAGAbstain + return.
// 13. LLM-judge re-pass (max-scope only via DoctrineProfile.LLMJudgeEnabled;
// D-8 HaikuLLMJudge); on reject → emit EvtRAGAbstain + return.
// 14. audit.Emit(EvtRAGAnswer, 97) → return QueryResult.
//
// Owned invariants:
// - invariant partial: 4-goroutine fan-out result merge is deterministic
// given fixed embedder / fixed classifier (verified by
// TestDispatcher_FanOut_Determinism_InvHades200). Full enforcement at
// - invariant partial: 6 query-side events emit in canonical order
// (Query → Retrieval → Citation → Verify → Abstain → Answer). Full
// chain-consistency property test at
// - invariant partial: live-fallback hook surface present (full impl D-11).
// - invariant partial: doctrine strictness knob (LLMJudgeEnabled +
// RefuseOnUnverified + AuditEmissionLevel + CitationMode) applied per-call.
//
// invariant boundary: this file does NOT import internal/store; it consumes
// per-ecosystem retrieval via the aggregatorAdapter interface (concrete impl
// lives at indexer.go in, IndexerQueryAdapter satisfied by *Indexer).

package ecosystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type aggregatorAdapter interface {
	BinaryTop200(ctx context.Context, queryEmbBin []byte, versionFilter string, eco Ecosystem) ([]Candidate, error)
	FTS5Top200(ctx context.Context, queryText, versionFilter string, eco Ecosystem) ([]Candidate, error)
	HydrateChunks(ctx context.Context, chunkIDs []int64, eco Ecosystem) ([]QueryChunk, error)
}

type versionDetectorAdapter interface {
	Detect(ctx context.Context, req QueryRequest) (detectedVersion string, layer int, err error)
}

type doctrineResolverAdapter interface {
	Resolve(ctx context.Context, projectPath string) (*DoctrineProfile, error)
}

type auditEmitterAdapter interface {
	Emit(ctx context.Context, evt EventType, payload interface{}) (int64, error)
	SetProfile(profile *DoctrineProfile)
}

// Options is the constructor input for NewDispatcher. All non-optional fields
// MUST be non-nil; NewDispatcher validates and returns an error on missing
// dependencies (fail-loud at startup vs surface mid-Query as nil-deref panic).
//
// Optional fields (LLMJudge, Accessor, AuditChain): nil acceptable for
// phased-construction during testing. Production wiring at satisfies
// all fields.
type Options struct {
	Embedder Embedder

	Reranker Reranker

	Router *Router

	Verifier *Verifier

	AbstentionPolicy *AbstentionPolicy

	LLMJudge LLMJudge

	DoctrineAccessor *active.Accessor

	AuditChain RAGAuditChainEmitter

	AnswerGenerator AnswerGenerator
}

type Dispatcher struct {
	embedder         Embedder
	reranker         Reranker
	router           *Router
	verifier         *Verifier
	abstention       *AbstentionPolicy
	llmJudge         LLMJudge
	doctrineResolver doctrineResolverAdapter
	auditEmitter     auditEmitterAdapter
	answerGenerator  AnswerGenerator
	versionDetector  versionDetectorAdapter
	aggregators      map[Ecosystem]aggregatorAdapter

	sources       map[Ecosystem]map[SourceType]Source
	researchMCP   ResearchMCPSynthesizer
	findingsCache FindingsCache
	indexerDelta  IndexerDeltaWriter
}

// NewDispatcher validates Options and constructs a Dispatcher.
//
// Validation
// - Embedder non-nil + NOT NoopEmbedder (production embedder required)
// - Router non-nil
// - Reranker non-nil (test code can inject NoopReranker via Options)
// - Verifier + AbstentionPolicy non-nil (security-critical paths)
//
// Other dependencies (LLMJudge, AuditChain, AnswerGenerator,
// DoctrineAccessor) may be nil at construction time and wired by the
// caller before Query is invoked. The Query method documents which fields
// each step depends on; calling Query without the corresponding field
// surfaces a typed error at the relevant step (NOT a nil-deref panic).
func NewDispatcher(opts Options) (*Dispatcher, error) {
	if opts.Embedder == nil {
		return nil, errors.New("dispatcher: Options.Embedder required")
	}

	if _, isNoop := opts.Embedder.(NoopEmbedder); isNoop {
		return nil, errors.New("dispatcher: Options.Embedder must not be NoopEmbedder (production embedder required per embedder.go contract)")
	}
	if opts.Router == nil {
		return nil, errors.New("dispatcher: Options.Router required")
	}
	if opts.Reranker == nil {
		return nil, errors.New("dispatcher: Options.Reranker required")
	}
	if opts.Verifier == nil {
		return nil, errors.New("dispatcher: Options.Verifier required")
	}
	if opts.AbstentionPolicy == nil {
		return nil, errors.New("dispatcher: Options.AbstentionPolicy required")
	}

	d := &Dispatcher{
		embedder:        opts.Embedder,
		reranker:        opts.Reranker,
		router:          opts.Router,
		verifier:        opts.Verifier,
		abstention:      opts.AbstentionPolicy,
		llmJudge:        opts.LLMJudge,
		answerGenerator: opts.AnswerGenerator,
		aggregators:     map[Ecosystem]aggregatorAdapter{},
	}

	if opts.DoctrineAccessor != nil {
		d.doctrineResolver = NewDoctrineResolver(opts.DoctrineAccessor)
	}

	if opts.AuditChain != nil {

		defaultProf := builtinProfiles["default"]
		d.auditEmitter = NewRAGAuditEmitter(opts.AuditChain, &defaultProf)
	}

	return d, nil
}

func (d *Dispatcher) Query(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.doctrineResolver == nil {
		return nil, errors.New("dispatcher: doctrineResolver unavailable (caller must set DoctrineAccessor in Options or assign d.doctrineResolver before Query)")
	}
	if d.auditEmitter == nil {
		return nil, errors.New("dispatcher: auditEmitter unavailable (caller must set AuditChain in Options or assign d.auditEmitter before Query)")
	}
	if d.versionDetector == nil {
		return nil, errors.New("dispatcher: versionDetector unavailable (stage daemon-init must assign d.versionDetector after NewDispatcher)")
	}
	if len(d.aggregators) == 0 {
		return nil, errors.New("dispatcher: per-ecosystem aggregators unavailable (stage daemon-init must populate d.aggregators after NewDispatcher)")
	}

	queryStart := time.Now()
	latencies := map[string]float64{}
	record := func(name string, start time.Time) {
		latencies[name] = float64(time.Since(start).Microseconds()) / 1000.0
	}

	stepStart := time.Now()
	version, detectionLayer, err := d.versionDetector.Detect(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: version detect: %w", err)
	}
	record("version_detect", stepStart)

	stepStart = time.Now()
	profile, err := d.doctrineResolver.Resolve(ctx, req.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: doctrine resolve: %w", err)
	}

	if req.Doctrine != "" && req.Doctrine != profile.Name {
		if forced, ok := builtinProfiles[req.Doctrine]; ok {
			cp := forced
			cp.AbstentionThresholds = copyThresholds(forced.AbstentionThresholds)
			profile = &cp
		}
	}

	d.auditEmitter.SetProfile(profile)
	record("doctrine_resolve", stepStart)

	stepStart = time.Now()
	queryEmbFP32, err := d.embedder.EmbedFP32_1536d(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: query embed fp32: %w", err)
	}
	routing, err := d.router.Classify(ctx, req.Query, queryEmbFP32)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: router classify: %w", err)
	}
	record("router_classify", stepStart)

	queryEmbBin, err := d.embedder.EmbedBinary256d(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: query embed bin: %w", err)
	}
	queryAuditSeq, err := d.auditEmitter.Emit(ctx, EvtRAGQuery, RAGQueryPayload{
		Query:                req.Query,
		Ecosystem:            req.Ecosystem,
		Version:              version,
		VersionLayer:         detectionLayer,
		Doctrine:             profile.Name,
		Routing:              routing,
		ClassifierCheckpoint: d.router.ClassifierCheckpointHash(),
		ProjectPath:          req.ProjectPath,
	})
	if err != nil {
		return nil, fmt.Errorf("dispatcher: emit query event: %w", err)
	}

	stepStart = time.Now()
	perEcoCandidates, err := d.fanOutRetrieve(ctx, routing, queryEmbBin, req.Query, version)
	if err != nil {
		return nil, err
	}
	record("fanout_retrieve", stepStart)

	stepStart = time.Now()
	fused := aggregator.FuseWeighted(
		toFuseInput(perEcoCandidates),
		routingWeights(routing),
		60,
		50,
	)
	record("rrf_fuse", stepStart)

	if _, err := d.auditEmitter.Emit(ctx, EvtRAGRetrieval, RAGRetrievalPayload{
		PerEcoCounts: countCandidates(perEcoCandidates),
		FusedCount:   len(fused),
		K:            60,
		Weights:      routingWeightsByEco(routing),
	}); err != nil {
		return nil, fmt.Errorf("dispatcher: emit retrieval event: %w", err)
	}

	stepStart = time.Now()
	candidates := fusedToCandidates(fused, perEcoCandidates)
	rerankTopK := profile.MaxResults
	if rerankTopK <= 0 {
		rerankTopK = 10
	}
	reranked, err := d.reranker.Rerank(ctx, req.Query, candidates, rerankTopK)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: rerank: %w", err)
	}
	record("rerank", stepStart)

	scores := scoresFromReranked(reranked)
	primaryEco := primaryEcosystem(routing, req.Ecosystem)
	abstainDecision := d.abstention.ShouldAbstainWithOverride(
		ctx, primaryEco, scores, profile.AbstentionThresholds,
	)
	if abstainDecision.Abstain {
		if _, err := d.auditEmitter.Emit(ctx, EvtRAGAbstain, RAGAbstainPayload{
			Reason:    abstainDecision.Reason,
			Lambda:    abstainDecision.Lambda,
			Mean:      abstainDecision.Mean,
			Stdev:     abstainDecision.Stdev,
			Threshold: abstainDecision.Threshold,
			Ecosystem: primaryEco,
			Doctrine:  profile.Name,
		}); err != nil {
			return nil, fmt.Errorf("dispatcher: emit abstain (low-confidence): %w", err)
		}
		return &QueryResult{
			Abstained:     true,
			AbstainReason: abstainDecision.Reason,
			Provenance:    d.buildProvenance(routing, version, detectionLayer, profile, latencies, false),
			AuditChainSeq: queryAuditSeq,
		}, nil
	}

	chunks, err := d.hydrateChunks(ctx, reranked)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: hydrate chunks: %w", err)
	}
	answer, validation, err := d.generateAndValidateCitations(ctx, req, chunks, profile)
	if err != nil {
		return nil, err
	}
	if validation.AbstainTriggered {
		if _, emitErr := d.auditEmitter.Emit(ctx, EvtRAGAbstain, RAGAbstainPayload{
			Reason:    "citation grammar persistent failure",
			Doctrine:  profile.Name,
			Ecosystem: primaryEco,
		}); emitErr != nil {
			return nil, fmt.Errorf("dispatcher: emit abstain (citation): %w", emitErr)
		}
		return &QueryResult{
			Abstained:     true,
			AbstainReason: "citation grammar persistent failure",
			Provenance:    d.buildProvenance(routing, version, detectionLayer, profile, latencies, false),
			AuditChainSeq: queryAuditSeq,
		}, nil
	}
	if profile.CitationMode != CitationNone {
		if _, err := d.auditEmitter.Emit(ctx, EvtRAGCitation, RAGCitationPayload{
			Citations: validation.Citations,
		}); err != nil {
			return nil, fmt.Errorf("dispatcher: emit citation: %w", err)
		}
	}

	symbolRefs := symbolRefsFromCitations(validation.Citations, primaryEco, version)
	verify, err := d.verifyWithDoctrine(ctx, symbolRefs, profile)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: verify: %w", err)
	}
	if _, err := d.auditEmitter.Emit(ctx, EvtRAGVerify, RAGVerifyPayload{
		Verifications: verify.Verifications,
		AllVerified:   verify.AllVerified,
	}); err != nil {
		return nil, fmt.Errorf("dispatcher: emit verify: %w", err)
	}

	if profile.RefuseOnUnverified && !verify.AllVerified && !req.Strict {
		if _, err := d.auditEmitter.Emit(ctx, EvtRAGAbstain, RAGAbstainPayload{
			Reason:    "capa-firewall: refuse on unverified symbol",
			Doctrine:  profile.Name,
			Ecosystem: primaryEco,
		}); err != nil {
			return nil, fmt.Errorf("dispatcher: emit abstain (capa-firewall): %w", err)
		}
		return &QueryResult{
			Abstained:     true,
			AbstainReason: "capa-firewall: refuse on unverified symbol",
			Verified:      verify.Verifications,
			Provenance:    d.buildProvenance(routing, version, detectionLayer, profile, latencies, false),
			AuditChainSeq: queryAuditSeq,
		}, nil
	}

	if profile.LLMJudgeEnabled && d.llmJudge != nil {
		judge, err := d.llmJudge.Judge(ctx, req.Query, answer, chunks, validation.Citations)
		if err != nil {
			return nil, fmt.Errorf("dispatcher: llm judge: %w", err)
		}
		if !judge.Acceptable {
			reason := "llm_judge: " + judge.Reason
			if _, err := d.auditEmitter.Emit(ctx, EvtRAGAbstain, RAGAbstainPayload{
				Reason:           reason,
				Doctrine:         profile.Name,
				Ecosystem:        primaryEco,
				SuspiciousChunks: judge.SuspiciousChunks,
			}); err != nil {
				return nil, fmt.Errorf("dispatcher: emit abstain (llm-judge): %w", err)
			}
			return &QueryResult{
				Abstained:     true,
				AbstainReason: reason,
				Verified:      verify.Verifications,
				Provenance:    d.buildProvenance(routing, version, detectionLayer, profile, latencies, false),
				AuditChainSeq: queryAuditSeq,
			}, nil
		}
	}

	finalChunks := decorateChunksWithCitationAndVerification(chunks, validation.Citations, verify.Verifications)
	answerSeq, err := d.auditEmitter.Emit(ctx, EvtRAGAnswer, RAGAnswerPayload{
		AnswerHashSHA256: sha256Hex(answer),
		CitedChunkIDs:    chunkIDsFromCitations(validation.Citations),
		Doctrine:         profile.Name,
		TotalLatencyMs:   float64(time.Since(queryStart).Microseconds()) / 1000.0,
	})
	if err != nil {
		return nil, fmt.Errorf("dispatcher: emit answer: %w", err)
	}
	return &QueryResult{
		Chunks:        finalChunks,
		Citations:     validation.Citations,
		Verified:      verify.Verifications,
		Provenance:    d.buildProvenance(routing, version, detectionLayer, profile, latencies, false),
		AuditChainSeq: answerSeq,
	}, nil
}

func (d *Dispatcher) fanOutRetrieve(
	ctx context.Context,
	routing RoutingDecision,
	queryEmbBin []byte,
	queryText, version string,
) (map[Ecosystem][]Candidate, error) {
	if len(routing.Ecosystems) == 0 {
		return nil, errors.New("dispatcher fanout: routing has zero ecosystems")
	}
	out := make(map[Ecosystem][]Candidate, len(routing.Ecosystems))
	var mu sync.Mutex
	errCh := make(chan error, len(routing.Ecosystems))
	var wg sync.WaitGroup
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, eco := range routing.Ecosystems {
		eco := eco
		wg.Add(1)
		go func() {
			defer wg.Done()
			agg, ok := d.aggregators[eco]
			if !ok {
				errCh <- fmt.Errorf("fanout: aggregator missing for %s", eco)
				cancel()
				return
			}
			binCands, err := agg.BinaryTop200(childCtx, queryEmbBin, version, eco)
			if err != nil {
				errCh <- fmt.Errorf("fanout %s binary: %w", eco, err)
				cancel()
				return
			}
			ftsCands, err := agg.FTS5Top200(childCtx, queryText, version, eco)
			if err != nil {
				errCh <- fmt.Errorf("fanout %s fts: %w", eco, err)
				cancel()
				return
			}
			top50 := localRRFTop50(binCands, ftsCands)
			mu.Lock()
			out[eco] = top50
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errCh)

	if err, ok := <-errCh; ok && err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func localRRFTop50(bin, fts []Candidate) []Candidate {
	type entry struct {
		score   float64
		cand    Candidate
		rankBin int
		rankFTS int
	}
	table := map[int64]*entry{}
	order := make([]int64, 0, len(bin)+len(fts))
	for i, c := range bin {
		if _, ok := table[c.ChunkID]; !ok {
			table[c.ChunkID] = &entry{cand: c, rankBin: i + 1}
			order = append(order, c.ChunkID)
		}
		table[c.ChunkID].score += 1.0 / float64(60+i+1)
	}
	for i, c := range fts {
		if _, ok := table[c.ChunkID]; !ok {
			table[c.ChunkID] = &entry{cand: c, rankFTS: i + 1}
			order = append(order, c.ChunkID)
		} else {
			table[c.ChunkID].rankFTS = i + 1
		}
		table[c.ChunkID].score += 1.0 / float64(60+i+1)
	}
	out := make([]Candidate, 0, len(table))
	for _, id := range order {
		e := table[id]
		c := e.cand
		c.SimilarityScore = e.score
		out = append(out, c)
	}
	sortCandidatesScoreDescStable(out)
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func sortCandidatesScoreDescStable(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].SimilarityScore != cs[j].SimilarityScore {
			return cs[i].SimilarityScore > cs[j].SimilarityScore
		}
		return cs[i].ChunkID < cs[j].ChunkID
	})
}

func (d *Dispatcher) hydrateChunks(ctx context.Context, reranked []RankedResult) ([]QueryChunk, error) {
	perEco := map[Ecosystem][]int64{}
	for _, r := range reranked {
		perEco[r.Ecosystem] = append(perEco[r.Ecosystem], r.ChunkID)
	}
	var out []QueryChunk

	for _, eco := range AllEcosystems {
		ids, ok := perEco[eco]
		if !ok {
			continue
		}
		agg, ok := d.aggregators[eco]
		if !ok {
			return nil, fmt.Errorf("hydrate: aggregator missing for %s", eco)
		}
		chunks, err := agg.HydrateChunks(ctx, ids, eco)
		if err != nil {
			return nil, fmt.Errorf("hydrate %s: %w", eco, err)
		}
		out = append(out, chunks...)
	}
	decorateRerankerScores(out, reranked)
	return out, nil
}

func (d *Dispatcher) generateAndValidateCitations(
	ctx context.Context,
	req QueryRequest,
	chunks []QueryChunk,
	profile *DoctrineProfile,
) (string, *ValidationResult, error) {
	if d.answerGenerator == nil {
		return "", nil, errors.New("dispatcher: AnswerGenerator unavailable (stage daemon-init must assign d.answerGenerator before Query under non-CitationNone profiles)")
	}
	validator, err := NewCitationValidator(CitationConfig{Mode: profile.CitationMode})
	if err != nil {
		return "", nil, fmt.Errorf("dispatcher: new citation validator: %w", err)
	}
	res, err := validator.ValidateWithRetry(ctx, d.answerGenerator, req.Query, chunks, 3)
	if err != nil {
		return "", nil, fmt.Errorf("dispatcher: citation validate: %w", err)
	}
	return res.AnswerText, res, nil
}

func (d *Dispatcher) buildProvenance(
	routing RoutingDecision,
	version string,
	detectionLayer int,
	profile *DoctrineProfile,
	latencies map[string]float64,
	freshDispatch bool,
) QueryProvenance {

	ecos := append([]Ecosystem(nil), routing.Ecosystems...)
	latCopy := make(map[string]float64, len(latencies))
	for k, v := range latencies {
		latCopy[k] = v
	}
	return QueryProvenance{
		DetectedVersion:   version,
		DetectionLayer:    detectionLayer,
		RoutingEcosystems: ecos,
		RoutingMethod:     string(routing.Method),
		FreshDispatch:     freshDispatch,
		DoctrineApplied:   profile.Name,
		RerankerModel:     d.rerankerModelLabel(),
		EmbedderModel:     d.embedderModelLabel(),
		LatencyBreakdown:  latCopy,
	}
}

func (d *Dispatcher) verifyWithDoctrine(
	ctx context.Context,
	refs []SymbolRef,
	profile *DoctrineProfile,
) (*VerifyResult, error) {
	if profile.Name == "default" {

		skipCfg := d.verifier.cfg
		skipCfg.SkipStageC = true
		skipVerifier, err := NewVerifier(skipCfg)
		if err != nil {
			return nil, fmt.Errorf("dispatcher: build skip-stage-c verifier: %w", err)
		}
		return skipVerifier.Verify(ctx, refs)
	}
	return d.verifier.Verify(ctx, refs)
}

func decorateRerankerScores(chunks []QueryChunk, reranked []RankedResult) {
	byID := map[int64]float64{}
	for _, r := range reranked {
		byID[r.ChunkID] = r.RerankerScore
	}
	for i := range chunks {
		chunks[i].RerankerScore = byID[chunks[i].ChunkID]
	}
}

func decorateChunksWithCitationAndVerification(
	chunks []QueryChunk,
	citations []CitationRef,
	verifications []SymbolVerification,
) []QueryChunk {
	cByID := map[int64]string{}
	for _, c := range citations {
		cByID[c.ChunkID] = c.ID
	}
	vByPath := map[string]string{}
	for _, v := range verifications {
		if v.Exists {
			vByPath[v.Symbol.SymbolPath] = "exists"
		} else {
			vByPath[v.Symbol.SymbolPath] = "not_found"
		}
	}
	for i, c := range chunks {
		chunks[i].CitationID = cByID[c.ChunkID]
		if status, ok := vByPath[c.SymbolPath]; ok {
			chunks[i].VerificationStatus = status
		} else {
			chunks[i].VerificationStatus = "skipped"
		}
	}
	return chunks
}

func symbolRefsFromCitations(citations []CitationRef, eco Ecosystem, version string) []SymbolRef {
	out := make([]SymbolRef, 0, len(citations))
	for _, c := range citations {
		out = append(out, SymbolRef{
			Ecosystem:  eco,
			SymbolPath: c.SymbolPath,
			Version:    version,
		})
	}
	return out
}

func chunkIDsFromCitations(citations []CitationRef) []int64 {
	out := make([]int64, 0, len(citations))
	for _, c := range citations {
		out = append(out, c.ChunkID)
	}
	return out
}

func routingWeights(r RoutingDecision) map[string]float64 {
	out := make(map[string]float64, len(r.ConfidenceWeights))
	for e, w := range r.ConfidenceWeights {
		out[string(e)] = w
	}
	return out
}

func routingWeightsByEco(r RoutingDecision) map[Ecosystem]float64 {
	out := make(map[Ecosystem]float64, len(r.ConfidenceWeights))
	for e, w := range r.ConfidenceWeights {
		out[e] = w
	}
	return out
}

func toFuseInput(perEco map[Ecosystem][]Candidate) []aggregator.TopK {

	out := make([]aggregator.TopK, 0, len(perEco))
	for _, eco := range AllEcosystems {
		cands, ok := perEco[eco]
		if !ok {
			continue
		}
		results := make([]aggregator.QueryResult, len(cands))
		for i, c := range cands {
			results[i] = aggregator.QueryResult{
				NoteID: chunkNoteID(c.Ecosystem, c.ChunkID),
				Title:  c.SymbolPath,
				Source: string(eco),
				Score:  c.SimilarityScore,
			}
		}
		out = append(out, aggregator.TopK{Source: string(eco), Results: results})
	}
	return out
}

func chunkNoteID(eco Ecosystem, id int64) string {
	return string(eco) + ":" + strconv.FormatInt(id, 10)
}

func countCandidates(perEco map[Ecosystem][]Candidate) map[Ecosystem]int {
	out := make(map[Ecosystem]int, len(perEco))
	for e, c := range perEco {
		out[e] = len(c)
	}
	return out
}

func fusedToCandidates(
	fused []aggregator.QueryResult,
	perEco map[Ecosystem][]Candidate,
) []Candidate {
	byNoteID := map[string]Candidate{}
	for _, cs := range perEco {
		for _, c := range cs {
			byNoteID[chunkNoteID(c.Ecosystem, c.ChunkID)] = c
		}
	}
	out := make([]Candidate, 0, len(fused))
	for _, r := range fused {
		if c, ok := byNoteID[r.NoteID]; ok {
			c.SimilarityScore = r.Score
			out = append(out, c)
		}
	}
	return out
}

func scoresFromReranked(rs []RankedResult) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = r.RerankerScore
	}
	return out
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func primaryEcosystem(routing RoutingDecision, requested Ecosystem) Ecosystem {
	if requested != "" {
		return requested
	}
	if len(routing.Ecosystems) > 0 {
		return routing.Ecosystems[0]
	}
	return EcoGo
}

func (d *Dispatcher) rerankerModelLabel() string {
	if d.reranker == nil {
		return ""
	}
	switch d.reranker.(type) {
	case *BGEReRankerV2M3:
		return "bge-reranker-v2-m3"
	case *CohereRerankV4:
		return "cohere-rerank-v4.0"
	case NoopReranker:
		return "noop-reranker"
	default:
		return "unknown-reranker"
	}
}

func (d *Dispatcher) embedderModelLabel() string {
	if d.embedder == nil {
		return ""
	}

	if _, ok := d.embedder.(NoopEmbedder); ok {
		return "noop-embedder"
	}
	return "jina-code-embeddings-1.5b"
}
