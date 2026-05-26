// SPDX-License-Identifier: MIT
package budget

import (
	"context"
	"errors"
	"sort"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

var (
	_stdioCanonicalSentinel = AssertStdioCanonical

	_boundaryPreservedSentinel = AssertBoundaryPreserved
)

func AssertStdioCanonical() bool { return true }

func AssertBoundaryPreserved() bool { return true }

type Server struct {
	sdk      *mcp.Server
	bc       *client.BudgetClient
	tools    []string
	handlers map[string]toolHandler
}

type toolHandler func(ctx context.Context, args map[string]any) (any, error)

func NewServer(bc *client.BudgetClient) *Server {
	s := &Server{
		bc:       bc,
		handlers: make(map[string]toolHandler, 7),
	}
	s.registerHandlers()

	sdk := mcp.NewServer(&mcp.Implementation{
		Name:    "zen-mcp-budget",
		Version: "0.4.0",
	}, nil)
	s.bindToMCPServer(sdk)
	s.sdk = sdk
	return s
}

func (s *Server) Run() error {
	return s.sdk.Run(context.Background(), &mcp.StdioTransport{})
}

func (s *Server) ToolNames() []string {
	names := make([]string, len(s.tools))
	copy(names, s.tools)
	return names
}

func (s *Server) InvokeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	h, ok := s.handlers[name]
	if !ok {
		return nil, errors.New("budget mcp: unknown tool: " + name)
	}
	return h(ctx, args)
}

func (s *Server) registerHandlers() {
	s.handlers["rollup"] = s.handleRollup
	s.handlers["cap_status"] = s.handleCapStatus
	s.handlers["tag"] = s.handleTag
	s.handlers["anomaly_check"] = s.handleAnomalyCheck
	s.handlers["pause"] = s.handlePause
	s.handlers["resume"] = s.handleResume
	s.handlers["events"] = s.handleEvents

	names := make([]string, 0, len(s.handlers))
	for name := range s.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	s.tools = names
}

type toolSpec struct {
	name        string
	description string

	inputSchema map[string]any
}

var budgetAxisEnum = []string{"project", "doctrine", "stage", "task", "operation", "augmentation"}

var budgetScopeEnum = []string{"project", "doctrine", "stage", "worker_id"}

const budgetEventKindList = "cap_hit, anomaly_triggered, pause, resume, axis_tag, pre_call_blocked"

func (s *Server) toolSpecs() []toolSpec {
	return []toolSpec{
		{
			name:        "rollup",
			description: "Multi-axis cost rollup: aggregate total and per-value breakdown for a given axis (project/doctrine/stage/task/operation/augmentation) since a timestamp.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"axis": map[string]any{
						"type":        "string",
						"description": "Cost-attribution axis to roll up.",
						"enum":        budgetAxisEnum,
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Specific value of the axis to filter on (e.g. \"internal-platform-x\" for project axis).",
					},
					"since": map[string]any{
						"type":        "string",
						"format":      "date-time",
						"description": "Optional RFC3339 lower-bound timestamp; absent = no time filter.",
					},
				},
				"required":             []string{"axis", "value"},
				"additionalProperties": false,
			},
		},
		{
			name:        "cap_status",
			description: "Pre-call cap check: query remaining budget and blocked state for a given axis+value. Returns immediately; does not modify state.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"axis": map[string]any{
						"type":        "string",
						"description": "Cost-attribution axis being checked.",
						"enum":        budgetAxisEnum,
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Specific value of the axis (e.g. \"design\" for stage axis).",
					},
				},
				"required":             []string{"axis", "value"},
				"additionalProperties": false,
			},
		},
		{
			name:        "tag",
			description: "Post-call axis tagging: associate a cost_ledger row with up to four axis tags (project, stage, task, operation). Idempotent via INSERT OR IGNORE.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cost_id": map[string]any{
						"type":        "string",
						"description": "cost_ledger row id emitted by the dispatcher post-call.",
					},
					"axis_tags": map[string]any{
						"type":        "object",
						"description": "Map of axis_name → axis_value (string-to-string). Up to four axes.",
						"additionalProperties": map[string]any{
							"type": "string",
						},
					},
				},
				"required":             []string{"cost_id"},
				"additionalProperties": false,
			},
		},
		{
			name:        "anomaly_check",
			description: "Manual anomaly inspection: compute z-score for cost per minute in the given scope and sliding window. Returns z_score, mean, std, sample count.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{
						"type":        "string",
						"description": "Bifrost-style pause scope.",
						"enum":        budgetScopeEnum,
					},
					"window": map[string]any{
						"type":        "string",
						"description": "Optional sliding-window duration string (e.g. \"1h\", \"30m\"); empty = doctrine-configured default.",
					},
				},
				"required":             []string{"scope"},
				"additionalProperties": false,
			},
		},
		{
			name:        "pause",
			description: "Manual operator pause: activate a budget pause for a given scope (project/doctrine/stage/worker_id) with a reason. Paused scopes block LLM dispatch.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{
						"type":        "string",
						"description": "Bifrost-style pause scope to activate.",
						"enum":        budgetScopeEnum,
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Human-readable explanation stored in budget_pauses table.",
					},
				},
				"required":             []string{"scope", "reason"},
				"additionalProperties": false,
			},
		},
		{
			name:        "resume",
			description: "Manual operator resume: deactivate a budget pause for a given scope.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{
						"type":        "string",
						"description": "Bifrost-style pause scope to deactivate.",
						"enum":        budgetScopeEnum,
					},
				},
				"required":             []string{"scope"},
				"additionalProperties": false,
			},
		},
		{
			name:        "events",
			description: "Event log query: return budget events (" + budgetEventKindList + ") since a given timestamp.",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"since": map[string]any{
						"type":        "string",
						"format":      "date-time",
						"description": "Optional RFC3339 lower-bound timestamp; absent = no time filter.",
					},
				},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) bindToMCPServer(srv *mcp.Server) {
	for _, sp := range s.toolSpecs() {
		spName := sp.name
		spDesc := sp.description
		spSchema := sp.inputSchema
		h, ok := s.handlers[spName]
		if !ok || h == nil {
			// Defensive registerHandlers and toolSpecs MUST stay in lock-step.
			// A registered tool spec with no handler would silently drop the
			// invocation; surface it loudly at construction.
			panic("budget: tool spec without handler: " + spName)
		}
		mcp.AddTool(srv, &mcp.Tool{
			Name:        spName,
			Description: spDesc,
			InputSchema: spSchema,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
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

var ErrNilClient = errors.New("budget mcp: client is nil (test construction)")
