// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/server.go
//
// Server — HTTP MCP endpoint Hermes consumes (Q1=B single endpoint).
// Wire format: JSON-RPC 2.0 over HTTP per MCP 2024-11-05 Streamable HTTP
// transport. is request/response only (no SSE streaming).
//
// Routes (mounted via daemon registerRoutes in main.go A-7):
//
// POST /v1/mcpgateway — JSON-RPC 2.0 endpoint
//
// Methods supported:
//
// initialize — returns server capabilities envelope
// tools/list — returns Dispatcher.ListTools as MCP-spec tools array
// tools/call — routes to Dispatcher.Dispatch and re-encodes response
//
// Header conventions (Hermes side forwards via HadesSystemTransport, release
// ):
//
// X-HADES-Doctrine — active doctrine slug (max-scope / default / capa-firewall)
// X-HADES-Mode — call mode (interactive / autonomy / afk)
// X-HADES-Session-ID — Hermes session id
// X-HADES-Project-ID — active project id
//
// JSON-RPC 2.0 error codes used:
//
// -32700 Parse error (invalid JSON in request body)
// -32600 Invalid Request (missing method or jsonrpc != "2.0")
// -32601 Method not found
// -32602 Invalid params (includes unrecognised X-HADES-Mode / X-HADES-Doctrine header)
// -32000 Server error (RBAC denies, gitnexus unreachable, etc.)
package mcpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

var ErrInvalidMode = errors.New("mcpgateway: invalid X-HADES-Mode header")

var ErrInvalidDoctrine = errors.New("mcpgateway: invalid X-HADES-Doctrine header")

// Server wraps a Dispatcher + exposes the HTTP MCP endpoint.
//
// aliasResolver translates the X-HADES-Project-ID header OR the body
// arguments.project_id from an alias (e.g. "hades-system-3572a35b") into
// the canonical id_sha256 (64 hex chars) the engine + caronteadapter
// consume. Wired via SetAliasResolver (NOT NewServer) so the daemon
// can construct Server before the projects_alias store + adapter exist
// (matches the buildDispatcher → NewServer → wire-tail order in
// cmd/hades-ctld/main.go).
//
// A nil aliasResolver means "legacy header pass-through mode" — the raw
// X-HADES-Project-ID value is forwarded AS-IS to CallRequest.ProjectID.
// Production daemons MUST call SetAliasResolver at boot; the nil
// fallback exists so that a partially-wired daemon does not crash on
// tools/call (defensive operator-recovery posture per spec §22.4
// graceful degradation).
//
// invariant (project_id dual-source), invariant (alias resolution).
type Server struct {
	d             *Dispatcher
	aliasResolver ProjectsAliasResolver
	callSeq       atomic.Int64
}

func NewServer(d *Dispatcher) *Server {
	if d == nil {
		panic("mcpgateway: NewServer with nil Dispatcher")
	}
	return &Server{d: d}
}

func (s *Server) SetAliasResolver(r ProjectsAliasResolver) {
	s.aliasResolver = r
}

func (s *Server) AliasResolver() ProjectsAliasResolver {
	return s.aliasResolver
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeErr(w, req.ID, -32600, "jsonrpc must be \"2.0\"")
		return
	}
	if req.Method == "" {
		s.writeErr(w, req.ID, -32600, "method required")
		return
	}

	doctrine, doctrineErr := parseDoctrine(r.Header.Get("X-HADES-Doctrine"))
	if doctrineErr != nil {
		s.writeErr(w, req.ID, -32602, "invalid params: "+doctrineErr.Error())
		return
	}
	mode, modeErr := parseMode(r.Header.Get("X-HADES-Mode"))
	if modeErr != nil {
		s.writeErr(w, req.ID, -32602, "invalid params: "+modeErr.Error())
		return
	}
	sessionID := r.Header.Get("X-HADES-Session-ID")
	projectID := r.Header.Get("X-HADES-Project-ID")

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req)
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(r.Context(), w, req, doctrine, mode, sessionID, projectID)
	default:
		s.writeErr(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, req jsonrpcRequest) {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": nil,
			"prompts":   nil,
		},
		"serverInfo": map[string]any{
			"name":    "hades-system-mcpgateway",
			"version": "0.11.0-dev",
		},
	}
	s.writeOK(w, req.ID, result)
}

func (s *Server) handleToolsList(w http.ResponseWriter, req jsonrpcRequest) {
	entries := s.d.ListTools()
	tools := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		tool := map[string]any{
			"name":        e.Name.String(),
			"description": e.Meta.Description,
		}
		if e.Meta.InputSchema != nil {
			tool["inputSchema"] = e.Meta.InputSchema
		} else {
			tool["inputSchema"] = map[string]any{"type": "object"}
		}
		tools = append(tools, tool)
	}
	s.writeOK(w, req.ID, map[string]any{"tools": tools})
}

func (s *Server) handleToolsCall(
	ctx context.Context,
	w http.ResponseWriter,
	req jsonrpcRequest,
	doctrine Doctrine,
	mode Mode,
	sessionID, projectID string,
) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeErr(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	if params.Name == "" {
		s.writeErr(w, req.ID, -32602, "params.name required")
		return
	}
	tn, err := ParseToolName(params.Name)
	if err != nil {
		s.writeErr(w, req.ID, -32602, "invalid tool name: "+err.Error())
		return
	}

	rawProjectID := projectID
	if rawProjectID == "" && params.Arguments != nil {
		if v, ok := params.Arguments["project_id"].(string); ok && v != "" {
			rawProjectID = v
		}
	}

	canonicalProjectID := rawProjectID
	if s.aliasResolver != nil {
		if rawProjectID == "" {

			s.writeErr(w, req.ID, -32602,
				"project_id required (header X-HADES-Project-ID or arguments.project_id)")
			return
		}

		resolved, resErr := s.aliasResolver.Resolve(ctx, rawProjectID)
		if resErr != nil {

			if errors.Is(resErr, ErrAliasNotFound) {
				s.writeErr(w, req.ID, -32000,
					fmt.Sprintf("project_id %q not found in projects_alias (register via 'hades project doctor' or check archive state)", rawProjectID))
			} else {
				s.writeErr(w, req.ID, -32000,
					fmt.Sprintf("project_id resolution failed: %v", resErr))
			}
			return
		}
		canonicalProjectID = resolved
	}

	cr := CallRequest{
		Tool:      tn,
		Args:      params.Arguments,
		Doctrine:  doctrine,
		Mode:      mode,
		SessionID: sessionID,
		ProjectID: canonicalProjectID,
		CallID:    s.callSeq.Add(1),
	}
	resp, dispErr := s.d.Dispatch(ctx, cr)
	if dispErr != nil {

		code := -32000
		switch {
		case errors.Is(dispErr, ErrToolNotRegistered):
			code = -32601
		case errors.Is(dispErr, ErrRBACDenied),
			errors.Is(dispErr, ErrConcurrencyLimit),
			errors.Is(dispErr, ErrCaronteUnreachable):
			code = -32000
		}
		s.writeErr(w, req.ID, code, dispErr.Error())
		return
	}

	result := map[string]any{
		"content": resp.Content,
		"isError": resp.IsError,
	}
	s.writeOK(w, req.ID, result)
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) writeOK(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func (s *Server) writeErr(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	})
}

func parseDoctrine(h string) (Doctrine, error) {
	switch h {
	case "":
		return Doctrine(""), nil
	case string(DoctrineMaxScope), string(DoctrineDefault), string(DoctrineCapaFirewall):
		return Doctrine(h), nil
	default:
		return Doctrine(""), fmt.Errorf("%w: %q (expected max-scope|default|capa-firewall)", ErrInvalidDoctrine, h)
	}
}

func parseMode(h string) (Mode, error) {
	switch h {
	case "":
		return ModeUnspecified, nil
	case "interactive":
		return ModeInteractive, nil
	case "autonomy":
		return ModeAutonomy, nil
	case "afk":
		return ModeAFK, nil
	default:
		return ModeUnspecified, fmt.Errorf("%w: %q (expected interactive|autonomy|afk)", ErrInvalidMode, h)
	}
}
