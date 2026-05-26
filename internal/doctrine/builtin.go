// SPDX-License-Identifier: MIT
// §7.3. Three doctrines: max-scope, default, capa-firewall. Each
// constructor returns a fresh Schema value (no shared maps, no global
// mutation). Per spec §0.2 MaxScopeBuiltin is the OOTB experience: an
// operator who never edits TOML still gets max-scope behaviour.
//
// Naming constructors are *Builtin to avoid collision with Plan 1's
// MaxScope/Default/CapaFirewall struct types in doctrine.go.

package doctrine

import (
	"fmt"
	"time"
)

func MaxScopeBuiltin() Schema {
	return Schema{
		SchemaVersion: 1,
		Name:          "max-scope",
		Research: ResearchAxis{
			CadencePerStage: map[string]string{
				"design":     "always",
				"brainstorm": "always",
				"spec":       "always",
				"build":      "on-demand",
				"verify":     "on-demand",
				"release":    "always",
			},
			Depth:          "deep",
			Sources:        []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"},
			CacheTTL:       Duration(7 * 24 * time.Hour),
			AgenticMaxIter: 5,
		},
		Subprocess: SubprocessAxis{
			EphemeralDefaultTimeout: Duration(60 * time.Minute),
			PersistentTTLSliding:    Duration(8 * time.Hour),
			PreWarmPoolSize:         3,
		},
		Reviewer: ReviewerAxis{
			FamilyDisjointPool: []string{"anthropic", "google", "deepseek", "local-qwen"},
			CriteriaDefault:    "default",
		},
		Budget: BudgetAxis{
			Caps: BudgetCaps{
				Project:  Money("100.00 USD"),
				Doctrine: Money("500.00 USD"),
				Stage: map[string]Money{
					"design":  Money("5.00 USD"),
					"build":   Money("10.00 USD"),
					"verify":  Money("3.00 USD"),
					"release": Money("1.00 USD"),
				},
				Task: map[string]Money{
					"trivial": Money("0.05 USD"),
					"simple":  Money("0.20 USD"),
					"medium":  Money("1.00 USD"),
					"complex": Money("2.00 USD"),
				},
				Operation: map[string]Money{
					"audit_review":      Money("0.10 USD"),
					"research_dispatch": Money("0.50 USD"),
				},
			},
			PauseMode:         "descriptive",
			AnomalyZThreshold: 4.0,
			AnomalyWindowSize: 60,
		},
		Workforce: WorkforceAxis{
			WritablePathsPolicy:                  "non-overlapping",
			DoctrineReinforcementTemplatePointer: "templates/doctrine/max-scope.txt",
		},
		Apply: ApplyAxis{
			MergeStrategy:    "three-way",
			ConflictHandling: "manual",
		},
		Watcher: WatcherAxis{
			Cadence:   Duration(15 * time.Minute),
			CPUBudget: 0.05,
		},
		Gateway: GatewayAxis{

			DisabledTools: []string{},
		},
		SSHExec: SSHExecAxis{
			Allowlist: SSHExecAllowlist{

				Patterns: []string{
					"alembic *",
					"pytest *",
					"psql *",
					"docker compose -f docker/docker-compose.yml *",
					"git status",
					"git log",
				},
				Hosts: []string{"vps"},
			},

			Defaults: SSHExecDefaults{
				Timeout:   Duration(30 * time.Minute),
				MaxStdout: 64 * 1024 * 1024,
				MaxStderr: 8 * 1024 * 1024,
			},
		},
		Future: map[string]map[string]any{},
	}
}

func DefaultBuiltin() Schema {
	return Schema{
		SchemaVersion: 1,
		Name:          "default",
		Research: ResearchAxis{
			CadencePerStage: map[string]string{
				"design":     "on-demand",
				"brainstorm": "on-demand",
				"spec":       "on-demand",
				"build":      "never",
				"verify":     "on-demand",
				"release":    "on-demand",
			},
			Depth:          "medium",
			Sources:        []string{"web_search", "arxiv", "github_search"},
			CacheTTL:       Duration(7 * 24 * time.Hour),
			AgenticMaxIter: 3,
		},
		Subprocess: SubprocessAxis{
			EphemeralDefaultTimeout: Duration(30 * time.Minute),
			PersistentTTLSliding:    Duration(4 * time.Hour),
			PreWarmPoolSize:         1,
		},
		Reviewer: ReviewerAxis{
			FamilyDisjointPool: []string{"anthropic", "google"},
			CriteriaDefault:    "default",
		},
		Budget: BudgetAxis{
			Caps: BudgetCaps{
				Project:  Money("50.00 USD"),
				Doctrine: Money("200.00 USD"),
				Stage: map[string]Money{
					"design":  Money("3.00 USD"),
					"build":   Money("5.00 USD"),
					"verify":  Money("1.50 USD"),
					"release": Money("0.50 USD"),
				},
				Task: map[string]Money{
					"trivial": Money("0.02 USD"),
					"simple":  Money("0.10 USD"),
					"medium":  Money("0.50 USD"),
					"complex": Money("1.00 USD"),
				},
				Operation: map[string]Money{
					"audit_review":      Money("0.05 USD"),
					"research_dispatch": Money("0.25 USD"),
				},
			},
			PauseMode:         "quiet",
			AnomalyZThreshold: 5.0,
			AnomalyWindowSize: 60,
		},
		Workforce: WorkforceAxis{
			WritablePathsPolicy:                  "non-overlapping",
			DoctrineReinforcementTemplatePointer: "templates/doctrine/default.txt",
		},
		Apply: ApplyAxis{
			MergeStrategy:    "three-way",
			ConflictHandling: "manual",
		},
		Watcher: WatcherAxis{
			Cadence:   Duration(30 * time.Minute),
			CPUBudget: 0.02,
		},
		Gateway: GatewayAxis{

			DisabledTools: []string{},
		},
		SSHExec: SSHExecAxis{
			Allowlist: SSHExecAllowlist{

				Patterns: []string{
					"alembic *",
					"pytest *",
					"git status",
					"git log",
				},
				Hosts: []string{"vps"},
			},

			Defaults: SSHExecDefaults{
				Timeout:   Duration(10 * time.Minute),
				MaxStdout: 16 * 1024 * 1024,
				MaxStderr: 4 * 1024 * 1024,
			},
		},
		Future: map[string]map[string]any{},
	}
}

func CapaFirewallBuiltin() Schema {
	return Schema{
		SchemaVersion: 1,
		Name:          "capa-firewall",
		Research: ResearchAxis{
			CadencePerStage: map[string]string{
				"design":     "always",
				"brainstorm": "always",
				"spec":       "always",
				"build":      "on-demand",
				"verify":     "always",
				"release":    "always",
			},
			Depth:          "deep",
			Sources:        []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"},
			CacheTTL:       Duration(14 * 24 * time.Hour),
			AgenticMaxIter: 7,
		},
		Subprocess: SubprocessAxis{
			EphemeralDefaultTimeout: Duration(120 * time.Minute),
			PersistentTTLSliding:    Duration(12 * time.Hour),
			PreWarmPoolSize:         2,
		},
		Reviewer: ReviewerAxis{
			FamilyDisjointPool: []string{"local-qwen", "deepseek"},
			CriteriaDefault:    "doctrine-violation",
		},
		Budget: BudgetAxis{
			Caps: BudgetCaps{
				Project:  Money("20.00 USD"),
				Doctrine: Money("80.00 USD"),
				Stage: map[string]Money{
					"design":  Money("2.00 USD"),
					"build":   Money("3.00 USD"),
					"verify":  Money("1.00 USD"),
					"release": Money("0.20 USD"),
				},
				Task: map[string]Money{
					"trivial": Money("0.01 USD"),
					"simple":  Money("0.05 USD"),
					"medium":  Money("0.20 USD"),
					"complex": Money("0.50 USD"),
				},
				Operation: map[string]Money{
					"audit_review":      Money("0.02 USD"),
					"research_dispatch": Money("0.10 USD"),
				},
			},
			PauseMode:         "fail_loud",
			AnomalyZThreshold: 3.0,
			AnomalyWindowSize: 30,
		},
		Workforce: WorkforceAxis{
			WritablePathsPolicy:                  "non-overlapping",
			DoctrineReinforcementTemplatePointer: "templates/doctrine/capa-firewall.txt",
		},
		Apply: ApplyAxis{
			MergeStrategy:    "three-way",
			ConflictHandling: "manual",
		},
		Watcher: WatcherAxis{
			Cadence:   Duration(10 * time.Minute),
			CPUBudget: 0.10,
		},
		Gateway: GatewayAxis{

			DisabledTools: []string{
				"mcp_zen-swarm_caronte_query",
				"mcp_zen-swarm_caronte_context",
				"mcp_zen-swarm_caronte_impact",
				"mcp_zen-swarm_research_agentic",
			},
		},
		SSHExec: SSHExecAxis{
			Allowlist: SSHExecAllowlist{

				Patterns: []string{
					"git status",
					"git log",
				},
				Hosts: []string{"vps"},
			},

			Defaults: SSHExecDefaults{
				Timeout:   Duration(2 * time.Minute),
				MaxStdout: 4 * 1024 * 1024,
				MaxStderr: 1024 * 1024,
			},
		},
		Future: map[string]map[string]any{},
	}
}

func Builtin(name string) (Schema, error) {
	switch name {
	case "max-scope":
		return MaxScopeBuiltin(), nil
	case "default":
		return DefaultBuiltin(), nil
	case "capa-firewall":
		return CapaFirewallBuiltin(), nil
	}
	return Schema{}, fmt.Errorf("doctrine: unknown builtin name %q (expected: max-scope|default|capa-firewall)", name)
}

func BuiltinNames() []string {
	return []string{"max-scope", "default", "capa-firewall"}
}
