// SPDX-License-Identifier: MIT
// Package augment — Pipeline.Run is the central augmentation orchestrator.
//
// Sequence (per design contract):
// 1. Validate request
// 2. DoctrineGate.Check (capa-firewall / disabled / max_tokens=0 -> skip)
// 3. BudgetGate.Check (over-cap -> skip)
// 4. Audit AugmentationStarted leaf
// 5. 5-lane fan-out via wg + per-lane timeout
// 6. Per-lane PrivacyFilter.FilterCrossProject
// 7. AggregatorConsumer.RunRRF
// 8. CommunitySummarize
// 9. CacheSplit
// 10. Truncation guard
// 11. BudgetGate.Commit
// 12. Audit AugmentationCompleted (or Truncated/Skipped) leaf
// 13. Return AugmentResponse
//
// Concurrency budget (design choice):
// - PerLaneTimeout: doctrine-tunable (max-scope=2s / default=1s / capa-firewall=500ms)
// - ConcurrencyBudget: in-flight lane goroutines
// - QueueDepth: queued Pipeline.Run calls beyond ConcurrencyBudget
//
// Partial-failure tolerance: errored lanes contribute empty TopK; pipeline continues.
//
// invariant: ZERO direct LLM calls from this file.

package augment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

const (
	ToolCaronteQuery = "mcp_hades-system_caronte_query"

	ToolCaronteContext = "mcp_hades-system_caronte_context"

	ToolCaronteCoChange = "mcp_hades-system_caronte_get_cochange"
)

func (r *pipelineRuntime) tryAcquire(maxInflight, maxQueue int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initCond()
	if r.inflight < maxInflight {
		r.inflight++
		return "inflight", nil
	}
	if r.queued >= maxQueue {
		return "", fmt.Errorf("augment: queue depth %d exceeded", maxQueue)
	}
	r.queued++
	return "queued", nil
}

func (r *pipelineRuntime) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflight--
	if r.cond != nil {
		r.cond.Broadcast()
	}
}

func (r *pipelineRuntime) releaseQueued() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued--
	if r.cond != nil {
		r.cond.Broadcast()
	}
}

func (r *pipelineRuntime) waitForCapacity(ctx context.Context, maxInflight int) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			if r.cond != nil {
				r.cond.Broadcast()
			}
			r.mu.Unlock()
		case <-stop:
		}
	}()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.initCond()
	for r.inflight >= maxInflight {
		if err := ctx.Err(); err != nil {
			r.queued--
			return err
		}
		r.cond.Wait()
	}
	r.queued--
	r.inflight++
	return nil
}

func (p *Pipeline) Run(ctx context.Context, req AugmentRequest) (AugmentResponse, error) {

	if req.Prompt == "" {
		return AugmentResponse{}, errors.New("augment: AugmentRequest.Prompt required")
	}
	if req.ProjectID == "" {
		return AugmentResponse{}, errors.New("augment: AugmentRequest.ProjectID required")
	}
	if req.RequestID == "" {
		return AugmentResponse{}, errors.New("augment: AugmentRequest.RequestID required (idempotency)")
	}
	if req.Doctrine == "" {
		req.Doctrine = "default"
	}
	if req.Mode == "" {
		req.Mode = "interactive"
	}

	if err := ctx.Err(); err != nil {
		return AugmentResponse{}, err
	}

	state, err := p.runtimeState.tryAcquire(p.concurrency, p.queueDepth)
	if err != nil {
		return AugmentResponse{}, err
	}
	if state == "queued" {

		if err := p.runtimeState.waitForCapacity(ctx, p.concurrency); err != nil {
			return AugmentResponse{}, err
		}
	}
	defer p.runtimeState.release()

	allowed, skipReason, err := p.doctrine.Check(ctx, req.Doctrine)
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: doctrine_gate: %w", err)
	}
	if !allowed {
		// propagate. invariant "every augmentation event MUST anchor"
		// is breached if the skip emit silently fails — operators see a
		// skipped outcome but no chain anchor row.
		eventID, emitErr := p.auditAnchor.Emit(ctx, EventAugmentationSkipped,
			mustJSON(map[string]any{
				"session_id": req.SessionID,
				"project":    req.ProjectID,
				"doctrine":   req.Doctrine,
				"reason":     skipReason,
			}),
			req.ProjectID,
		)
		if emitErr != nil {
			return AugmentResponse{}, fmt.Errorf("augment: audit skip (doctrine): %w", emitErr)
		}
		return AugmentResponse{
			SkippedReason: skipReason,
			AuditEventID:  eventID,
		}, nil
	}

	schema, err := p.doctrineLoaderField.Load(ctx, req.Doctrine)
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: doctrine_loader (post-check): %w", err)
	}
	if schema == nil {
		return AugmentResponse{}, fmt.Errorf("augment: doctrine_loader: nil schema for %q (post-check)", req.Doctrine)
	}
	maxKgTokens := schema.Augmentation.MaxKGTokens

	capUSD := float64(maxKgTokens) * PerTokenUSDDefault * 100

	proceed, blockedScope, err := p.budget.Check(ctx, BudgetCheckInput{
		ProjectID:       req.ProjectID,
		Doctrine:        req.Doctrine,
		RequestedTokens: maxKgTokens,
		CapUSD:          capUSD,
	})
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: budget_gate.Check: %w", err)
	}
	if !proceed {

		eventID, emitErr := p.auditAnchor.Emit(ctx, EventAugmentationSkipped,
			mustJSON(map[string]any{
				"session_id":    req.SessionID,
				"project":       req.ProjectID,
				"doctrine":      req.Doctrine,
				"reason":        "budget-cap",
				"blocked_scope": blockedScope,
			}),
			req.ProjectID,
		)
		if emitErr != nil {
			return AugmentResponse{}, fmt.Errorf("augment: audit skip (budget): %w", emitErr)
		}
		return AugmentResponse{
			SkippedReason: "budget-cap",
			AuditEventID:  eventID,
		}, nil
	}

	startedID, err := p.auditAnchor.Emit(ctx, EventAugmentationStarted,
		mustJSON(map[string]any{
			"session_id": req.SessionID,
			"project":    req.ProjectID,
			"doctrine":   req.Doctrine,
			"prompt_len": len(req.Prompt),
		}),
		req.ProjectID,
	)
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: audit started: %w", err)
	}

	lanes, laneEmitErr := p.runFiveLanes(ctx, req, schema)
	if laneEmitErr != nil {

		return AugmentResponse{}, fmt.Errorf("augment: audit kg_query_dispatched: %w", laneEmitErr)
	}

	filtered, droppedProjects, err := p.applyPrivacyFilter(ctx, lanes, req)
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: privacy_filter: %w", err)
	}
	if len(droppedProjects) > 0 {

		if _, emitErr := p.auditAnchor.Emit(ctx, EventCrossProjectQueryFiltered,
			mustJSON(map[string]any{
				"session_id":       req.SessionID,
				"source_doctrine":  req.Doctrine,
				"source_project":   req.ProjectID,
				"dropped_projects": droppedProjects,
			}),
			req.ProjectID,
		); emitErr != nil {
			return AugmentResponse{}, fmt.Errorf("augment: audit cross_project_filtered: %w", emitErr)
		}
	}

	fuseLimit := maxKgTokens / 100
	if fuseLimit <= 0 {
		fuseLimit = 20
	}
	fusedRaw := p.aggregator.RunRRF(ctx, filtered, fuseLimit)
	fused := convertToRRFFused(fusedRaw, filtered)

	summaries, _ := Summarize(ctx, fused, req.ProjectID)

	projectMeta := ProjectMeta{
		ProjectID: req.ProjectID,
		Doctrine:  req.Doctrine,
	}

	staticCtx, volatileCtx := p.cacheSplit.Split(summaries, fused, projectMeta, nil, nil)

	staticCtx, volatileCtx, truncated := p.truncation.Apply(ctx, staticCtx, volatileCtx, maxKgTokens)
	if truncated {

		if _, emitErr := p.auditAnchor.Emit(ctx, EventAugmentationTruncated,
			mustJSON(map[string]any{
				"session_id":       req.SessionID,
				"project":          req.ProjectID,
				"requested_tokens": staticCtx.EstimatedTokens + volatileCtx.EstimatedTokens,
				"max_tokens":       maxKgTokens,
			}),
			req.ProjectID,
		); emitErr != nil {
			return AugmentResponse{}, fmt.Errorf("augment: audit truncated: %w", emitErr)
		}
	}

	totalTokens := staticCtx.EstimatedTokens + volatileCtx.EstimatedTokens
	if totalTokens > 0 {
		if err := p.budget.Commit(ctx, BudgetCommitInput{
			RequestID: req.RequestID,
			ProjectID: req.ProjectID,
			Doctrine:  req.Doctrine,
			Tokens:    totalTokens,
		}); err != nil {
			return AugmentResponse{}, fmt.Errorf("augment: budget commit: %w", err)
		}
	}

	citations := buildCitations(fused, req.ProjectID)

	completedEvType := EventAugmentationCompleted
	if truncated {
		completedEvType = EventAugmentationTruncated
	}
	completedID, err := p.auditAnchor.Emit(ctx, completedEvType,
		mustJSON(map[string]any{
			"session_id":    req.SessionID,
			"project":       req.ProjectID,
			"doctrine":      req.Doctrine,
			"tokens":        totalTokens,
			"lanes_used":    laneIDsFromLanes(filtered),
			"citations":     len(citations),
			"started_event": startedID,
		}),
		req.ProjectID,
	)
	if err != nil {
		return AugmentResponse{}, fmt.Errorf("augment: audit completed: %w", err)
	}

	return AugmentResponse{
		StaticContext:   staticCtx,
		VolatileContext: volatileCtx,
		Citations:       citations,
		AuditEventID:    completedID,
		Truncated:       truncated,
		SkippedReason:   "",
	}, nil
}

// runFiveLanes fans out 5 lanes in parallel with per-lane timeout.
//
// auditAnchor.Emit error any lane goroutine produces (alongside the
// usual lane TopK list). The orchestrator surfaces this error to the
// caller — invariant "every augmentation event MUST anchor" is
// load-bearing; silently swallowing lane emit errors leaves the chain
// with no record of gateway-call failures.
func (p *Pipeline) runFiveLanes(ctx context.Context, req AugmentRequest, schema *DoctrineSchema) ([]TopK, error) {
	timeoutMs := schema.Augmentation.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = int(p.perLaneTO.Milliseconds())
	}
	perLaneTO := time.Duration(timeoutMs) * time.Millisecond

	limit := 20

	type laneResult struct {
		topK    TopK
		emitErr error
	}
	resCh := make(chan laneResult, 5)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		laneCtx, cancel := context.WithTimeout(ctx, perLaneTO)
		defer cancel()
		resp, err := p.gateway.CallTool(laneCtx, ToolCaronteQuery, map[string]any{
			"project_id": req.ProjectID,
			"query":      req.Prompt,
			"limit":      limit,
		})
		if err != nil {
			_, emitErr := p.auditAnchor.Emit(ctx, EventKGQueryDispatched,
				mustJSON(map[string]any{
					"lane": 1, "tool": ToolCaronteQuery, "project": req.ProjectID, "error": err.Error(),
				}), req.ProjectID)
			resCh <- laneResult{topK: TopK{Source: "kg"}, emitErr: emitErr}
			return
		}
		resCh <- laneResult{topK: TopK{Source: "kg", Results: parseGatewayResults(resp, "kg", req.ProjectID)}}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		laneCtx, cancel := context.WithTimeout(ctx, perLaneTO)
		defer cancel()
		l2, err := p.aggregator.Lane2FTS(laneCtx, req.Prompt, limit)
		if err != nil {
			resCh <- laneResult{topK: TopK{Source: "fts"}}
			return
		}
		resCh <- laneResult{topK: TopK{Source: "fts", Results: l2.Results}}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		laneCtx, cancel := context.WithTimeout(ctx, perLaneTO)
		defer cancel()
		resp, err := p.gateway.CallTool(laneCtx, ToolCaronteContext, map[string]any{
			"project_id": req.ProjectID,
			"query":      req.Prompt,
			"limit":      limit,
		})
		if err != nil {
			_, emitErr := p.auditAnchor.Emit(ctx, EventKGQueryDispatched,
				mustJSON(map[string]any{
					"lane": 3, "tool": ToolCaronteContext, "project": req.ProjectID, "error": err.Error(),
				}), req.ProjectID)
			resCh <- laneResult{topK: TopK{Source: "graph"}, emitErr: emitErr}
			return
		}
		resCh <- laneResult{topK: TopK{Source: "graph", Results: parseGatewayResults(resp, "graph", req.ProjectID)}}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		laneCtx, cancel := context.WithTimeout(ctx, perLaneTO)
		defer cancel()
		l4, err := p.aggregator.Lane4Vec(laneCtx, req.Prompt, limit, VecSimilarityThreshold)
		if err != nil {
			resCh <- laneResult{topK: TopK{Source: "vec"}}
			return
		}
		resCh <- laneResult{topK: TopK{Source: "vec", Results: l4.Results}}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		laneCtx, cancel := context.WithTimeout(ctx, perLaneTO)
		defer cancel()

		merged := make([]QueryResult, 0, limit*2)

		if l5, err := p.aggregator.Lane5Temporal(laneCtx, req.Prompt, time.Time{}, limit); err == nil {
			merged = append(merged, l5.Results...)
		}

		if resp, err := p.gateway.CallTool(laneCtx, ToolCaronteCoChange, map[string]any{
			"project_id": req.ProjectID,
			"file":       req.Prompt,
		}); err == nil {
			merged = append(merged, parseCoChangeResults(resp, req.ProjectID)...)
		}

		resCh <- laneResult{topK: TopK{Source: "temporal", Results: merged}}
	}()

	wg.Wait()
	close(resCh)

	out := make([]TopK, 0, 5)
	var firstEmitErr error
	for lr := range resCh {
		if lr.emitErr != nil && firstEmitErr == nil {
			firstEmitErr = lr.emitErr
		}
		if len(lr.topK.Results) > 0 {
			out = append(out, lr.topK)
		}
	}
	return out, firstEmitErr
}

func (p *Pipeline) applyPrivacyFilter(ctx context.Context, lanes []TopK, req AugmentRequest) ([]TopK, []string, error) {
	if len(lanes) == 0 {
		return nil, nil, nil
	}
	droppedSet := map[string]struct{}{}
	out := make([]TopK, 0, len(lanes))
	for _, lane := range lanes {
		filtered, dropped, err := p.privacy.FilterCrossProject(ctx, PrivacyFilterInput{
			SourceDoctrine: req.Doctrine,
			SourceProject:  req.ProjectID,
			Candidates:     lane.Results,
		})
		if err != nil {
			return nil, nil, err
		}
		for _, d := range dropped {
			droppedSet[d] = struct{}{}
		}
		if len(filtered) > 0 {
			out = append(out, TopK{Source: lane.Source, Results: filtered})
		}
	}
	dropped := make([]string, 0, len(droppedSet))
	for d := range droppedSet {
		dropped = append(dropped, d)
	}
	sort.Strings(dropped)
	return out, dropped, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}

func parseGatewayResults(resp any, source, projectID string) []QueryResult {
	m, ok := resp.(map[string]any)
	if !ok {
		return nil
	}
	rawResults, ok := m["results"].([]any)
	if !ok {
		return nil
	}
	out := make([]QueryResult, 0, len(rawResults))
	for _, r := range rawResults {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		qr := QueryResult{
			NoteID:    asString(rm["note_id"]),
			Title:     asString(rm["title"]),
			Snippet:   asString(rm["snippet"]),
			ProjectID: projectID,
			Source:    source,
		}
		if v, ok := rm["score"].(float64); ok {
			qr.Score = v
		}
		if v := asString(rm["audit_chain_anchor"]); v != "" {
			qr.AuditChainAnchor = v
		}
		out = append(out, qr)
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func parseCoChangeResults(resp any, projectID string) []QueryResult {
	m, ok := resp.(map[string]any)
	if !ok {
		return nil
	}
	rawPeers, ok := m["peers"].([]any)
	if !ok {
		return nil
	}
	out := make([]QueryResult, 0, len(rawPeers))
	for _, p := range rawPeers {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		path := asString(pm["path"])
		if path == "" {
			continue
		}
		coupling, _ := pm["coupling_percent"].(float64)
		shared, _ := pm["shared_revs"].(float64)
		window, _ := pm["window_days"].(float64)
		out = append(out, QueryResult{
			NoteID:    path,
			Title:     path,
			Snippet:   fmt.Sprintf("co-changed %.0f%% (%d shared revs / %dd window)", coupling, int(shared), int(window)),
			ProjectID: projectID,
			Source:    "temporal",
			Score:     coupling / 100.0,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func convertToRRFFused(fused []QueryResult, lanes []TopK) []RRFFusedResult {
	noteIDToLanes := make(map[string]map[int]struct{})
	laneIDForSource := map[string]int{
		"kg": 1, "fts": 2, "graph": 3, "vec": 4, "temporal": 5,
	}
	for _, lane := range lanes {
		laneID := laneIDForSource[lane.Source]
		if laneID == 0 {
			continue
		}
		for _, r := range lane.Results {
			s := noteIDToLanes[r.NoteID]
			if s == nil {
				s = map[int]struct{}{}
				noteIDToLanes[r.NoteID] = s
			}
			s[laneID] = struct{}{}
		}
	}
	out := make([]RRFFusedResult, 0, len(fused))
	for _, f := range fused {
		var lids []int
		for lid := range noteIDToLanes[f.NoteID] {
			lids = append(lids, lid)
		}
		sort.Ints(lids)
		out = append(out, RRFFusedResult{
			NoteID:           f.NoteID,
			Title:            f.Title,
			Snippet:          f.Snippet,
			Source:           f.Source,
			Score:            f.Score,
			ProjectID:        f.ProjectID,
			AuditChainAnchor: f.AuditChainAnchor,
			LaneIDs:          lids,
		})
	}
	return out
}

func laneIDsFromLanes(lanes []TopK) []int {
	laneIDForSource := map[string]int{
		"kg": 1, "fts": 2, "graph": 3, "vec": 4, "temporal": 5,
	}
	out := make([]int, 0, len(lanes))
	for _, lane := range lanes {
		if id := laneIDForSource[lane.Source]; id > 0 {
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

func buildCitations(fused []RRFFusedResult, projectID string) []citation.Envelope {
	if len(fused) == 0 {
		return nil
	}
	out := make([]citation.Envelope, 0, len(fused))
	for i, f := range fused {
		src := citationSourceFromLane(f.Source)
		lane := retrievalLaneFromSource(f.Source)

		cid := makeCitationID(f.NoteID, i)

		auditID := f.AuditChainAnchor
		if auditID == "" {

			auditID = fmt.Sprintf("evt-unanchored-%d", i)
		}

		proj := f.ProjectID
		if proj == "" {
			proj = projectID
		}

		payload := f.Title
		if f.Snippet != "" {
			if payload != "" {
				payload += " | "
			}
			payload += f.Snippet
		}
		if payload == "" {
			payload = f.NoteID
		}

		conf := clampConfidence(f.Score)

		env := citation.Envelope{
			ID:           citation.CitationID(cid),
			Type:         citation.CitationTypeKGNode,
			Source:       src,
			Lane:         lane,
			AuditEventID: auditID,
			Confidence:   conf,
			RRFScore:     clampNonNegative(f.Score),
			RRFRank:      i,
			ProjectID:    proj,
			Payload:      payload,
		}
		out = append(out, env)
	}
	return out
}

func citationSourceFromLane(source string) citation.CitationSource {
	switch source {
	case "kg":
		return citation.SourceCaronteQuery
	case "graph":
		return citation.SourceCaronteContext
	case "fts":
		return citation.SourceAggregatorFTS
	case "vec":
		return citation.SourceAggregatorVec
	case "temporal":
		return citation.SourceTemporal
	default:
		return citation.SourceManualOverride
	}
}

func retrievalLaneFromSource(source string) citation.RetrievalLane {
	switch source {
	case "kg":
		return citation.LaneSemantic
	case "graph":
		return citation.LaneGraph
	case "fts":
		return citation.LaneLexical
	case "vec":
		return citation.LaneRerank
	case "temporal":
		return citation.LaneTemporal
	default:
		return citation.LaneLexical
	}
}

func makeCitationID(noteID string, idx int) string {
	const (
		cleanCapacity  = 16
		hashSuffixLen  = 4
		prefixCapacity = cleanCapacity - hashSuffixLen
	)

	clean := make([]byte, 0, cleanCapacity)
	for i := 0; i < len(noteID); i++ {
		c := noteID[i]
		switch {
		case c >= 'a' && c <= 'z':
			clean = append(clean, c)
		case c >= 'A' && c <= 'Z':
			clean = append(clean, c+32)
		case c >= '0' && c <= '9':
			clean = append(clean, c)
		}
		if len(clean) >= cleanCapacity {
			break
		}
	}
	if len(clean) == 0 {

		clean = []byte(fmt.Sprintf("idx%d", idx))
	} else if shouldHashSuffix(noteID, cleanCapacity) {

		if len(clean) > prefixCapacity {
			clean = clean[:prefixCapacity]
		}
		clean = append(clean, citationHashSuffix(noteID, hashSuffixLen)...)
	}

	for len(clean) < 2 {
		clean = append(clean, '0')
	}
	return "c-" + string(clean)
}

func shouldHashSuffix(noteID string, budget int) bool {
	count := 0
	for i := 0; i < len(noteID); i++ {
		c := noteID[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			count++
		}
		if count > budget {
			return true
		}
	}
	return false
}

func citationHashSuffix(noteID string, n int) []byte {
	h := sha256.Sum256([]byte(noteID))

	var v uint64
	for i := 0; i < 8 && i < len(h); i++ {
		v |= uint64(h[i]) << (8 * uint(i))
	}
	s := strconv.FormatUint(v, 36)

	if len(s) < n {
		pad := make([]byte, n-len(s))
		for i := range pad {
			pad[i] = '0'
		}
		return append(pad, []byte(s)...)
	}
	return []byte(s[:n])
}

func clampConfidence(score float64) float64 {
	if score <= 0 {
		return 0
	}
	if score >= 1 {
		return 1
	}
	return score
}

func clampNonNegative(score float64) float64 {
	if score < 0 {
		return 0
	}
	return score
}
