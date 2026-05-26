// SPDX-License-Identifier: MIT
// cmd/zen-swarm-ctld/caronte_wiring.go
//
// Caronte engine (Plan 19 sovereign in-process replacement for the former gitnexus subprocess, DECISION 1/2).
//
// This file is the daemon's composition root for caronte: it is ALLOWED to
// import internal/caronte, internal/caronte/*, internal/daemon/caronte*,
// internal/orchestrator, internal/orchestrator/merge, and
// internal/research/ecosystem. The composition root is the ONLY layer that
// imports concretes from all sides; the intermediate layers see only narrow
// seam interfaces (inv-zen-031).
//
// Three public helpers exported to main.go (J-10):
//
//   - buildCaronteEngine(caronteWiringDeps) (*caronte.Engine, error)
//     Assembles caronte.Deps from the daemon's real substrate and calls
//     caronte.NewEngine. main.go os.Exit(1)s on error (bootstrap-required,
//     generalises inv-zen-206).
//
//   - caronteOrchVerdictAdapter  — satisfies orchestrator.BlastRadiusProvider
//
//   - caronteMergeVerdictAdapter — satisfies merge.BlastRadiusScorer
//     Both map evolution.RiskScore → the local Verdict type (DECISION 3:
//     one adapter type CANNOT have two BlastRadius methods returning different
//     Verdict types, so the mapping is split across two tiny wrappers over a
//     shared caronteBlastRadiusCore).
//
//   - newCaronteSubsystem(*mcpgateway.CaronteProxy) *caronteSubsystem
//     Wraps the proxy as a gateway Subsystem named "caronte" (Plan 19 Phase L
//     renamed the segment gitnexus->caronte; RBAC/REST/augment lanes all moved
//     to the "caronte" segment in the same atomic cutover).
package main

import (
	"context"
	"database/sql"

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/caronteadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/caronteembedadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	daemon_orch "github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// caronteWiringDeps bundles the daemon substrate buildCaronteEngine needs. The
// caller (main.go) supplies the already-constructed pieces: the daemon DB for
// the caronteadapter, the orchestrator (satisfies semantic.CaronteDispatcher),
// the Jina/BGE implementations (optional: nil bge → KNN-order, no reranker),
// the audit emitter, and the optional project repo-root resolver.
//
// Fields are concrete pointer types sourced from the daemon's real substrate.
// Tests that do NOT call buildCaronteEngine (e.g. J-9 Verdict adapter tests)
// never instantiate this struct — they go straight to the adapter types.
type caronteWiringDeps struct {
	daemonDB    *sql.DB
	orch        *daemon_orch.Orchestrator
	jina        *ecosystem.JinaCodeEmbeddings
	bge         *ecosystem.BGEReRankerV2M3
	audit       func(eventType string, payload []byte)
	repoRootFor func(ctx context.Context, projectID string) (string, error)
}

func buildCaronteEngine(deps caronteWiringDeps) (*caronte.Engine, error) {
	adapter := caronteadapter.NewAdapterFromDB(deps.daemonDB)
	d := caronte.Deps{
		OpenProjectDB: adapter.OpenProjectDB,
		Dispatcher:    deps.orch,
		Embedder:      caronteembedadapter.NewEmbedder(deps.jina),
		AuditEmit:     deps.audit,
		Params:        staticParamsAccessor{params: evolution.DefaultParams()},
		IntentParams:  intent.DefaultIntentParams(intent.IntentParams{}),
		RepoRootFor:   deps.repoRootFor,
	}

	if deps.bge != nil {
		d.Reranker = caronteembedadapter.NewReranker(deps.bge)
	}
	return caronte.NewEngine(d)
}

type staticParamsAccessor struct{ params evolution.Params }

func (s staticParamsAccessor) CoChangeParams(string) evolution.Params { return s.params }

var _ evolution.ParamsAccessor = staticParamsAccessor{}

type caronteBlastRadiusCore interface {
	blastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (evolution.RiskScore, error)
}

type engineBlastCore struct{ engine *caronte.Engine }

func (c engineBlastCore) blastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (evolution.RiskScore, error) {
	return c.engine.BlastRadius(ctx, projectID, changedSymbols, changedFiles)
}

type caronteOrchVerdictAdapter struct{ core caronteBlastRadiusCore }

var _ orch.BlastRadiusProvider = caronteOrchVerdictAdapter{}

func (a caronteOrchVerdictAdapter) BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (orch.Verdict, error) {
	rs, err := a.core.blastRadius(ctx, projectID, changedSymbols, changedFiles)
	if err != nil {
		return orch.Verdict{}, err
	}
	return orch.Verdict{
		Level:       rs.Level,
		Score:       rs.Score,
		TopAffected: rs.TopAffected,
	}, nil
}

type caronteMergeVerdictAdapter struct{ core caronteBlastRadiusCore }

var _ merge.BlastRadiusScorer = caronteMergeVerdictAdapter{}

func (a caronteMergeVerdictAdapter) BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (merge.Verdict, error) {
	rs, err := a.core.blastRadius(ctx, projectID, changedSymbols, changedFiles)
	if err != nil {
		return merge.Verdict{}, err
	}
	return merge.Verdict{
		Level:       rs.Level,
		Score:       rs.Score,
		TopAffected: rs.TopAffected,
	}, nil
}

type caronteSubsystem struct {
	proxy *mcpgateway.CaronteProxy
	tools []mcpgateway.ToolEntry
}

func newCaronteSubsystem(p *mcpgateway.CaronteProxy) *caronteSubsystem {
	names := p.Tools()
	tools := make([]mcpgateway.ToolEntry, 0, len(names))
	for _, tn := range names {
		tools = append(tools, mcpgateway.ToolEntry{
			Name:    tn,
			Handler: p.CallByTool,
			Meta: mcpgateway.ToolMeta{
				Description: "caronte code-graph engine — " + tn.Tool(),
				InputSchema: caronteInputSchema(tn.Tool()),
			},
		})
	}
	return &caronteSubsystem{proxy: p, tools: tools}
}

func (c *caronteSubsystem) Name() string { return "caronte" }

func (c *caronteSubsystem) Tools() []mcpgateway.ToolEntry { return c.tools }

func caronteInputSchema(tool string) map[string]any {

	prop := func(keys ...string) map[string]any {
		props := map[string]any{}
		for _, k := range keys {
			props[k] = map[string]any{"type": "string"}
		}
		return map[string]any{"type": "object", "properties": props}
	}
	switch tool {
	case "query":
		return withRequired(prop("query", "project_id"), "query")
	case "context", "get_why", "trace_call_path":
		return withRequired(prop("symbol", "project_id"), "symbol")
	case "impact", "get_risk":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"changed_symbols": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"changed_files":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"project_id":      map[string]any{"type": "string"},
			},
		}
	case "get_implementations":
		return withRequired(prop("interface", "project_id"), "interface")
	case "get_cochange":
		return withRequired(prop("file", "project_id"), "file")
	case "wiki":
		return prop("module", "project_id")
	default:
		return prop("project_id")
	}
}

func withRequired(schema map[string]any, required ...string) map[string]any {
	schema["required"] = required
	return schema
}

type caronteEngineDaemonAdapter struct {
	engine *caronte.Engine
}

func (a caronteEngineDaemonAdapter) CodeGraph(ctx context.Context, query, projectID string) (research.CodeGraphResult, error) {
	return a.engine.CodeGraph(ctx, query, projectID)
}

func (a caronteEngineDaemonAdapter) IndexProject(ctx context.Context, projectID string) (handlers.CaronteReindexReport, error) {
	rep, err := a.engine.IndexProject(ctx, projectID)
	out := handlers.CaronteReindexReport{
		ProjectID:      rep.ProjectID,
		NodesCreated:   rep.NodesCreated,
		EdgesCreated:   rep.EdgesCreated,
		FilesIndexed:   rep.FilesIndexed,
		LanguageCounts: rep.LanguageCounts,
		DurationMillis: rep.DurationMillis,
		StartedAt:      rep.StartedAt,
		Completed:      rep.Completed,
	}
	if out.LanguageCounts == nil {

		out.LanguageCounts = map[string]int{}
	}
	return out, err
}

func (a caronteEngineDaemonAdapter) Close() error {
	return a.engine.Close()
}

var _ daemon.CaronteEngineForDaemon = caronteEngineDaemonAdapter{}

func newCaronteEngineDaemonAdapter(e *caronte.Engine) caronteEngineDaemonAdapter {
	return caronteEngineDaemonAdapter{engine: e}
}

type caronteAliasResolverDaemonAdapter struct {
	resolver mcpgateway.ProjectsAliasResolver
}

func (a caronteAliasResolverDaemonAdapter) Resolve(ctx context.Context, idOrAlias string) (string, error) {
	id, err := a.resolver.Resolve(ctx, idOrAlias)
	if err != nil {

		if isAliasNotFound(err) {
			return "", handlers.ErrCaronteAliasNotFound
		}
		return "", err
	}
	return id, nil
}

func isAliasNotFound(err error) bool {
	return err != nil && (err == mcpgateway.ErrAliasNotFound)
}

var _ daemon.ProjectsAliasResolverForDaemon = caronteAliasResolverDaemonAdapter{}

func newCaronteAliasResolverDaemonAdapter(r mcpgateway.ProjectsAliasResolver) caronteAliasResolverDaemonAdapter {
	return caronteAliasResolverDaemonAdapter{resolver: r}
}
