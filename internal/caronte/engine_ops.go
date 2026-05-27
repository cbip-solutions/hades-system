//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package caronte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	caronteparser "github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/structure"
)

func (pe *projectEngine) decomposition(ctx context.Context) (structure.Decomposition, error) {
	pe.decompMu.Lock()
	defer pe.decompMu.Unlock()

	lastKey := ""
	if pe.decompOK {
		lastKey = pe.decomp.HashKey
	}
	dec, _, err := structure.Recompute(ctx, pe.store, lastKey)
	if err != nil {
		return structure.Decomposition{}, err
	}
	pe.decomp = dec
	pe.decompOK = true
	return pe.decomp, nil
}

// indexSkipDirs is the closed set of directory base names the IndexProject
// walker MUST NOT descend into. The list mirrors the plan §C-1:
// `.git`, `node_modules`, `.hades` (Caronte's own caronte.db lives there),
// `vendor`, `target`. Hidden directories (any name starting with ".") are
// also skipped — the dot-prefix check is applied in addition to this set so
// `.git` is rejected by both rules (defense in depth).
//
// The walker also skips files whose extension is not registered in the
// parser registry (extToLanguage in internal/caronte/parser/registry.go
// returns "" — the file is silently ignored, no error).
var indexSkipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".hades":       {},
	"vendor":       {},
	"target":       {},
}

func extToLanguageLabel(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".py", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

// IndexProject performs a full reindex of every source file under the
// project's canonical source root (resolved via Deps.RepoRootFor). Blocking:
// returns when the walk completes. Idempotent: a second call against an
// unchanged tree reports the same final NodesCreated / FilesIndexed totals
// (the parser indexer skips unchanged content_hashes via
// internal/caronte/parser/indexer.go::writeNodes::ContentHashFor — re-writes
// upsert rows in place; no orphan data accumulates).
//
// projectID MUST be canonical id_sha256 (64 hex) — the engine consumes the
// projects_alias resolution upstream ( ProjectsAliasResolver in the
// HTTP layer) and never sees aliases. Per spec §15, an unknown project
// degrades to ErrProjectUnavailable + an IndexReport with ProjectID echoed
// and Completed=false (NOT a panic).
//
// Walker skip-list (indexSkipDirs + dot-prefix): `.git`, `node_modules`,
// `.hades`, `vendor`, `target`, hidden dirs. Files whose extension is not
// registered with the parser registry are silently ignored (extToLanguage
// returns ""). Errors during a per-file parse are wrapped + propagated;
// Completed stays false in that case so callers can distinguish a partial
// walk from a clean empty pass.
//
// Returns IndexReport with totals, per-language file counts, duration, and
// Completed=true on a clean walk. invariant.
func (e *Engine) IndexProject(ctx context.Context, projectID string) (IndexReport, error) {
	started := time.Now()
	rep := IndexReport{
		ProjectID:      projectID,
		LanguageCounts: map[string]int{},
		StartedAt:      started,
	}
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {

		rep.DurationMillis = time.Since(started).Milliseconds()
		return rep, err
	}

	if pe.repoRoot == "" {
		rep.DurationMillis = time.Since(started).Milliseconds()
		return rep, fmt.Errorf("%w: %s: no canonical source root (RepoRootFor unwired or returned error)",
			ErrProjectUnavailable, projectID)
	}

	p, err := caronteparser.NewParser()
	if err != nil {
		rep.DurationMillis = time.Since(started).Milliseconds()
		return rep, fmt.Errorf("caronte: IndexProject: parser construct: %w", err)
	}
	indexer := caronteparser.NewIndexer(p, pe.store)
	defer indexer.Close()

	walkErr := filepath.WalkDir(pe.repoRoot, func(path string, d os.DirEntry, walkE error) error {
		if walkE != nil {

			return walkE
		}
		if d.IsDir() {
			base := d.Name()
			// Skip the root itself (base may equal the tempdir basename, which
			// we never want to skip; the dot-prefix and skip-list checks
			// SKIP the descent only — they do NOT short-circuit the root).
			if path == pe.repoRoot {
				return nil
			}
			if _, skip := indexSkipDirs[base]; skip {
				return filepath.SkipDir
			}

			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		lang := extToLanguageLabel(ext)
		if lang == "" {
			return nil
		}

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("caronte: IndexProject: read %s: %w", path, rerr)
		}

		relPath, relErr := filepath.Rel(pe.repoRoot, path)
		if relErr != nil {
			return fmt.Errorf("caronte: IndexProject: rel %s: %w", path, relErr)
		}
		ireport, ierr := indexer.IndexFile(ctx, relPath, src)
		if ierr != nil {

			if errors.Is(ierr, caronteparser.ErrUnsupportedLanguage) {
				return nil
			}
			return ierr
		}
		rep.FilesIndexed++
		rep.LanguageCounts[lang]++

		rep.NodesCreated += ireport.Written + ireport.Skipped
		return nil
	})
	rep.DurationMillis = time.Since(started).Milliseconds()
	if walkErr != nil {
		return rep, fmt.Errorf("caronte: IndexProject: walk %s: %w", pe.repoRoot, walkErr)
	}

	stats, _ := pe.resolver.ResolveProject(ctx, projectID, pe.repoRoot)
	rep.EdgesCreated = stats.CallEdges + stats.ImplementsEdges + stats.LLMHintEdges
	rep.Completed = true
	return rep, nil
}

func (e *Engine) Context(ctx context.Context, symbol, projectID string) (ContextResult, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return ContextResult{Symbol: symbol}, err
	}
	callers, err := pe.store.ListEdgesByTarget(ctx, symbol, store.EdgeCalls)
	if err != nil {
		return ContextResult{Symbol: symbol}, err
	}
	callees, err := pe.store.ListEdgesBySource(ctx, symbol, store.EdgeCalls)
	if err != nil {
		return ContextResult{Symbol: symbol}, err
	}
	res := ContextResult{Symbol: symbol}
	seen := map[string]struct{}{}
	for _, ed := range callers {
		if _, dup := seen[ed.SourceID]; !dup {
			res.Callers = append(res.Callers, ed.SourceID)
			seen[ed.SourceID] = struct{}{}
		}
	}
	seen = map[string]struct{}{}
	for _, ed := range callees {
		if _, dup := seen[ed.TargetID]; !dup {
			res.Callees = append(res.Callees, ed.TargetID)
			seen[ed.TargetID] = struct{}{}
		}
	}
	dec, derr := pe.decomposition(ctx)
	if derr == nil {
		res.Community = dec.PackageOf(symbol)
		res.Coreness = dec.CorenessOf(symbol)
		res.SCCID = dec.SCCOf(symbol)
		res.Cyclic = dec.IsCyclic(symbol)

		if res.Community != "" {
			for nodeID, pkg := range dec.PackageID {
				if pkg == res.Community && nodeID != symbol {
					res.Neighbors = append(res.Neighbors, nodeID)
				}
			}
			sort.Strings(res.Neighbors)
		}
	}
	sort.Strings(res.Callers)
	sort.Strings(res.Callees)
	return res, nil
}

func (e *Engine) BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (evolution.RiskScore, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return evolution.RiskScore{}, err
	}
	dec, err := pe.decomposition(ctx)
	if err != nil {
		return evolution.RiskScore{}, err
	}
	return evolution.BlastRadius(ctx, pe.store, pe.builder, dec,
		evolution.DefaultRiskWeights(), evolution.DefaultRiskThresholds(),
		projectID, changedSymbols, changedFiles)
}

func (e *Engine) GetWhy(ctx context.Context, projectID, subject string) (intent.WhyAnswer, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return intent.WhyAnswer{Subject: subject, Degraded: true}, err
	}
	return pe.intent.GetWhy(ctx, subject)
}

func (e *Engine) GetImplementations(ctx context.Context, interfaceID, projectID string) ([]semantic.Implementation, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return nil, err
	}
	impls, err := pe.resolver.GetImplementations(ctx, interfaceID)
	if err != nil {
		return nil, err
	}
	if impls == nil {
		impls = []semantic.Implementation{}
	}
	return impls, nil
}

func (e *Engine) TraceCallPath(ctx context.Context, rootID string, maxDepth int, projectID string) ([]semantic.CallPathHop, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return pe.resolver.TraceCallPath(ctx, rootID, maxDepth)
}

func (e *Engine) GetCoChange(ctx context.Context, file, projectID string) ([]CoChangePeer, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return nil, err
	}
	window := e.deps.Params.CoChangeParams(projectID).WindowDays
	couplings, err := pe.builder.ListCoupling(ctx, projectID, file, window)
	if err != nil {
		return nil, err
	}
	peers := make([]CoChangePeer, 0, len(couplings))
	for _, c := range couplings {
		peer := c.FileB
		if peer == file {
			peer = c.FileA
		}
		peers = append(peers, CoChangePeer{
			Path:            peer,
			CouplingPercent: c.CouplingPercent,
			SharedRevs:      c.SharedRevs,
			WindowDays:      c.WindowDays,
		})
	}

	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].CouplingPercent != peers[j].CouplingPercent {
			return peers[i].CouplingPercent > peers[j].CouplingPercent
		}
		return peers[i].Path < peers[j].Path
	})
	return peers, nil
}

func (e *Engine) GetHealth(ctx context.Context, projectID string) (HealthReport, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return HealthReport{ProjectID: projectID, Degraded: true}, err
	}
	h := HealthReport{ProjectID: projectID}
	langSeen := map[string]struct{}{}
	for _, k := range store.AllNodeKinds() {
		nodes, lerr := pe.store.ListNodesByKind(ctx, k)
		if lerr != nil {
			return HealthReport{ProjectID: projectID, Degraded: true}, lerr
		}
		h.NodeCount += len(nodes)
		for _, n := range nodes {
			if n.Language != "" {
				langSeen[n.Language] = struct{}{}
			}

			out, eerr := pe.store.ListEdgesBySource(ctx, n.NodeID, store.EdgeCalls)
			if eerr != nil {
				return HealthReport{ProjectID: projectID, Degraded: true}, eerr
			}
			h.EdgeCount += len(out)
		}
	}
	for lang := range langSeen {
		h.Languages = append(h.Languages, lang)
	}
	sort.Strings(h.Languages)
	if dec, derr := pe.decomposition(ctx); derr == nil {
		pkgs := map[string]struct{}{}
		for _, pkg := range dec.PackageID {
			pkgs[pkg] = struct{}{}
		}
		h.PackageCount = len(pkgs)
		cyclic := map[int]struct{}{}
		for nodeID := range dec.SCCID {
			if dec.IsCyclic(nodeID) {
				cyclic[dec.SCCOf(nodeID)] = struct{}{}
			}
		}
		h.CyclicSCCs = len(cyclic)
	}
	return h, nil
}

func (e *Engine) GetArchitecture(ctx context.Context, projectID string) (ArchitectureReport, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return ArchitectureReport{}, err
	}
	dec, err := pe.decomposition(ctx)
	if err != nil {
		return ArchitectureReport{}, err
	}
	pkgAgg := map[string]*PackageNode{}
	for nodeID, pkg := range dec.PackageID {
		pn := pkgAgg[pkg]
		if pn == nil {
			pn = &PackageNode{PackageID: pkg}
			pkgAgg[pkg] = pn
		}
		pn.NodeCount++
		if c := dec.CorenessOf(nodeID); c > pn.Coreness {
			pn.Coreness = c
		}
	}
	report := ArchitectureReport{}
	for _, pn := range pkgAgg {
		report.Packages = append(report.Packages, *pn)
	}
	sort.SliceStable(report.Packages, func(i, j int) bool {
		return report.Packages[i].PackageID < report.Packages[j].PackageID
	})
	sccMembers := map[int][]string{}
	for nodeID := range dec.SCCID {
		if dec.IsCyclic(nodeID) {
			id := dec.SCCOf(nodeID)
			sccMembers[id] = append(sccMembers[id], nodeID)
		}
	}
	for id, members := range sccMembers {
		sort.Strings(members)
		report.Cycles = append(report.Cycles, SCCGroup{SCCID: id, Members: members})
	}
	sort.SliceStable(report.Cycles, func(i, j int) bool {
		return report.Cycles[i].SCCID < report.Cycles[j].SCCID
	})
	return report, nil
}

func (e *Engine) Wiki(ctx context.Context, module, projectID string) (WikiDoc, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {
		return WikiDoc{Module: module}, err
	}
	dec, err := pe.decomposition(ctx)
	if err != nil {
		return WikiDoc{Module: module}, err
	}

	type sym struct {
		id       string
		coreness int
	}
	var syms []sym
	for nodeID, pkg := range dec.PackageID {
		if pkg == module {
			syms = append(syms, sym{id: nodeID, coreness: dec.CorenessOf(nodeID)})
		}
	}
	sort.SliceStable(syms, func(i, j int) bool {
		if syms[i].coreness != syms[j].coreness {
			return syms[i].coreness > syms[j].coreness
		}
		return syms[i].id < syms[j].id
	})
	md := "# " + module + "\n\n"
	md += fmt.Sprintf("%d symbols. Architectural hubs (by k-core coreness) first.\n\n", len(syms))
	for _, s := range syms {
		cyclic := ""
		if dec.IsCyclic(s.id) {
			cyclic = " (in a mutual-call cycle)"
		}
		md += fmt.Sprintf("- `%s` — coreness %d%s\n", s.id, s.coreness, cyclic)
	}
	return WikiDoc{Module: module, Markdown: md}, nil
}

const federationListLimit = 100

func (e *Engine) GetContract(_ context.Context, _, _ string) (ContractPayload, error) {

	return ContractPayload{}, ErrFederationUnavailable
}

func (e *Engine) GetConsumers(ctx context.Context, endpointID, workspaceID string) (ConsumerList, error) {
	if e.deps.FederationDB == nil {
		return ConsumerList{}, ErrFederationUnavailable
	}
	rows, err := e.deps.FederationDB.FederationListContractLinks(ctx, workspaceID, federationListLimit)
	if err != nil {
		return ConsumerList{}, err
	}
	out := ConsumerList{
		EndpointID:  endpointID,
		WorkspaceID: workspaceID,
		Consumers:   make([]ConsumerLink, 0, len(rows)),
	}
	for _, r := range rows {
		if r.EndpointID != endpointID {
			continue
		}
		out.EndpointRepo = r.EndpointRepo
		out.Consumers = append(out.Consumers, ConsumerLink{
			CallID:     r.CallID,
			Repo:       r.CallRepo,
			Confidence: r.Confidence,
			LinkMethod: r.LinkMethod,
		})
	}
	return out, nil
}

func (e *Engine) GetBreakingChanges(ctx context.Context, workspaceID string, sinceUnix int64) ([]BreakingChangePayload, error) {
	if e.deps.FederationDB == nil {
		return nil, ErrFederationUnavailable
	}
	rows, err := e.deps.FederationDB.FederationListRecentBreakingChanges(ctx, workspaceID, federationListLimit)
	if err != nil {
		return nil, err
	}
	out := make([]BreakingChangePayload, 0, len(rows))
	for _, r := range rows {
		if sinceUnix > 0 && r.DetectedAt < sinceUnix {
			continue
		}

		_, consumers, ferr := e.deps.FederationDB.FederationGetBreakingChangeWithConsumers(ctx, r.ChangeID)
		var fan []ConsumerLink
		if ferr == nil {
			fan = make([]ConsumerLink, 0, len(consumers))
			for _, c := range consumers {
				fan = append(fan, ConsumerLink{
					CallID: c.CallID,
					Repo:   c.CallRepo,
				})
			}
		}

		var adrRefs, supersedes []string
		if r.LoreADRRefs != "" {
			_ = decodeJSONArray(r.LoreADRRefs, &adrRefs)
		}
		if r.LoreSupersedes != "" {
			_ = decodeJSONArray(r.LoreSupersedes, &supersedes)
		}
		out = append(out, BreakingChangePayload{
			ChangeID:     r.ChangeID,
			WorkspaceID:  r.WorkspaceID,
			EndpointID:   r.EndpointID,
			EndpointRepo: r.EndpointRepo,
			Kind:         r.Kind,
			Detail:       json.RawMessage(r.Detail),
			DetectedAt:   r.DetectedAt,
			DetectorID:   r.DetectorID,
			Consumers:    fan,
			Lore: LoreAttributionPayload{
				Author:     r.LoreAuthor,
				CommitSHA:  r.LoreCommitSHA,
				ADRRefs:    adrRefs,
				Supersedes: supersedes,
			},
		})
	}
	return out, nil
}

func (e *Engine) TraceAPICall(ctx context.Context, callID, workspaceID string) (APICallTrace, error) {
	if e.deps.FederationDB == nil {
		return APICallTrace{}, ErrFederationUnavailable
	}
	rows, err := e.deps.FederationDB.FederationListContractLinks(ctx, workspaceID, federationListLimit)
	if err != nil {
		return APICallTrace{}, err
	}
	for _, r := range rows {
		if r.CallID != callID {
			continue
		}
		return APICallTrace{
			CallID:       r.CallID,
			CallRepo:     r.CallRepo,
			WorkspaceID:  r.WorkspaceID,
			EndpointID:   r.EndpointID,
			EndpointRepo: r.EndpointRepo,
			Confidence:   r.Confidence,
			LinkMethod:   r.LinkMethod,
			Unresolved:   false,
		}, nil
	}
	return APICallTrace{
		CallID:      callID,
		WorkspaceID: workspaceID,
		Unresolved:  true,
	}, nil
}

func (e *Engine) GetWorkspace(ctx context.Context, workspaceID string) (WorkspaceSnapshot, error) {
	if e.deps.FederationDB == nil {
		return WorkspaceSnapshot{}, ErrFederationUnavailable
	}
	wsRow, err := e.deps.FederationDB.FederationGetWorkspace(ctx, workspaceID)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	members, err := e.deps.FederationDB.FederationListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	memberIDs := make([]string, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.ProjectID)
	}
	return WorkspaceSnapshot{
		WorkspaceID:   wsRow.WorkspaceID,
		OwningProject: wsRow.OwningProject,
		Members:       memberIDs,
		PolicyLocked:  wsRow.PolicyLocked,
		CreatedAt:     wsRow.CreatedAt,
		SchemaVersion: wsRow.SchemaVersion,
	}, nil
}

func (e *Engine) FederationHealth(ctx context.Context, workspaceID string) (FederationHealthReport, error) {
	if e.deps.FederationDB == nil {
		return FederationHealthReport{
			WorkspaceID: workspaceID,
			Reachable:   false,
		}, ErrFederationUnavailable
	}
	out := FederationHealthReport{
		WorkspaceID: workspaceID,
		Reachable:   true,
	}
	if workspaceID != "" {

		links, err := e.deps.FederationDB.FederationListContractLinks(ctx, workspaceID, federationListLimit)
		if err == nil {
			out.ContractLinksCount = len(links)
		}
		brk, err := e.deps.FederationDB.FederationListRecentBreakingChanges(ctx, workspaceID, federationListLimit)
		if err == nil {
			out.BreakingChangesOpenCount = len(brk)
		}
		return out, nil
	}

	if _, err := e.deps.FederationDB.FederationListWorkspaces(ctx); err != nil {
		out.Reachable = false
		return out, err
	}
	return out, nil
}

func (e *Engine) ContractDiff(_ context.Context, endpointID string, sinceUnix int64) (ContractDiff, error) {
	if e.deps.FederationDB == nil {
		return ContractDiff{}, ErrFederationUnavailable
	}
	return ContractDiff{
		EndpointID: endpointID,
		SinceUnix:  sinceUnix,
		Severity:   "INSUFFICIENT",
	}, nil
}

func (e *Engine) GetWhyBreakingChange(ctx context.Context, changeID string) (WhyBreakingChange, error) {
	if e.deps.FederationDB == nil {
		return WhyBreakingChange{}, ErrFederationUnavailable
	}
	bc, _, err := e.deps.FederationDB.FederationGetBreakingChangeWithConsumers(ctx, changeID)
	if err != nil {
		return WhyBreakingChange{}, err
	}
	out := WhyBreakingChange{
		ChangeID:      bc.ChangeID,
		WorkspaceID:   bc.WorkspaceID,
		EndpointID:    bc.EndpointID,
		EndpointRepo:  bc.EndpointRepo,
		LoreAuthor:    bc.LoreAuthor,
		LoreCommitSHA: bc.LoreCommitSHA,
		DetectedAt:    bc.DetectedAt,
	}

	if bc.LoreADRRefs != "" {
		var refs []string
		if err := decodeJSONArray(bc.LoreADRRefs, &refs); err == nil {
			out.LoreADRRefs = refs
		}
	}
	if bc.LoreSupersedes != "" {
		var sup []string
		if err := decodeJSONArray(bc.LoreSupersedes, &sup); err == nil {
			out.LoreSupersedes = sup
		}
	}
	return out, nil
}
