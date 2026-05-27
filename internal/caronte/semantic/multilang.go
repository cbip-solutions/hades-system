// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"errors"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type MultiLangMode string

const (
	ModeSCIP MultiLangMode = "scip"

	ModeHeuristic MultiLangMode = "heuristic"
)

type MultiLangStats struct {
	Language       string
	Mode           MultiLangMode
	SCIPEdges      int
	HeuristicEdges int
	LLMHintEdges   int
	Unresolved     int
}

type MultiLangOpts struct {
	MaxTailSites  int
	EnableLLMTail bool
}

var errMultiLangNilStore = errors.New("caronte/semantic: MultiLangResolver has nil store (wiring bug)")

type MultiLangResolver struct {
	store      *store.Store
	runner     SCIPRunner
	dispatcher CaronteDispatcher
	opts       MultiLangOpts
}

func NewMultiLangResolver(s *store.Store, runner SCIPRunner, dispatcher CaronteDispatcher, opts MultiLangOpts) *MultiLangResolver {
	if opts.MaxTailSites <= 0 {
		opts.MaxTailSites = DefaultMaxTailSites
	}
	return &MultiLangResolver{store: s, runner: runner, dispatcher: dispatcher, opts: opts}
}

func (r *MultiLangResolver) ResolveLanguage(ctx context.Context, projectID, srcDir, language string) (MultiLangStats, error) {
	if r.store == nil {
		return MultiLangStats{}, errMultiLangNilStore
	}
	stats := MultiLangStats{Language: language}

	kind, hasIndexer := IndexerKindForLanguage(language)
	usedSCIP := false
	if hasIndexer && r.runner != nil && r.runner.Available(kind) {
		raw, err := r.runner.Index(ctx, kind, srcDir)
		if err == nil {

			lookup := func(file string, line int) (string, bool) {
				id, ok, lerr := r.store.GetNodeByPosition(ctx, file, line)
				if lerr != nil {
					return "", false
				}
				return id, ok
			}
			edges, perr := parseSCIPIndex(raw, language, lookup)
			if perr == nil {
				wrote, werr := r.writeEdges(ctx, edges)
				if werr != nil {
					return MultiLangStats{}, werr
				}
				stats.SCIPEdges = wrote
				stats.Mode = ModeSCIP
				usedSCIP = true
			}

		}

	}

	if !usedSCIP {

		stats.Mode = ModeHeuristic
		interfaces, err := r.store.ListNodesByKind(ctx, store.KindInterface)
		if err != nil {
			return MultiLangStats{}, err
		}
		structs, err := r.store.ListNodesByKind(ctx, store.KindStruct)
		if err != nil {
			return MultiLangStats{}, err
		}
		members, err := r.collectMembers(ctx)
		if err != nil {
			return MultiLangStats{}, err
		}
		interfaces = filterByLanguage(interfaces, language)
		structs = filterByLanguage(structs, language)
		edges := heuristicImplementsEdges(interfaces, structs, members)
		wrote, werr := r.writeEdges(ctx, edges)
		if werr != nil {
			return MultiLangStats{}, werr
		}
		stats.HeuristicEdges = wrote
	}

	if r.opts.EnableLLMTail && r.dispatcher != nil {
		unresolved, err := r.unresolvedInterfaces(ctx, language)
		if err != nil {
			return MultiLangStats{}, err
		}
		stats.Unresolved = len(unresolved)
		if len(unresolved) > 0 {
			n, tailErr := r.resolveMultiLangTail(ctx, language, unresolved)
			if tailErr == nil {
				stats.LLMHintEdges = n
			}
			// ErrNoDispatcher / Ollama-down: leave LLMHintEdges=0, do not fail.
		}
	}
	return stats, nil
}

func (r *MultiLangResolver) writeEdges(ctx context.Context, edges []store.Edge) (int, error) {
	written := 0
	for _, e := range edges {
		if err := r.store.UpsertEdge(ctx, e); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func (r *MultiLangResolver) collectMembers(ctx context.Context) ([]store.Node, error) {
	methods, err := r.store.ListNodesByKind(ctx, store.KindMethod)
	if err != nil {
		return nil, err
	}
	fields, err := r.store.ListNodesByKind(ctx, store.KindField)
	if err != nil {
		return nil, err
	}
	return append(methods, fields...), nil
}

func (r *MultiLangResolver) unresolvedInterfaces(ctx context.Context, language string) ([]unresolvedSite, error) {
	interfaces, err := r.store.ListNodesByKind(ctx, store.KindInterface)
	if err != nil {
		return nil, err
	}
	interfaces = filterByLanguage(interfaces, language)
	var sites []unresolvedSite
	for _, iface := range interfaces {
		impls, err := r.store.ListEdgesByTarget(ctx, iface.NodeID, store.EdgeImplements)
		if err != nil {
			return nil, err
		}
		if len(impls) == 0 {
			sites = append(sites, unresolvedSite{
				CallerID: iface.NodeID,
				SiteFile: iface.FilePath,
				SiteLine: iface.StartLine,
				Hint:     "interface/trait with no statically-resolved implementor",
			})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].CallerID < sites[j].CallerID })
	if len(sites) > r.opts.MaxTailSites {
		sites = sites[:r.opts.MaxTailSites]
	}
	return sites, nil
}

func filterByLanguage(nodes []store.Node, language string) []store.Node {
	if language == "" {
		return nodes
	}
	out := nodes[:0:0]
	for _, n := range nodes {
		if n.Language == language {
			out = append(out, n)
		}
	}
	return out
}
