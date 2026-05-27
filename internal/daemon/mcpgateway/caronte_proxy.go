// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/caronte_proxy.go
//
// CaronteProxy — gateway facade over the in-daemon Caronte engine (
// + L). The SOLE code-graph proxy: deleted the old gitnexus
// subprocess proxy + the gitnexus subprocess client; this proxy fronts the
// in-process engine under the "caronte" wire segment. The proxy:
//
// 1. Wraps a narrow CaronteEngine interface (declared here) so the daemon
// composition root wires the concrete *caronte.Engine without this package
// importing internal/caronte's constructor (invariant; the package does
// import caronte's result/return VALUE types — a one-way lower-layer
// import, fine). The engine satisfies CaronteEngine + research.GitnexusClient
// (the research interface name is the stable drop-in contract — DECISION L-3).
// 2. Exposes the 11 C-8 tools (query·context·impact·wiki·get_risk·get_why·
// get_health·trace_call_path·get_cochange·get_implementations·
// get_architecture) — all under the "caronte" subsystem segment
// ( renamed the segment gitnexus->caronte; the REST adapter,
// RBAC, and augment lanes all dispatch the caronte_* wire names).
// query/context/impact are GENUINELY DISTINCT real ops (DECISION 6), not
// aliases.
// 3. PRESERVES the per-mode escalate() semantics (Q7=B): autonomy →
// WAITING_FOR_CONFIRMATION, afk → cached_summary, interactive →
// doctor_warning.
// 4. Bootstrap-required: a nil engine → EnsureReachable returns
// ErrCaronteBootstrapRequired so daemon boot fails fast (os.Exit(1)).
package mcpgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type CaronteEngine interface {
	CodeGraph(ctx context.Context, query, projectID string) (research.CodeGraphResult, error)
	Context(ctx context.Context, symbol, projectID string) (caronte.ContextResult, error)
	BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (evolution.RiskScore, error)
	GetWhy(ctx context.Context, projectID, subject string) (intent.WhyAnswer, error)
	GetImplementations(ctx context.Context, interfaceID, projectID string) ([]semantic.Implementation, error)
	TraceCallPath(ctx context.Context, rootID string, maxDepth int, projectID string) ([]semantic.CallPathHop, error)
	GetCoChange(ctx context.Context, file, projectID string) ([]caronte.CoChangePeer, error)
	GetHealth(ctx context.Context, projectID string) (caronte.HealthReport, error)
	GetArchitecture(ctx context.Context, projectID string) (caronte.ArchitectureReport, error)
	Wiki(ctx context.Context, module, projectID string) (caronte.WikiDoc, error)
	GetContract(ctx context.Context, endpointID, projectID string) (caronte.ContractPayload, error)
	GetConsumers(ctx context.Context, endpointID, workspaceID string) (caronte.ConsumerList, error)
	GetBreakingChanges(ctx context.Context, workspaceID string, sinceUnix int64) ([]caronte.BreakingChangePayload, error)
	TraceAPICall(ctx context.Context, callID, workspaceID string) (caronte.APICallTrace, error)
	GetWorkspace(ctx context.Context, workspaceID string) (caronte.WorkspaceSnapshot, error)
	FederationHealth(ctx context.Context, workspaceID string) (caronte.FederationHealthReport, error)
	ContractDiff(ctx context.Context, endpointID string, sinceUnix int64) (caronte.ContractDiff, error)
	GetWhyBreakingChange(ctx context.Context, changeID string) (caronte.WhyBreakingChange, error)
	Close() error
}

var caronteToolNames = []string{
	"query", "context", "impact", "wiki",
	"get_risk", "get_why", "get_health",
	"trace_call_path", "get_cochange",
	"get_implementations", "get_architecture",
	"get_contract", "get_consumers", "get_breaking_changes", "trace_api_call",
	"get_workspace", "federation_health", "contract_diff", "get_why_breaking_change",
}

type CaronteProxy struct {
	engine CaronteEngine
	audit  AuditEmitter
}

func NewCaronteProxy(engine CaronteEngine, audit AuditEmitter) *CaronteProxy {
	if audit == nil {
		audit = NopAuditEmitter()
	}
	return &CaronteProxy{engine: engine, audit: audit}
}

func (p *CaronteProxy) EnsureReachable(_ context.Context) error {
	if p.engine == nil {
		return ErrCaronteBootstrapRequired
	}
	return nil
}

func (p *CaronteProxy) Tools() []ToolName {
	out := make([]ToolName, 0, len(caronteToolNames))
	for _, t := range caronteToolNames {
		out = append(out, MustToolName("caronte", t))
	}
	return out
}

func (p *CaronteProxy) Close() error {
	if p.engine == nil {
		return nil
	}
	return p.engine.Close()
}

func (p *CaronteProxy) CallByTool(ctx context.Context, req CallRequest) (CallResponse, error) {
	if p.engine == nil {
		return CallResponse{}, p.escalate(req.Mode,
			fmt.Errorf("%w: engine not constructed", ErrCaronteBootstrapRequired))
	}
	started := time.Now()
	var (
		payload any
		opErr   error
	)
	switch req.Tool.Tool() {
	case "query":
		q, err := caronteStringArg(req.Args, "query")
		if err != nil {
			return CallResponse{}, err
		}
		var res research.CodeGraphResult
		res, opErr = p.engine.CodeGraph(ctx, q, req.ProjectID)
		payload = map[string]any{"hits": res.Hits, "project_id": res.ProjectID}
	case "context":
		sym, err := caronteStringArg(req.Args, "symbol")
		if err != nil {
			return CallResponse{}, err
		}
		var res caronte.ContextResult
		res, opErr = p.engine.Context(ctx, sym, req.ProjectID)
		payload = res
	case "impact", "get_risk":

		syms, files := caronteChangeArgs(req.Args)
		var rs evolution.RiskScore
		rs, opErr = p.engine.BlastRadius(ctx, req.ProjectID, syms, files)
		payload = rs
	case "get_why":
		subject, err := caronteStringArg(req.Args, "subject")
		if err != nil {
			return CallResponse{}, err
		}
		var ans intent.WhyAnswer
		ans, opErr = p.engine.GetWhy(ctx, req.ProjectID, subject)
		payload = ans
	case "get_implementations":
		iface, err := caronteStringArg(req.Args, "interface")
		if err != nil {
			return CallResponse{}, err
		}
		var impls []semantic.Implementation
		impls, opErr = p.engine.GetImplementations(ctx, iface, req.ProjectID)
		payload = map[string]any{"implementations": impls}
	case "trace_call_path":
		root, err := caronteStringArg(req.Args, "symbol")
		if err != nil {
			return CallResponse{}, err
		}
		depth := caronteIntArg(req.Args, "depth", 5)
		var hops []semantic.CallPathHop
		hops, opErr = p.engine.TraceCallPath(ctx, root, depth, req.ProjectID)
		payload = map[string]any{"hops": hops}
	case "get_cochange":
		file, err := caronteStringArg(req.Args, "file")
		if err != nil {
			return CallResponse{}, err
		}
		var peers []caronte.CoChangePeer
		peers, opErr = p.engine.GetCoChange(ctx, file, req.ProjectID)
		payload = map[string]any{"peers": peers}
	case "get_health":
		var h caronte.HealthReport
		h, opErr = p.engine.GetHealth(ctx, req.ProjectID)
		payload = h
	case "get_architecture":
		var a caronte.ArchitectureReport
		a, opErr = p.engine.GetArchitecture(ctx, req.ProjectID)
		payload = a
	case "wiki":
		module, _ := caronteStringArgOpt(req.Args, "module")
		var w caronte.WikiDoc
		w, opErr = p.engine.Wiki(ctx, module, req.ProjectID)
		payload = map[string]any{"module": w.Module, "markdown": w.Markdown}

	case "get_contract":
		payload, opErr = p.handleGetContract(ctx, req)
	case "get_consumers":
		payload, opErr = p.handleGetConsumers(ctx, req)
	case "get_breaking_changes":
		payload, opErr = p.handleGetBreakingChanges(ctx, req)
	case "trace_api_call":
		payload, opErr = p.handleTraceAPICall(ctx, req)
	case "get_workspace":
		payload, opErr = p.handleGetWorkspace(ctx, req)
	case "federation_health":
		payload, opErr = p.handleFederationHealth(ctx, req)
	case "contract_diff":
		payload, opErr = p.handleContractDiff(ctx, req)
	case "get_why_breaking_change":
		payload, opErr = p.handleGetWhyBreakingChange(ctx, req)
	default:
		return CallResponse{}, fmt.Errorf("%w: caronte subsystem does not expose %q",
			ErrToolNotRegistered, req.Tool.Tool())
	}
	latency := time.Since(started)
	if opErr != nil {
		p.emitUnreachable(req, latency, opErr)
		return CallResponse{}, p.escalate(req.Mode, fmt.Errorf("%w: %v", ErrCaronteUnreachable, opErr))
	}

	body, _ := json.Marshal(payload)
	return CallResponse{
		Content:   []CallContentItem{{Type: "text", Text: string(body)}},
		Subsystem: "caronte",
		Latency:   latency,
	}, nil
}

// escalate wraps err with a mode-aware hint string — keyword-stable so callers
// grep for autonomy/interactive/afk rather than parsing free-text. The three
// mappings are the Q7=B contract. Mode values outside the closed enum fall
// through to interactive (operator-visible doctor warning).
func (p *CaronteProxy) escalate(mode Mode, err error) error {
	switch mode {
	case ModeAutonomy:
		return fmt.Errorf("%w; mode=autonomy escalate=WAITING_FOR_CONFIRMATION", err)
	case ModeAFK:
		return fmt.Errorf("%w; mode=afk degrade=cached_summary", err)
	default:

		return fmt.Errorf("%w; mode=interactive degrade=doctor_warning", err)
	}
}

func (p *CaronteProxy) emitUnreachable(req CallRequest, latency time.Duration, cause error) {
	payload, _ := json.Marshal(map[string]any{
		"tool":       req.Tool.Tool(),
		"project_id": req.ProjectID,
		"latency_ms": latency.Milliseconds(),
		"cause":      cause.Error(),
	})
	p.audit.Emit("CaronteUnreachable", payload)
}

func caronteStringArg(args map[string]any, key string) (string, error) {
	if args == nil {
		return "", fmt.Errorf("mcpgateway: caronte args nil")
	}
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("mcpgateway: caronte args missing %q", key)
	}
	s, isStr := v.(string)
	if !isStr {
		return "", fmt.Errorf("mcpgateway: caronte args %q must be string, got %T", key, v)
	}
	if s == "" {
		return "", fmt.Errorf("mcpgateway: caronte args %q empty", key)
	}
	return s, nil
}

func caronteStringArgOpt(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	if v, ok := args[key]; ok {
		if s, isStr := v.(string); isStr {
			return s, true
		}
	}
	return "", false
}

func caronteIntArg(args map[string]any, key string, def int) int {
	if args == nil {
		return def
	}
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return def
}

func caronteChangeArgs(args map[string]any) (symbols, files []string) {
	return caronteStringSliceArg(args, "changed_symbols"), caronteStringSliceArg(args, "changed_files")
}

func caronteStringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			if s, isStr := e.(string); isStr {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
