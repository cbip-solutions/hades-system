// SPDX-License-Identifier: MIT
// dispatch.go — fan-out parallel research dispatch (Q4 C deterministic
// core) + aggregator URL-keyed dedup + min-source threshold +
// citation-verification gate.
//
// Architecture
// - DispatcherImpl is the concrete Dispatcher.
// - Dispatch fans out to {WebSearch, Arxiv, GitHub, Code Graph,
// Ecosystem Docs} in parallel via sync.WaitGroup. (Originally
// used golang.org/x/sync/errgroup but per-backend goroutines
// unconditionally returned nil — g.Wait was guaranteed-nil dead
// code and the cancel-on-first-error semantic was never wanted;
// fix C-2 + C-19 in post-review I-2 swapped to WaitGroup +
// explicit error capture.)
// - Per-backend errors are recorded as soft-fail (other backends
// keep running) to maximize coverage; only the empty-aggregate
// case becomes a hard error, and the error message + audit
// payload include the per-backend root causes (C-3).
// - Aggregator deduplicates by canonicalized URL key and applies a
// min-source threshold (doctrine-tunable; default 1).
// - Raw hits become RawCitation seeds; CiteService.Verify converts
// to VerifiedCitation, enforcing invariant at the type level.
// - Every per-backend dispatch is preceded by BudgetClient.PreCall
// (invariant anchor; CI grep rule enforces presence).
//
// Budget axis convention:
// - PreCall axis is "stage" with value "research:<tool>". The four
// daemon-recognised axes (project | doctrine | stage | worker_id)
// are documented in internal/daemon/handlers/budget_plan4.go
// BudgetCapStatus. Research-MCP costs attribute to the "stage"
// axis (the research stage of the parent workflow). Using any
// non-recognised axis (e.g. "operation") would cause the daemon
// handler to silently NoOp the gate (C-7, post-review I-2 fix).
// The compliance test
// internal/mcp/research/precall_axis_test.go pins this contract.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

type DispatcherOptions struct {
	WebSearch    WebSearchBackend
	Arxiv        ArxivBackend
	GitHub       GitHubBackend
	Ecosystem    EcosystemBackend
	Gitnexus     GitnexusClient
	Synthesizer  Synthesizer
	Cite         CiteService
	BudgetClient BudgetClient
	AuditClient  AuditClient
	Cache        CacheClient

	MinSourceThreshold int

	MaxResultsPerBackend int

	DefaultProjectID string
}

type DispatcherImpl struct {
	opts DispatcherOptions
}

func NewDispatcher(opts DispatcherOptions) *DispatcherImpl {
	if opts.MinSourceThreshold == 0 {
		opts.MinSourceThreshold = 1
	}
	if opts.MaxResultsPerBackend == 0 {
		opts.MaxResultsPerBackend = 10
	}
	return &DispatcherImpl{opts: opts}
}

var _ Dispatcher = (*DispatcherImpl)(nil)

func (d *DispatcherImpl) Dispatch(ctx context.Context, q DispatchQuery) (DispatchResult, error) {
	if strings.TrimSpace(q.Query) == "" {
		return DispatchResult{}, fmt.Errorf("dispatch: empty query")
	}
	maxPer := q.MaxResultsPer
	if maxPer <= 0 {
		maxPer = d.opts.MaxResultsPerBackend
	}

	results := make([]backendResult, 0, 5)
	var resultsMu sync.Mutex

	var wg sync.WaitGroup

	runBackend := func(source string, scope, value string, est float64, fn func(context.Context) ([]SourceHit, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !d.preCheck(ctx, scope, value, est) {
				resultsMu.Lock()
				results = append(results, backendResult{source: source, hits: nil, err: errBudgetBlocked})
				resultsMu.Unlock()
				return
			}
			hits, err := fn(ctx)
			resultsMu.Lock()
			results = append(results, backendResult{source: source, hits: hits, err: err})
			resultsMu.Unlock()
		}()
	}

	if d.opts.WebSearch != nil {
		runBackend("web_search", "stage", "research:web_search", 0.05, func(c context.Context) ([]SourceHit, error) {
			return d.opts.WebSearch.Search(c, q.Query, maxPer)
		})
	}
	if d.opts.Arxiv != nil {
		runBackend("arxiv", "stage", "research:arxiv", 0.01, func(c context.Context) ([]SourceHit, error) {
			return d.opts.Arxiv.Search(c, q.Query, maxPer, "relevance")
		})
	}
	if d.opts.GitHub != nil {
		runBackend("github_search", "stage", "research:github_search", 0.01, func(c context.Context) ([]SourceHit, error) {
			return d.opts.GitHub.Search(c, q.Query, "", 0)
		})
	}
	if d.opts.Ecosystem != nil {
		runBackend("ecosystem_docs", "stage", "research:ecosystem_docs", 0.001, func(c context.Context) ([]SourceHit, error) {

			return d.opts.Ecosystem.Search(c, q.Query, "go")
		})
	}
	if d.opts.Gitnexus != nil {
		runBackend("code_graph", "stage", "research:code_graph", 0.001, func(c context.Context) ([]SourceHit, error) {
			res, err := d.opts.Gitnexus.CodeGraph(c, q.Query, d.opts.DefaultProjectID)
			return codeGraphToHits(res), err
		})
	}

	wg.Wait()

	for _, r := range results {
		if r.err != nil && r.err != errBudgetBlocked {
			d.emitAuditJSON(ctx, "dispatch-soft-fail", map[string]any{
				"source": r.source,
				"err":    r.err.Error(),
			})
		}
	}

	merged, sourceCount := aggregateAndDedup(results)
	if sourceCount < d.opts.MinSourceThreshold {

		errsBySource := make(map[string]string, len(results))
		errMsgs := make([]string, 0, len(results))
		for _, r := range results {
			if r.err != nil {
				errsBySource[r.source] = r.err.Error()
				errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", r.source, r.err.Error()))
			}
		}
		d.emitAuditJSON(ctx, "dispatch-no-source", map[string]any{
			"threshold":      d.opts.MinSourceThreshold,
			"got_sources":    sourceCount,
			"errs_by_source": errsBySource,
		})
		if len(errMsgs) > 0 {
			return DispatchResult{}, fmt.Errorf(
				"dispatch: %d source(s) contributed; threshold %d not met; per-backend errors: %s",
				sourceCount, d.opts.MinSourceThreshold, strings.Join(errMsgs, "; "))
		}
		return DispatchResult{}, fmt.Errorf(
			"dispatch: %d source(s) contributed; threshold %d not met (no per-backend errors recorded)",
			sourceCount, d.opts.MinSourceThreshold)
	}

	rawCites := make([]RawCitation, 0, len(merged))
	for _, h := range merged {
		if h.URL == "" {
			continue
		}
		rawCites = append(rawCites, RawCitation{
			SourceID: h.Source + ":" + h.URL,
			URL:      h.URL,
			Title:    h.Title,
		})
	}
	verified, err := d.opts.Cite.Verify(ctx, rawCites)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("dispatch: cite verify: %w", err)
	}

	// Strip findings whose URL was not verified.
	//
	// C-4 invariant (post-review I-2): the verified-URL match below
	// uses h.URL as the lookup key. This relies on aggregateAndDedup
	// keeping ONE raw URL form per canonical key — i.e. the URL
	// stored on the surviving SourceHit must be the same string
	// passed to CiteService.Verify. If aggregateAndDedup ever
	// re-canonicalises h.URL on the way out (e.g. lower-cases the
	// host on the surviving hit), this match silently drops the hit.
	// Do not change aggregateAndDedup to mutate h.URL post-merge
	// without updating both the cite-verify call (using the canonical
	// key as RawCitation.URL) AND this lookup.
	verifiedURLs := make(map[string]struct{}, len(verified))
	for _, v := range verified {
		verifiedURLs[v.URL] = struct{}{}
	}
	stripped := make([]SourceHit, 0, len(merged))
	for _, h := range merged {
		if h.URL == "" {

			stripped = append(stripped, h)
			continue
		}
		if _, ok := verifiedURLs[h.URL]; ok {
			stripped = append(stripped, h)
		}
	}

	return DispatchResult{
		Findings:   stripped,
		Citations:  verified,
		Iterations: 1,
	}, nil
}

func (d *DispatcherImpl) preCheck(ctx context.Context, scope, value string, est float64) bool {
	if d.opts.BudgetClient == nil {
		return true
	}
	allowed, _, err := d.opts.BudgetClient.PreCall(ctx, scope, value, est)
	return err == nil && allowed
}

var errBudgetBlocked = fmt.Errorf("budget pre-check denied")

func (d *DispatcherImpl) emitAudit(ctx context.Context, eventType string, payload []byte) {
	if d.opts.AuditClient == nil {
		return
	}
	_ = d.opts.AuditClient.Emit(ctx, eventType, payload)
}

func (d *DispatcherImpl) emitAuditJSON(ctx context.Context, eventType string, payload map[string]any) {
	if d.opts.AuditClient == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {

		body, _ = json.Marshal(map[string]string{"marshal_error": err.Error()})
	}
	_ = d.opts.AuditClient.Emit(ctx, eventType, body)
}

type backendResult struct {
	source string
	hits   []SourceHit
	err    error
}

func aggregateAndDedup(results []backendResult) ([]SourceHit, int) {
	seen := make(map[string]int)
	out := make([]SourceHit, 0)
	sources := make(map[string]struct{})
	for _, r := range results {
		if len(r.hits) == 0 {
			continue
		}
		sources[r.source] = struct{}{}
		for _, h := range r.hits {
			key := canonicalURL(h.URL)
			if key == "" {

				out = append(out, h)
				continue
			}
			if existing, ok := seen[key]; ok {

				if h.Score > out[existing].Score {
					out[existing] = h
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, h)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out, len(sources)
}

func canonicalURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	canon := u.String()
	canon = strings.TrimRight(canon, "/")
	return canon
}

func codeGraphToHits(res CodeGraphResult) []SourceHit {
	out := make([]SourceHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, SourceHit{
			Source: "code_graph",
			URL:    h.URL,
			Title:  h.Node,
			Score:  h.Score,
		})
	}
	return out
}
