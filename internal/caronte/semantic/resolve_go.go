// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

type ResolverOpts struct {
	MaxTailSites int

	PreferStaleSnapshot bool
}

const DefaultMaxTailSites = 32

// Resolver is Caronte's on-demand L2 resolver for Go. It is constructed per
// engine (shared across projects) and holds the injected store + the C-2
// dispatcher seam. NOT safe for the store to be nil. dispatcher MAY be nil
// (static-only; the LLM tail is skipped — §15).
//
// Scheduling contract (§21): callers MUST invoke ResolveProject on-demand
// (per query / per package, cached by the engine), NEVER during initial
// indexing — the go/types cold load is 10-60 s on 500 k LOC.
type Resolver struct {
	store      *store.Store
	dispatcher CaronteDispatcher
	opts       ResolverOpts
}

func NewResolver(s *store.Store, dispatcher CaronteDispatcher, opts ResolverOpts) *Resolver {
	if opts.MaxTailSites <= 0 {
		opts.MaxTailSites = DefaultMaxTailSites
	}
	return &Resolver{store: s, dispatcher: dispatcher, opts: opts}
}

type unresolvedSite struct {
	CallerID string
	SiteFile string
	SiteLine int
	Hint     string
}

func (r *Resolver) ResolveProject(ctx context.Context, projectID, srcDir string) (ResolutionStats, error) {
	if r.store == nil {
		return ResolutionStats{}, fmt.Errorf("caronte/semantic: ResolveProject: nil store (wiring bug)")
	}
	load, err := loadGoPackages(ctx, srcDir)
	if err != nil {

		return ResolutionStats{}, fmt.Errorf("caronte/semantic: ResolveProject %q: %w", projectID, err)
	}
	ssaIn, err := buildSSA(load.Packages)
	if err != nil {
		return ResolutionStats{}, fmt.Errorf("caronte/semantic: ResolveProject %q: buildSSA: %w", projectID, err)
	}

	var (
		callEdges []store.Edge
		implEdges []store.Edge
		mode      ResolveMode
		conf      store.Confidence
		reachSet  map[string]bool
	)
	if load.Buildable {
		mode = ModeVTA
		conf = store.ConfExactVTA
		g := buildVTACallGraph(ssaIn)
		callEdges = callEdgesFromGraph(g, ssaIn.modulePrefix)
		reachSet = reachableNodeIDs(ssaIn)
	} else {

		mode = ModeCHA
		conf = store.ConfExactCHA
		g := buildCHACallGraph(ssaIn)
		callEdges = callEdgesFromCHA(g, ssaIn.modulePrefix)
		reachSet = nil
	}
	implEdges = interfaceImplementsEdges(load.Packages, conf, reachSet)

	wroteCalls, err := r.writeEdges(ctx, callEdges)
	if err != nil {
		return ResolutionStats{}, err
	}
	wroteImpls, err := r.writeEdges(ctx, implEdges)
	if err != nil {
		return ResolutionStats{}, err
	}

	cand := r.collectUnresolved(callEdges, reachSet)
	llmEdges := 0
	if len(cand) > 0 {
		n, tailErr := r.resolveTail(ctx, cand)
		if tailErr == nil {
			llmEdges = n
		}
		// ErrNoDispatcher / Ollama-down: leave llmEdges=0, do not fail.
	}

	return ResolutionStats{
		CallEdges:       wroteCalls,
		ImplementsEdges: wroteImpls,
		LLMHintEdges:    llmEdges,
		ResolvedFuncs:   len(ssaIn.funcs),
		UnresolvedSites: len(cand),
		Mode:            mode,
		Stale:           false,
	}, nil
}

func (r *Resolver) writeEdges(ctx context.Context, edges []store.Edge) (int, error) {
	for i, e := range edges {
		if err := r.store.UpsertEdge(ctx, e); err != nil {
			return i, fmt.Errorf("caronte/semantic: writeEdges[%d] %s→%s: %w", i, e.SourceID, e.TargetID, err)
		}
	}
	return len(edges), nil
}

func (r *Resolver) collectUnresolved(callEdges []store.Edge, reachSet map[string]bool) []unresolvedSite {

	var cand []unresolvedSite
	for _, e := range callEdges {
		if e.Kind != string(store.EdgeInvoke) {
			continue
		}
		if reachSet != nil && !reachSet[e.TargetID] {
			cand = append(cand, unresolvedSite{
				CallerID: e.SourceID,
				SiteFile: e.SiteFile,
				SiteLine: e.SiteLine,
				Hint:     "interface dispatch with target outside reachable set",
			})
		}
	}
	sort.Slice(cand, func(i, j int) bool {
		if cand[i].CallerID != cand[j].CallerID {
			return cand[i].CallerID < cand[j].CallerID
		}
		return cand[i].SiteLine < cand[j].SiteLine
	})
	if len(cand) > r.opts.MaxTailSites {
		cand = cand[:r.opts.MaxTailSites]
	}
	return cand
}

type llmTailRequest struct {
	Task  string           `json:"task"`
	Sites []unresolvedSite `json:"sites"`
}

// resolveTail sends the bounded set of unresolved call sites to the LLM via
// the C-2 single-egress seam
// and records any high-confidence disambiguation as a ConfLLMHint edge.
// Returns the count of llm_hint edges written.
//
// A nil dispatcher ⇒ ErrNoDispatcher (the caller treats the tail as skipped,
// §15). A dispatcher error (Ollama down) is propagated; ResolveProject
// swallows it (degrade, do not block). This method NEVER dials a backend
// directly — every LLM call is dispatcher.Forward.
func (r *Resolver) resolveTail(ctx context.Context, sites []unresolvedSite) (int, error) {
	if r.dispatcher == nil {
		return 0, ErrNoDispatcher
	}
	if len(sites) == 0 {
		return 0, nil
	}
	body, err := json.Marshal(llmTailRequest{
		Task:  "Resolve the concrete callee(s) for each ambiguous Go call site. Reply with high-confidence resolutions only.",
		Sites: sites,
	})
	if err != nil {
		return 0, fmt.Errorf("caronte/semantic: resolveTail marshal: %w", err)
	}
	call := orchestrator.Call{
		Profile: DefaultLLMProfile,
		Method:  "POST",
		Path:    "/v1/messages",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	}
	resp, err := r.dispatcher.Forward(ctx, call)
	if err != nil {
		return 0, fmt.Errorf("caronte/semantic: resolveTail dispatch: %w", err)
	}

	resolutions := parseTailResolutions(resp.Body)
	written := 0
	for _, res := range resolutions {
		edge := store.Edge{
			SourceID:   res.FromID,
			TargetID:   res.ToID,
			Kind:       string(store.EdgeInvoke),
			Confidence: store.ConfLLMHint,
			SiteFile:   res.SiteFile,
			SiteLine:   res.SiteLine,
		}
		if edge.SourceID == "" || edge.TargetID == "" {
			continue
		}
		if err := r.store.UpsertEdge(ctx, edge); err != nil {
			return written, fmt.Errorf("caronte/semantic: resolveTail write %s→%s: %w", edge.SourceID, edge.TargetID, err)
		}
		written++
	}
	return written, nil
}

type tailResolution struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	SiteFile string `json:"site_file"`
	SiteLine int    `json:"site_line"`
}

func parseTailResolutions(respBody []byte) []tailResolution {
	if len(respBody) == 0 {
		return nil
	}

	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil
	}
	var out []tailResolution
	for _, c := range env.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}
		var wrap struct {
			Resolutions []tailResolution `json:"resolutions"`
		}
		if err := json.Unmarshal([]byte(c.Text), &wrap); err == nil {
			out = append(out, wrap.Resolutions...)
		}
	}
	return out
}

func (r *Resolver) GetImplementations(ctx context.Context, interfaceID string) ([]Implementation, error) {
	if r.store == nil {
		return nil, fmt.Errorf("caronte/semantic: GetImplementations: nil store")
	}
	edges, err := r.store.ListEdgesBySource(ctx, interfaceID, store.EdgeImplements)
	if err != nil {
		return nil, fmt.Errorf("caronte/semantic: GetImplementations %q: %w", interfaceID, err)
	}
	out := make([]Implementation, 0, len(edges))
	for _, e := range edges {
		reachable := false
		if e.Reachable != nil {
			reachable = *e.Reachable
		}
		out = append(out, Implementation{
			InterfaceID: e.SourceID,
			ImplID:      e.TargetID,
			Confidence:  string(e.Confidence),
			Reachable:   reachable,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImplID < out[j].ImplID })
	return out, nil
}

func (r *Resolver) TraceCallPath(ctx context.Context, symbolID string, maxDepth int) ([]CallPathHop, error) {
	if r.store == nil {
		return nil, fmt.Errorf("caronte/semantic: TraceCallPath: nil store")
	}
	if maxDepth <= 0 {
		maxDepth = 1
	}
	type queued struct {
		id    string
		depth int
	}
	visited := map[string]bool{symbolID: true}
	queue := []queued{{symbolID, 0}}
	var hops []CallPathHop
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}
		for _, kind := range []store.EdgeKind{store.EdgeCalls, store.EdgeInvoke} {
			edges, err := r.store.ListEdgesBySource(ctx, cur.id, kind)
			if err != nil {
				return nil, fmt.Errorf("caronte/semantic: TraceCallPath %q: %w", cur.id, err)
			}
			for _, e := range edges {
				hops = append(hops, CallPathHop{
					FromID:     e.SourceID,
					ToID:       e.TargetID,
					Confidence: string(e.Confidence),
					SiteFile:   e.SiteFile,
					SiteLine:   e.SiteLine,
					Depth:      cur.depth + 1,
				})
				if !visited[e.TargetID] {
					visited[e.TargetID] = true
					queue = append(queue, queued{e.TargetID, cur.depth + 1})
				}
			}
		}
	}
	return hops, nil
}
