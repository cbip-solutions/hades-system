// SPDX-License-Identifier: MIT
// Package handlers — mcpgateway_rest.go ( MAJOR-2 fix;
// Task J-11 distinct-ops repoint).
//
// REST sub-route adapters for /v1/mcpgateway/{codegraph,impact,context,wiki}.
//
// Background — substrate gap closure:
//
// (Q1=B "single HTTP MCP endpoint"). Task E-3 then shipped
// the client wrappers (internal/client/codegraph.go::CodegraphQuery / Impact /
// Context360 / Wiki) which call REST sub-routes — /v1/mcpgateway/codegraph,
// /v1/mcpgateway/impact, /v1/mcpgateway/context, /v1/mcpgateway/wiki — that
// the daemon never registered. built the TUI F7 panel on
// top of those client wrappers (CodegraphFile composes three of the four
// sub-routes into one round-trip). In production every F7 [Q]/[I]/[W]/[C]
// subpanel returned 404; the gap was masked because the client tests use
// httptest mock servers and never exercise the live daemon.
//
// MAJOR-2 closes the substrate gap. Each REST sub-route is a thin translation
// layer onto the existing well-tested gateway: build a JSON-RPC 2.0
// tools/call request, ServeHTTP through the gateway handler in-process via
// a buffered ResponseRecorder, parse the JSON-RPC response back into the
// REST shape the client expects. The gateway's RBAC, concurrency gate,
// audit emission, doctrine + mode header propagation, panic recovery, and
// caronte per-mode escalation all flow through unchanged.
//
// Cherry-pick narrative: this file completes the substrate gap
// inherited; if a backport branch is ever needed, the
// commit can be cherry-picked to a follow-up release.
//
// With the CaronteProxy registered under the "caronte" slot (
// renamed the segment gitnexus->caronte), the `context`, `impact`, and `wiki`
// tools return GENUINELY DISTINCT payload shapes (DECISION 6) — no longer the
// collapsed {hits} alias.
//
// - context → ContextResult JSON (Symbol/Callers/Callees/Neighbors/Community)
// - impact → RiskScore JSON (Score/Level/TopAffected)
// - wiki → {module, markdown} JSON (per-module auto-generated markdown)
//
// The `query` (codegraph) op still uses the {hits} shape — CodegraphQueryREST
// is unchanged. Only context/impact/wiki are repointed; they now call
// callGatewayRaw (which returns the raw content-text bytes) rather than
// callGateway (which parses the {hits} shape).
//
// invariant boundary: this file imports only stdlib. It does NOT import
// internal/caronte or internal/daemon/mcpgateway — the payload structs are
// local anonymous types parsed from the JSON the gateway returns. The gateway
// is consumed via the http.Handler interface threaded through
// MCPGatewayCtx.MCPGateway() — same pattern used N +
// merge gates.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

type MCPGatewayCtx interface {
	MCPGateway() http.Handler
}

type CodegraphRESTRequest struct {
	Query        string `json:"query"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type CodegraphRESTHit struct {
	Symbol     string `json:"symbol"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
}

type CodegraphRESTResponse struct {
	Hits []CodegraphRESTHit `json:"hits"`
}

type ImpactRESTRequest struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type ImpactRESTResponse struct {
	Symbol        string   `json:"symbol"`
	BlastRadius   string   `json:"blast_radius"`
	Score         int      `json:"score"`
	AffectedFiles []string `json:"affected_files,omitempty"`
}

type Context360RESTRequest struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type Context360RESTResponse struct {
	Symbol    string   `json:"symbol"`
	Callers   []string `json:"callers,omitempty"`
	Callees   []string `json:"callees,omitempty"`
	Neighbors []string `json:"neighbors,omitempty"`
	Community string   `json:"community,omitempty"`
}

type WikiRESTRequest struct {
	Module       string `json:"module,omitempty"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Regenerate   bool   `json:"regenerate,omitempty"`
}

type WikiRESTResponse struct {
	Module   string `json:"module,omitempty"`
	Markdown string `json:"markdown"`
}

type jsonrpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonrpcContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type jsonrpcCallResult struct {
	Content []jsonrpcContent `json:"content"`
	IsError bool             `json:"isError"`
}

type jsonrpcResp struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      int                `json:"id"`
	Result  *jsonrpcCallResult `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type caronteToolPayload struct {
	Hits []struct {
		Node  string  `json:"node"`
		Score float64 `json:"score"`
		URL   string  `json:"url"`
	} `json:"hits"`
	ProjectID string `json:"project_id"`
}

func CodegraphQueryREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req CodegraphRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Query) == "" {
			http.Error(w, "query required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"query": req.Query}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}
		payload, status, err := callGateway(r.Context(), gw, "mcp_zen-swarm_caronte_query", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		out := CodegraphRESTResponse{Hits: make([]CodegraphRESTHit, 0, len(payload.Hits))}
		for i, h := range payload.Hits {
			if req.Limit > 0 && i >= req.Limit {
				break
			}
			symbol, file, line, kind := splitCaronteNode(h.Node, h.URL)
			out.Hits = append(out.Hits, CodegraphRESTHit{
				Symbol:     symbol,
				File:       file,
				Line:       line,
				Kind:       kind,
				Confidence: scoreToConfidence(h.Score),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func ImpactREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req ImpactRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Symbol) == "" {
			http.Error(w, "symbol required", http.StatusBadRequest)
			return
		}

		args := map[string]any{"changed_symbols": []string{req.Symbol}}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_impact", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var risk struct {
			Score       float64  `json:"Score"`
			Level       string   `json:"Level"`
			TopAffected []string `json:"TopAffected"`
		}
		if err := json.Unmarshal(raw, &risk); err != nil {
			http.Error(w, "decode risk payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := ImpactRESTResponse{
			Symbol:        req.Symbol,
			BlastRadius:   risk.Level,
			Score:         int(risk.Score * 100),
			AffectedFiles: risk.TopAffected,
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func Context360REST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req Context360RESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Symbol) == "" {
			http.Error(w, "symbol required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"symbol": req.Symbol}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_context", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		var ctxPayload struct {
			Symbol    string   `json:"Symbol"`
			Callers   []string `json:"Callers"`
			Callees   []string `json:"Callees"`
			Neighbors []string `json:"Neighbors"`
			Community string   `json:"Community"`
		}
		if err := json.Unmarshal(raw, &ctxPayload); err != nil {
			http.Error(w, "decode context payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := Context360RESTResponse{
			Symbol:    req.Symbol,
			Callers:   ctxPayload.Callers,
			Callees:   ctxPayload.Callees,
			Neighbors: ctxPayload.Neighbors,
			Community: ctxPayload.Community,
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func WikiREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req WikiRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		args := map[string]any{}
		if req.Module != "" {
			args["module"] = req.Module
		}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}
		if req.Regenerate {
			args["regenerate"] = true
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_wiki", args, r.Header)
		if err != nil {
			if status == http.StatusNotImplemented || strings.Contains(err.Error(), "method not found") || strings.Contains(err.Error(), "not registered") {
				http.Error(w, "wiki tool not registered in caronte subsystem", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, err.Error(), status)
			return
		}
		var wiki struct {
			Module   string `json:"module"`
			Markdown string `json:"markdown"`
		}
		if err := json.Unmarshal(raw, &wiki); err != nil {
			http.Error(w, "decode wiki payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, WikiRESTResponse{Module: wiki.Module, Markdown: wiki.Markdown})
	}
}

func callGateway(
	ctx context.Context,
	gw http.Handler,
	toolName string,
	args map[string]any,
	headers http.Header,
) (caronteToolPayload, int, error) {
	rpcReq := jsonrpcReq{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return caronteToolPayload{}, http.StatusInternalServerError, fmt.Errorf("marshal: %w", err)
	}
	innerReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	if err != nil {
		return caronteToolPayload{}, http.StatusInternalServerError, fmt.Errorf("new request: %w", err)
	}
	innerReq.Header.Set("Content-Type", "application/json")
	for _, h := range []string{"X-Zen-Doctrine", "X-Zen-Mode", "X-Zen-Session-ID", "X-Zen-Project-ID"} {
		if v := headers.Get(h); v != "" {
			innerReq.Header.Set(h, v)
		}
	}
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, innerReq)

	if rec.Code != http.StatusOK {

		respBody, _ := io.ReadAll(rec.Body)
		return caronteToolPayload{}, rec.Code, fmt.Errorf("gateway: %s", strings.TrimSpace(string(respBody)))
	}

	var rpcResp jsonrpcResp
	if err := json.Unmarshal(rec.Body.Bytes(), &rpcResp); err != nil {
		return caronteToolPayload{}, http.StatusBadGateway, fmt.Errorf("decode jsonrpc: %w", err)
	}
	if rpcResp.Error != nil {
		status := mapJSONRPCError(rpcResp.Error.Code)
		return caronteToolPayload{}, status, fmt.Errorf("gateway: %s", rpcResp.Error.Message)
	}
	if rpcResp.Result == nil || len(rpcResp.Result.Content) == 0 {
		return caronteToolPayload{}, http.StatusOK, nil
	}

	var payload caronteToolPayload
	if err := json.Unmarshal([]byte(rpcResp.Result.Content[0].Text), &payload); err != nil {
		return caronteToolPayload{}, http.StatusBadGateway, fmt.Errorf("decode tool payload: %w", err)
	}
	return payload, http.StatusOK, nil
}

func callGatewayRaw(
	ctx context.Context,
	gw http.Handler,
	toolName string,
	args map[string]any,
	headers http.Header,
) ([]byte, int, error) {
	rpcReq := jsonrpcReq{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("marshal: %w", err)
	}
	innerReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/mcpgateway", bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("new request: %w", err)
	}
	innerReq.Header.Set("Content-Type", "application/json")
	for _, h := range []string{"X-Zen-Doctrine", "X-Zen-Mode", "X-Zen-Session-ID", "X-Zen-Project-ID"} {
		if v := headers.Get(h); v != "" {
			innerReq.Header.Set(h, v)
		}
	}
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, innerReq)
	if rec.Code != http.StatusOK {
		respBody, _ := io.ReadAll(rec.Body)
		return nil, rec.Code, fmt.Errorf("gateway: %s", strings.TrimSpace(string(respBody)))
	}
	var rpcResp jsonrpcResp
	if err := json.Unmarshal(rec.Body.Bytes(), &rpcResp); err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("decode jsonrpc: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, mapJSONRPCError(rpcResp.Error.Code), fmt.Errorf("gateway: %s", rpcResp.Error.Message)
	}
	if rpcResp.Result == nil || len(rpcResp.Result.Content) == 0 {
		return []byte("{}"), http.StatusOK, nil
	}
	return []byte(rpcResp.Result.Content[0].Text), http.StatusOK, nil
}

func mapJSONRPCError(code int) int {
	switch code {
	case -32700, -32600, -32602:
		return http.StatusBadRequest
	case -32601:
		return http.StatusNotImplemented
	case -32000:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func splitCaronteNode(node, url string) (symbol, file string, line int, kind string) {
	symbol = node
	if url == "" {
		return symbol, "", 0, ""
	}

	rest := url
	if strings.HasPrefix(rest, "caronte://") {
		rest = strings.TrimPrefix(rest, "caronte://")
		if idx := strings.Index(rest, "/"); idx >= 0 {
			rest = rest[idx+1:]
		}
	}

	if idx := strings.Index(rest, "?"); idx >= 0 {
		q := rest[idx+1:]
		rest = rest[:idx]
		for _, kv := range strings.Split(q, "&") {
			if k, v, ok := strings.Cut(kv, "="); ok && k == "kind" {
				kind = v
			}
		}
	}

	if idx := strings.LastIndex(rest, ":"); idx >= 0 {
		file = rest[:idx]
		var n int
		fmt.Sscanf(rest[idx+1:], "%d", &n)
		line = n
	} else {
		file = rest
	}
	return symbol, file, line, kind
}

func scoreToConfidence(score float64) int {
	switch {
	case score <= 0:
		return 0
	case score >= 1:
		return 100
	default:
		return int(score * 100)
	}
}

type WhyRESTRequest struct {
	Symbol       string `json:"symbol"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type WhyRESTLinkedADR struct {
	ADRID      string  `json:"adr_id"`
	ADRTitle   string  `json:"adr_title"`
	LinkKind   string  `json:"link_kind"`
	Confidence float64 `json:"confidence"`
	Stale      bool    `json:"stale"`
}

type WhyRESTSemanticPassage struct {
	SourceID   string  `json:"source_id"`
	SourceKind string  `json:"source_kind"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

type WhyRESTLoreEntry struct {
	CommitSHA   string `json:"commit_sha"`
	TrailerKind string `json:"trailer_kind"`
	Body        string `json:"body"`
	AuthoredAt  int64  `json:"authored_at"`
}

type WhyRESTResponse struct {
	Subject          string                   `json:"subject"`
	LinkedADRs       []WhyRESTLinkedADR       `json:"linked_adrs"`
	SemanticPassages []WhyRESTSemanticPassage `json:"semantic_passages"`
	LoreTrailers     []WhyRESTLoreEntry       `json:"lore_trailers"`
	Degraded         bool                     `json:"degraded"`
}

type RiskRESTRequest struct {
	ChangedSymbols []string `json:"changed_symbols,omitempty"`
	ChangedFiles   []string `json:"changed_files,omitempty"`
	ProjectAlias   string   `json:"project_alias,omitempty"`
}

type RiskRESTResponse struct {
	Level       string   `json:"level"`
	Score       float64  `json:"score"`
	Cone        float64  `json:"cone"`
	Coreness    float64  `json:"coreness"`
	Churn       float64  `json:"churn"`
	Coupling    float64  `json:"coupling"`
	TopAffected []string `json:"top_affected"`
}

type CoChangeRESTRequest struct {
	File         string `json:"file"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type CoChangeRESTPeer struct {
	Path            string  `json:"path"`
	CouplingPercent float64 `json:"coupling_percent"`
	SharedRevs      int     `json:"shared_revs"`
	WindowDays      int     `json:"window_days"`
}

type CoChangeRESTResponse struct {
	File  string             `json:"file"`
	Peers []CoChangeRESTPeer `json:"peers"`
}

type ImplRESTRequest struct {
	Interface    string `json:"interface"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type ImplRESTRow struct {
	InterfaceID string `json:"interface_id"`
	ImplID      string `json:"impl_id"`
	Confidence  string `json:"confidence"`
	Reachable   bool   `json:"reachable"`
}

type ImplRESTResponse struct {
	Interface       string        `json:"interface"`
	Implementations []ImplRESTRow `json:"implementations"`
}

type HealthRESTRequest struct {
	ProjectAlias string `json:"project_alias,omitempty"`
}

type HealthRESTResponse struct {
	ProjectID    string   `json:"project_id"`
	NodeCount    int      `json:"node_count"`
	EdgeCount    int      `json:"edge_count"`
	PackageCount int      `json:"package_count"`
	CyclicSCCs   int      `json:"cyclic_sccs"`
	Languages    []string `json:"languages"`
	Degraded     bool     `json:"degraded"`
	ResolveMode  string   `json:"resolve_mode"`
	LastIndexed  int64    `json:"last_indexed"`
}

func WhyREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req WhyRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Symbol) == "" {
			http.Error(w, "symbol required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"subject": req.Symbol}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_why", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		var p struct {
			Subject    string `json:"Subject"`
			LinkedADRs []struct {
				ADRID      string  `json:"ADRID"`
				ADRTitle   string  `json:"ADRTitle"`
				LinkKind   string  `json:"LinkKind"`
				Confidence float64 `json:"Confidence"`
				Stale      bool    `json:"Stale"`
			} `json:"LinkedADRs"`
			SemanticPassages []struct {
				SourceID   string  `json:"SourceID"`
				SourceKind string  `json:"SourceKind"`
				Text       string  `json:"Text"`
				Score      float64 `json:"Score"`
			} `json:"SemanticPassages"`
			LoreTrailers []struct {
				CommitSHA   string `json:"CommitSHA"`
				TrailerKind string `json:"TrailerKind"`
				Body        string `json:"Body"`
				AuthoredAt  int64  `json:"AuthoredAt"`
			} `json:"LoreTrailers"`
			Degraded bool `json:"Degraded"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "decode why payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := WhyRESTResponse{Subject: req.Symbol, Degraded: p.Degraded}
		for _, a := range p.LinkedADRs {
			out.LinkedADRs = append(out.LinkedADRs, WhyRESTLinkedADR{
				ADRID: a.ADRID, ADRTitle: a.ADRTitle, LinkKind: a.LinkKind,
				Confidence: a.Confidence, Stale: a.Stale,
			})
		}
		for _, s := range p.SemanticPassages {
			out.SemanticPassages = append(out.SemanticPassages, WhyRESTSemanticPassage{
				SourceID: s.SourceID, SourceKind: s.SourceKind, Text: s.Text, Score: s.Score,
			})
		}
		for _, l := range p.LoreTrailers {
			out.LoreTrailers = append(out.LoreTrailers, WhyRESTLoreEntry{
				CommitSHA: l.CommitSHA, TrailerKind: l.TrailerKind, Body: l.Body, AuthoredAt: l.AuthoredAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func RiskREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req RiskRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.ChangedSymbols) == 0 && len(req.ChangedFiles) == 0 {
			http.Error(w, "at least one changed_symbol or changed_file required", http.StatusBadRequest)
			return
		}
		args := map[string]any{
			"changed_symbols": req.ChangedSymbols,
			"changed_files":   req.ChangedFiles,
		}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_risk", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		var p struct {
			Score       float64  `json:"Score"`
			Level       string   `json:"Level"`
			Cone        float64  `json:"Cone"`
			Coreness    float64  `json:"Coreness"`
			Churn       float64  `json:"Churn"`
			Coupling    float64  `json:"Coupling"`
			TopAffected []string `json:"TopAffected"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "decode risk payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, RiskRESTResponse{
			Level: p.Level, Score: p.Score, Cone: p.Cone, Coreness: p.Coreness,
			Churn: p.Churn, Coupling: p.Coupling, TopAffected: p.TopAffected,
		})
	}
}

func CochangeREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req CoChangeRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.File) == "" {
			http.Error(w, "file required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"file": req.File}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_cochange", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		var p struct {
			Peers []struct {
				Path            string  `json:"Path"`
				CouplingPercent float64 `json:"CouplingPercent"`
				SharedRevs      int     `json:"SharedRevs"`
				WindowDays      int     `json:"WindowDays"`
			} `json:"peers"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "decode cochange payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := CoChangeRESTResponse{File: req.File, Peers: make([]CoChangeRESTPeer, 0, len(p.Peers))}
		for _, pr := range p.Peers {
			out.Peers = append(out.Peers, CoChangeRESTPeer{
				Path: pr.Path, CouplingPercent: pr.CouplingPercent,
				SharedRevs: pr.SharedRevs, WindowDays: pr.WindowDays,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func ImplREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req ImplRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Interface) == "" {
			http.Error(w, "interface required", http.StatusBadRequest)
			return
		}
		args := map[string]any{"interface": req.Interface}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_implementations", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		var p struct {
			Implementations []struct {
				InterfaceID string `json:"InterfaceID"`
				ImplID      string `json:"ImplID"`
				Confidence  string `json:"Confidence"`
				Reachable   bool   `json:"Reachable"`
			} `json:"implementations"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "decode impl payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		out := ImplRESTResponse{Interface: req.Interface, Implementations: make([]ImplRESTRow, 0, len(p.Implementations))}
		for _, im := range p.Implementations {
			out.Implementations = append(out.Implementations, ImplRESTRow{
				InterfaceID: im.InterfaceID, ImplID: im.ImplID,
				Confidence: im.Confidence, Reachable: im.Reachable,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func HealthREST(ctx MCPGatewayCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		gw := ctx.MCPGateway()
		if gw == nil {
			http.Error(w, "mcpgateway not configured", http.StatusServiceUnavailable)
			return
		}
		var req HealthRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		args := map[string]any{}
		if req.ProjectAlias != "" {
			args["project_id"] = req.ProjectAlias
		}

		raw, status, err := callGatewayRaw(r.Context(), gw, "mcp_zen-swarm_caronte_get_health", args, r.Header)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		var p struct {
			ProjectID    string   `json:"ProjectID"`
			NodeCount    int      `json:"NodeCount"`
			EdgeCount    int      `json:"EdgeCount"`
			PackageCount int      `json:"PackageCount"`
			CyclicSCCs   int      `json:"CyclicSCCs"`
			Languages    []string `json:"Languages"`
			Degraded     bool     `json:"Degraded"`
			ResolveMode  string   `json:"ResolveMode"`
			LastIndexed  int64    `json:"LastIndexed"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			http.Error(w, "decode health payload: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, HealthRESTResponse{
			ProjectID: p.ProjectID, NodeCount: p.NodeCount, EdgeCount: p.EdgeCount,
			PackageCount: p.PackageCount, CyclicSCCs: p.CyclicSCCs, Languages: p.Languages,
			Degraded: p.Degraded, ResolveMode: p.ResolveMode, LastIndexed: p.LastIndexed,
		})
	}
}
