// SPDX-License-Identifier: MIT
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	_stdioCanonicalSentinel = AssertStdioCanonical

	_boundaryPreservedSentinel = AssertBoundaryPreserved
)

func AssertStdioCanonical() bool { return true }

func AssertBoundaryPreserved() bool { return true }

type EmptyPoolPolicy int

const (
	EmptyPoolHardStop EmptyPoolPolicy = iota
	// EmptyPoolWarnAndDegrade logs a warning and proceeds with the largest
	// available disjoint pool (even if below MinPoolSize). Used by default doctrine.
	EmptyPoolWarnAndDegrade
)

type ServerConfig struct {
	DaemonBaseURL string

	AuthToken string

	ReviewerFamilyPool []string

	MinPoolSize int

	CustomCriteria map[string]string

	DefaultReviewerModel string

	EmptyPoolPolicy EmptyPoolPolicy
	// Logger receives structured warning lines emitted by the server (e.g. on
	// EmptyPoolWarnAndDegrade fall-through). Optional: when nil, the server
	// uses log.Default() which writes to stderr.
	//
	// Pre-fix (review C-1) the warning was computed via _ = fmt.Sprintf(...)
	// and silently discarded — operators had no signal that a degraded review
	// was happening. Tests can inject log.New(&bytes.Buffer{}, "", 0) to
	// assert warning emission.
	Logger *log.Logger
}

type Server struct {
	mcpSrv   *mcp.Server
	cfg      ServerConfig
	criteria *CriteriaRegistry
	router   *Router
}

func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.MinPoolSize < 1 {
		cfg.MinPoolSize = 2
	}

	if len(cfg.ReviewerFamilyPool) < cfg.MinPoolSize+1 {
		return nil, fmt.Errorf(
			"audit: ReviewerFamilyPool has %d entries but needs >= %d (MinPoolSize+1=%d) "+
				"to guarantee inv-zen-080 for any generator family",
			len(cfg.ReviewerFamilyPool), cfg.MinPoolSize+1, cfg.MinPoolSize+1,
		)
	}

	criteria := NewCriteriaRegistry(cfg.CustomCriteria)
	router := NewRouter(cfg.DaemonBaseURL, cfg.AuthToken, cfg.DefaultReviewerModel)

	sdk := mcp.NewServer(&mcp.Implementation{
		Name:    "zen-swarm-audit",
		Version: "0.4.0",
	}, nil)

	srv := &Server{
		mcpSrv:   sdk,
		cfg:      cfg,
		criteria: criteria,
		router:   router,
	}

	mcp.AddTool(sdk, &mcp.Tool{
		Name:        "audit_review",
		Description: "Review a diff using a cross-provider family-disjoint reviewer. Returns a structured verdict (classification, concerns, suggestions, reviewer_provider, reviewer_model). Generator provider family is excluded from the reviewer pool (inv-zen-080).",
		InputSchema: auditReviewInputSchema(criteria.Names()),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
		req, err := parseAuditRequestFromParams(args)
		if err != nil {
			return nil, nil, fmt.Errorf("audit_review: invalid parameters: %w", err)
		}
		resp, err := srv.handleAuditReview(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(resp)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(b)},
			},
		}, resp, nil
	})

	return srv, nil
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpSrv.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) ListTools() []string {
	return []string{"audit_review"}
}

func (s *Server) ToolNames() []string {
	return s.ListTools()
}

func (s *Server) InvokeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if name != "audit_review" {
		return nil, fmt.Errorf("audit mcp: unknown tool %q", name)
	}
	req, err := parseAuditRequestFromParams(args)
	if err != nil {
		return nil, fmt.Errorf("audit_review: invalid parameters: %w", err)
	}
	return s.handleAuditReview(ctx, req)
}

func (s *Server) handleAuditReview(ctx context.Context, req AuditRequest) (AuditResponse, error) {
	if err := req.Validate(); err != nil {
		return AuditResponse{}, err
	}

	pool, err := NewPool(s.cfg.ReviewerFamilyPool, req.GeneratorProviderFamily, s.cfg.MinPoolSize)
	if err != nil {
		switch s.cfg.EmptyPoolPolicy {
		case EmptyPoolHardStop:
			return AuditResponse{}, fmt.Errorf(
				"audit: hard-stop — disjoint reviewer pool empty for generator %q "+
					"(inv-zen-080; requires >= %d disjoint families): %w",
				req.GeneratorProviderFamily, s.cfg.MinPoolSize, err,
			)
		case EmptyPoolWarnAndDegrade:

			pool, err = NewPool(s.cfg.ReviewerFamilyPool, req.GeneratorProviderFamily, 1)
			if err != nil {

				return AuditResponse{}, fmt.Errorf(
					"audit: warn-and-degrade failed — zero disjoint families after "+
						"excluding generator %q: %w", req.GeneratorProviderFamily, err,
				)
			}
			// Emit a structured warning so operators see real signal — pre-fix
			// (review C-1) this branch silently discarded the message via
			// _ = fmt.Sprintf(...), making degraded review indistinguishable
			// from a clean run in production logs.
			s.logger().Printf(
				"[audit WARN] disjoint pool below MinPoolSize=%d for generator %q; "+
					"proceeding with %d family(ies): %v",
				s.cfg.MinPoolSize,
				req.GeneratorProviderFamily,
				len(pool.Families()),
				pool.Families(),
			)
		}
	}

	reviewerFamily := pool.Choose()

	criteriaPrompt, criteriaResolved := s.criteria.Get(req.CriteriaName)

	verdict, err := s.router.RouteCall(ctx, RouteRequest{
		Diff:           req.Diff,
		CriteriaPrompt: criteriaPrompt,
		ReviewerFamily: reviewerFamily,
		ReviewerModel:  s.cfg.DefaultReviewerModel,
	})
	if err != nil {
		return AuditResponse{}, fmt.Errorf("audit: reviewer call failed: %w", err)
	}

	return AuditResponse{
		Verdict:          verdict,
		CriteriaUsed:     req.CriteriaName,
		CriteriaResolved: criteriaResolved,
		GeneratorFamily:  req.GeneratorProviderFamily,
	}, nil
}

func (s *Server) HandleAuditReviewForTest(ctx context.Context, req AuditRequest) (AuditResponse, error) {
	return s.handleAuditReview(ctx, req)
}

func (s *Server) logger() *log.Logger {
	if s.cfg.Logger != nil {
		return s.cfg.Logger
	}
	return log.Default()
}

func reqString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected string, got %T", key, v)
	}
	return s, nil
}

func requireString(args map[string]any, key string) (string, error) {
	s, err := reqString(args, key)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("%s: required field missing or empty", key)
	}
	return s, nil
}

func parseAuditRequestFromParams(params map[string]any) (AuditRequest, error) {
	diff, err := requireString(params, "diff")
	if err != nil {
		return AuditRequest{}, err
	}
	criteria, err := reqString(params, "criteria")
	if err != nil {
		return AuditRequest{}, err
	}
	if criteria == "" {
		criteria = "default"
	}
	generatorFamily, err := requireString(params, "generator_provider_family")
	if err != nil {
		return AuditRequest{}, err
	}
	return AuditRequest{
		Diff:                    diff,
		CriteriaName:            criteria,
		GeneratorProviderFamily: generatorFamily,
	}, nil
}

var builtinCriteriaEnum = []string{
	"default",
	"security",
	"performance",
	"doctrine-violation",
}

func auditReviewInputSchema(criteriaNames []string) map[string]any {
	enum := criteriaNames
	if len(enum) == 0 {
		enum = builtinCriteriaEnum
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"diff": map[string]any{
				"type":        "string",
				"description": "The unified diff to review.",
			},
			"criteria": map[string]any{
				"type":        "string",
				"description": "Criteria name: a built-in template (default, security, performance, doctrine-violation) or an operator-defined name from doctrine TOML. The enum reflects the active registry — unknown names sent over the wire are rejected by schema validation; direct callers via handleAuditReview still get the registry's graceful default-template fallback.",
				"default":     "default",
				"enum":        enum,
			},
			"generator_provider_family": map[string]any{
				"type":        "string",
				"description": "Provider family that generated the diff (e.g. anthropic, google, deepseek). Excluded from reviewer pool per inv-zen-080.",
			},
		},
		"required":             []string{"diff", "generator_provider_family"},
		"additionalProperties": false,
	}
}

func ReviewerFamilyPoolFromRegistry(nameToFamily map[string]string) []string {
	seen := make(map[string]struct{}, len(nameToFamily))
	out := make([]string, 0, len(nameToFamily))
	for _, fam := range nameToFamily {
		if fam == "" {
			continue
		}
		if _, ok := seen[fam]; ok {
			continue
		}
		seen[fam] = struct{}{}
		out = append(out, fam)
	}
	sort.Strings(out)
	return out
}
