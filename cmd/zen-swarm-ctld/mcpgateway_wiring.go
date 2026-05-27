// SPDX-License-Identifier: MIT
// cmd/zen-swarm-ctld/mcpgateway_wiring.go
//
// Extracted from main.go for testability (the buildDispatcher function is
// the testable seam; main.go retains only the os.Exit-on-error glue).
//
// Q5=A: a nil caronte engine
// returns ErrCaronteBootstrapRequired → main.go aborts startup. The code-graph
// engine is in-process (no subprocess/binary to install), so the failure is a
// daemon-construction bug, not a missing dependency.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	mcpaudit "github.com/cbip-solutions/hades-system/internal/mcp/audit"
	mcpbudget "github.com/cbip-solutions/hades-system/internal/mcp/budget"
	mcpsshexec "github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

type researchServer interface {
	ToolNames() []string
	InvokeTool(ctx context.Context, name string, args map[string]any) (any, error)
}

type mcpgatewayDeps struct {
	caronte mcpgateway.CaronteEngine
	audit   mcpgateway.AuditEmitter
	rbacCfg mcpgateway.RBACConfig

	research researchServer
	budget   *mcpbudget.Server
	audit5   *mcpaudit.Server
	sshexec  *mcpsshexec.Server
}

func buildDispatcher(deps mcpgatewayDeps) (*mcpgateway.Dispatcher, error) {
	if deps.audit == nil {
		deps.audit = mcpgateway.NopAuditEmitter()
	}

	caronteProxy := mcpgateway.NewCaronteProxy(deps.caronte, deps.audit)
	if err := caronteProxy.EnsureReachable(context.Background()); err != nil {
		return nil, fmt.Errorf("mcpgateway boot: %w", err)
	}

	evaluator := buildDoctrineEvaluator(newAuditEmitterAdapter(deps.audit))
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:     deps.audit,
		RBACCfg:   deps.rbacCfg,
		Evaluator: evaluator,
	})

	if err := registerSubsystemNamed(d, "caronte", newCaronteSubsystem(caronteProxy)); err != nil {
		return nil, err
	}

	if deps.research != nil {
		if err := registerSubsystemNamed(d, "research", newResearchSubsystem(deps.research)); err != nil {
			return nil, err
		}
	}
	if deps.budget != nil {
		if err := registerSubsystemNamed(d, "budget", newBudgetSubsystem(deps.budget)); err != nil {
			return nil, err
		}
	}
	if deps.audit5 != nil {
		if err := registerSubsystemNamed(d, "audit", newAuditSubsystem(deps.audit5)); err != nil {
			return nil, err
		}
	}
	if deps.sshexec != nil {
		if err := registerSubsystemNamed(d, "sshexec", newSSHExecSubsystem(deps.sshexec)); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func registerSubsystemNamed(d *mcpgateway.Dispatcher, name string, sub mcpgateway.Subsystem) error {
	if err := d.RegisterSubsystem(sub); err != nil {
		return fmt.Errorf("register %s: %w", name, err)
	}
	return nil
}

func defaultRBACConfig() mcpgateway.RBACConfig {
	return mcpgateway.RBACConfig{
		DoctrineDisabled: map[mcpgateway.Doctrine][]string{
			mcpgateway.DoctrineCapaFirewall: {
				"mcp_zen-swarm_caronte_query",
				"mcp_zen-swarm_caronte_context",
				"mcp_zen-swarm_caronte_impact",
				"mcp_zen-swarm_caronte_get_contract",
				"mcp_zen-swarm_caronte_get_consumers",
				"mcp_zen-swarm_caronte_get_breaking_changes",
				"mcp_zen-swarm_caronte_trace_api_call",
				"mcp_zen-swarm_caronte_get_workspace",
				"mcp_zen-swarm_caronte_federation_health",
				"mcp_zen-swarm_caronte_contract_diff",
				"mcp_zen-swarm_caronte_get_why_breaking_change",

				"mcp_zen-swarm_research_agentic",
			},
		},
	}
}

type internalMCPSubsystem struct {
	name  string
	tools []mcpgateway.ToolEntry
}

func (a *internalMCPSubsystem) Name() string                  { return a.name }
func (a *internalMCPSubsystem) Tools() []mcpgateway.ToolEntry { return a.tools }

func buildInternalMCPSubsystem(
	name string,
	toolNames []string,
	invoke func(ctx context.Context, name string, args map[string]any) (any, error),
) *internalMCPSubsystem {
	tools := make([]mcpgateway.ToolEntry, 0, len(toolNames))
	handler := invokeAdapter(invoke)
	for _, tn := range toolNames {
		canonical, err := mcpgateway.NewToolName(name, tn)
		if err != nil {

			continue
		}
		tools = append(tools, mcpgateway.ToolEntry{
			Name:    canonical,
			Handler: handler,
			Meta:    mcpgateway.ToolMeta{Description: name + " MCP — " + tn},
		})
	}
	return &internalMCPSubsystem{name: name, tools: tools}
}

func invokeAdapter(invoke func(ctx context.Context, name string, args map[string]any) (any, error)) mcpgateway.Handler {
	return func(ctx context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		raw, err := invoke(ctx, req.Tool.Tool(), req.Args)
		if err != nil {
			return mcpgateway.CallResponse{
				IsError:   true,
				Content:   []mcpgateway.CallContentItem{{Type: "text", Text: err.Error()}},
				Subsystem: req.Tool.Subsystem(),
			}, nil
		}
		body, mErr := coerceToText(raw)
		if mErr != nil {
			return mcpgateway.CallResponse{}, fmt.Errorf("marshal: %w", mErr)
		}
		return mcpgateway.CallResponse{
			Content:   []mcpgateway.CallContentItem{{Type: "text", Text: body}},
			Subsystem: req.Tool.Subsystem(),
		}, nil
	}
}

func coerceToText(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	case json.RawMessage:
		return string(t), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func newResearchSubsystem(s researchServer) *internalMCPSubsystem {
	return buildInternalMCPSubsystem("research", s.ToolNames(), s.InvokeTool)
}

func newBudgetSubsystem(s *mcpbudget.Server) *internalMCPSubsystem {
	return buildInternalMCPSubsystem("budget", s.ToolNames(), s.InvokeTool)
}

func newAuditSubsystem(s *mcpaudit.Server) *internalMCPSubsystem {
	return buildInternalMCPSubsystem("audit", s.ToolNames(), s.InvokeTool)
}

func newSSHExecSubsystem(s *mcpsshexec.Server) *internalMCPSubsystem {
	return buildInternalMCPSubsystem("sshexec", s.ToolNames(), s.InvokeTool)
}
