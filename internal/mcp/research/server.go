// SPDX-License-Identifier: MIT
package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var ErrInvalidOptions = errors.New("research: invalid options")

type ServerOptions struct {
	Dispatcher Dispatcher

	WebSearchTool WebSearchBackend
	ArxivTool     ArxivBackend
	GitHubTool    GitHubBackend
	EcosystemTool EcosystemBackend

	GitnexusClient GitnexusClient

	Synthesizer Synthesizer

	Cache        CacheClient
	BudgetClient BudgetClient
	AuditClient  AuditClient

	Cite CiteService

	Doctrine DoctrineSnapshot
}

type Server struct {
	mcpServer *mcp.Server
	opts      *ServerOptions

	toolNames []string

	handlers map[string]toolHandler
}

type toolHandler func(ctx context.Context, args map[string]any) (any, error)

func NewServer(opts *ServerOptions) (*Server, error) {
	if opts == nil {
		return nil, fmt.Errorf("%w: nil options", ErrInvalidOptions)
	}
	if opts.Dispatcher == nil {
		return nil, fmt.Errorf("%w: missing Dispatcher", ErrInvalidOptions)
	}
	if opts.WebSearchTool == nil {
		return nil, fmt.Errorf("%w: missing WebSearchTool", ErrInvalidOptions)
	}
	if opts.ArxivTool == nil {
		return nil, fmt.Errorf("%w: missing ArxivTool", ErrInvalidOptions)
	}
	if opts.GitHubTool == nil {
		return nil, fmt.Errorf("%w: missing GitHubTool", ErrInvalidOptions)
	}
	if opts.EcosystemTool == nil {
		return nil, fmt.Errorf("%w: missing EcosystemTool", ErrInvalidOptions)
	}
	if opts.GitnexusClient == nil {
		return nil, fmt.Errorf("%w: missing GitnexusClient", ErrInvalidOptions)
	}
	if opts.Synthesizer == nil {
		return nil, fmt.Errorf("%w: missing Synthesizer", ErrInvalidOptions)
	}
	if opts.Cache == nil {
		return nil, fmt.Errorf("%w: missing Cache", ErrInvalidOptions)
	}
	if opts.BudgetClient == nil {
		return nil, fmt.Errorf("%w: missing BudgetClient", ErrInvalidOptions)
	}
	if opts.AuditClient == nil {
		return nil, fmt.Errorf("%w: missing AuditClient", ErrInvalidOptions)
	}
	if opts.Cite == nil {
		return nil, fmt.Errorf("%w: missing Cite", ErrInvalidOptions)
	}

	srv := &Server{
		opts:     opts,
		handlers: make(map[string]toolHandler, 8),
	}
	srv.registerTools()

	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "hades-mcp-research",
		Version: "0.4.0",
	}, nil)
	srv.bindToMCPServer(mcpSrv)
	srv.mcpServer = mcpSrv
	return srv, nil
}

func (s *Server) registerTools() {
	s.handlers["web_search"] = s.handleWebSearch
	s.handlers["arxiv"] = s.handleArxiv
	s.handlers["github_search"] = s.handleGitHubSearch
	s.handlers["code_graph"] = s.handleCodeGraph
	s.handlers["ecosystem_docs"] = s.handleEcosystemDocs
	s.handlers["synthesize"] = s.handleSynthesize
	s.handlers["cite"] = s.handleCite
	s.handlers["agentic_deep"] = s.handleAgenticDeep

	names := make([]string, 0, len(s.handlers))
	for name := range s.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	s.toolNames = names
}

type toolSpec struct {
	name        string
	description string
	inputSchema any
}

func (s *Server) toolSpecs() []toolSpec {
	return []toolSpec{
		{
			name:        "web_search",
			description: "Web search via DDG + Firecrawl full-page extraction.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":       map[string]any{"type": "string"},
					"max_results": map[string]any{"type": "integer", "default": 10},
				},
				"required": []string{"query"},
			},
		},
		{
			name:        "arxiv",
			description: "ArXiv paper search via export.arxiv.org REST API.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":       map[string]any{"type": "string"},
					"max_results": map[string]any{"type": "integer", "default": 10},
					"sort_by":     map[string]any{"type": "string", "enum": []string{"relevance", "lastUpdatedDate"}, "default": "relevance"},
				},
				"required": []string{"query"},
			},
		},
		{
			name:        "github_search",
			description: "GitHub repository search via api.github.com.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string"},
					"language":  map[string]any{"type": "string"},
					"stars_min": map[string]any{"type": "integer", "default": 0},
				},
				"required": []string{"query"},
			},
		},
		{
			name:        "code_graph",
			description: "Project knowledge-graph query via the code-graph engine.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":      map[string]any{"type": "string"},
					"project_id": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
		{
			name:        "ecosystem_docs",
			description: "Search official Go/Python/TS ecosystem docs.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string"},
					"ecosystem": map[string]any{"type": "string", "enum": []string{"go", "python", "typescript"}},
				},
				"required": []string{"query", "ecosystem"},
			},
		},
		{
			name:        "synthesize",
			description: "Synthesize findings via HADES design dispatcher (LLM call).",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"findings": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
					"prompt":   map[string]any{"type": "string"},
				},
				"required": []string{"findings"},
			},
		},
		{
			name:        "cite",
			description: "Format a verified citation for spec/commit/audit consumption.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source_id": map[string]any{"type": "string"},

					"url":   map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
				},
				"required": []string{"source_id"},
			},
		},
		{
			name:        "agentic_deep",
			description: "Iterative deep-research wrapper (design choice C). Doctrine-tunable max-iter.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"initial_query": map[string]any{"type": "string"},
					"max_iter":      map[string]any{"type": "integer"},
				},
				"required": []string{"initial_query"},
			},
		},
	}
}

func (s *Server) bindToMCPServer(srv *mcp.Server) {
	for _, sp := range s.toolSpecs() {
		spName := sp.name
		spDesc := sp.description
		spSchema := sp.inputSchema
		mcp.AddTool(srv, &mcp.Tool{
			Name:        spName,
			Description: spDesc,
			InputSchema: spSchema,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
			h, ok := s.handlers[spName]
			if !ok {
				return nil, nil, fmt.Errorf("research: tool not registered: %s", spName)
			}
			out, err := h(ctx, args)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: jsonString(out)},
				},
			}, out, nil
		})
	}
}

func (s *Server) RegisteredToolNames() []string {
	out := make([]string, len(s.toolNames))
	copy(out, s.toolNames)
	return out
}

func (s *Server) ToolNames() []string {
	return s.RegisteredToolNames()
}

func (s *Server) InvokeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	h, ok := s.handlers[name]
	if !ok {
		return nil, fmt.Errorf("research: unknown tool: %s", name)
	}
	return h(ctx, args)
}

func (s *Server) Serve(ctx context.Context) error {
	transport := &mcp.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}

func (s *Server) Close() error {
	var firstErr error
	closeOnce(&firstErr, s.opts.GitnexusClient)
	return firstErr
}

func closeOnce(out *error, c io.Closer) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil && *out == nil {
		*out = err
	}
}

func (s *Server) handleWebSearch(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	max := intArg(args, "max_results", 10)
	allowed, blockedScope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:web_search", 0.05)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", blockedScope)
	}
	hits, err := s.opts.WebSearchTool.Search(ctx, q, max)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": hits, "query": q}, nil
}

func (s *Server) handleArxiv(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	max := intArg(args, "max_results", 10)
	sortBy, _ := args["sort_by"].(string)
	if sortBy == "" {
		sortBy = "relevance"
	}
	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:arxiv", 0.01)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}
	hits, err := s.opts.ArxivTool.Search(ctx, q, max, sortBy)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": hits, "query": q}, nil
}

func (s *Server) handleGitHubSearch(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	lang, _ := args["language"].(string)
	stars := intArg(args, "stars_min", 0)
	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:github_search", 0.01)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}
	hits, err := s.opts.GitHubTool.Search(ctx, q, lang, stars)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": hits, "query": q}, nil
}

func (s *Server) handleCodeGraph(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	pid, _ := args["project_id"].(string)
	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:code_graph", 0.001)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}
	res, err := s.opts.GitnexusClient.CodeGraph(ctx, q, pid)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) handleEcosystemDocs(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["query"].(string)
	eco, _ := args["ecosystem"].(string)
	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:ecosystem_docs", 0.001)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}
	hits, err := s.opts.EcosystemTool.Search(ctx, q, eco)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": hits, "query": q, "ecosystem": eco}, nil
}

func (s *Server) handleSynthesize(ctx context.Context, args map[string]any) (any, error) {
	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:synthesize", 0.20)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}
	in := SynthesizeInput{}
	if findingsRaw, ok := args["findings"].([]any); ok {
		in.RawFindings = findingsRaw
	}
	if p, ok := args["prompt"].(string); ok {
		in.Prompt = p
	}
	out, err := s.opts.Synthesizer.Synthesize(ctx, in)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) handleCite(ctx context.Context, args map[string]any) (any, error) {
	src, _ := args["source_id"].(string)
	if src == "" {
		return nil, errors.New("research: source_id required")
	}
	rawURL, _ := args["url"].(string)
	rawTitle, _ := args["title"].(string)
	verified, err := s.opts.Cite.Verify(ctx, []RawCitation{{SourceID: src, URL: rawURL, Title: rawTitle}})
	if err != nil {
		return nil, err
	}
	md, structured := s.opts.Cite.Format(verified)
	return map[string]any{"markdown": md, "structured": json.RawMessage(structured)}, nil
}

func (s *Server) handleAgenticDeep(ctx context.Context, args map[string]any) (any, error) {
	q, _ := args["initial_query"].(string)
	maxIter := intArg(args, "max_iter", -1)
	if q == "" {
		return nil, errors.New("research: initial_query required")
	}

	allowed, scope, err := s.opts.BudgetClient.PreCall(ctx, "stage", "research:agentic_deep", 0.10)
	if err != nil {
		return nil, fmt.Errorf("budget pre-check: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("research: blocked by budget at scope=%s", scope)
	}

	if maxIter <= 0 {
		if s.opts.Doctrine != nil {
			maxIter = s.opts.Doctrine.AgenticMaxIter()
		}
	}
	a := NewAgentic(AgenticOptions{
		Dispatcher:  s.opts.Dispatcher,
		Synthesizer: s.opts.Synthesizer,
		Budget:      s.opts.BudgetClient,
		MaxIter:     maxIter,
	})
	res, err := a.Run(ctx, q)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func intArg(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case int64:
			return int(t)
		}
	}
	return def
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
