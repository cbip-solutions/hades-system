// SPDX-License-Identifier: MIT
// Package cli — orchestrator.go.
//
// `zen orchestrator` exposes six subcommands backed by the daemon's
// /v1/orchestrator/* RESTful surface:
//
// - status — per-tier breaker state + active pins + 30d cost summary
// - pin — operator pin (scope=session|project|global, --tier required,
// optional --ttl, --provider, --reason)
// - unpin — operator unpin by scope OR --all (mutually exclusive)
// - pins — list every active (non-expired) pin
// - probe — trigger circuit-breaker recovery on each non-Closed tier
// - history — current state per tier (post-rescope: CircuitBreaker
// does not yet track state-transition history)
//
// Replaces the stub set (status/pin/unpin via /switch + 4-tier
// numeric mapping). Tier names are now canonical providers.Tier strings
// ("in-house" / "openclaude" / etc.) so future tiers added in
// buildOrchestrator widen this surface without CLI changes.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewOrchestratorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Orchestrator controls (status/pin/unpin/pins/probe/history + state/depth/pool/capture/replay)",
	}
	attachPlan5DaemonURLFlag(cmd)
	cmd.AddCommand(orchStatusCmd())
	cmd.AddCommand(orchPinCmd())
	cmd.AddCommand(orchUnpinCmd())
	cmd.AddCommand(orchPinsCmd())
	cmd.AddCommand(orchProbeCmd())
	cmd.AddCommand(orchHistoryCmd())
	cmd.AddCommand(orchStateCmd())
	cmd.AddCommand(orchDepthCmd())
	cmd.AddCommand(orchPoolCmd())
	cmd.AddCommand(orchCaptureCmd())
	cmd.AddCommand(orchReplayCmd())
	return cmd
}

func orchStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show per-tier circuit breaker state, active pins, and 30d cost summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).OrchestratorStatus(ctx)
			if err != nil {
				return err
			}
			fmt.Println("Tiers:")
			if len(r.Tiers) == 0 {
				fmt.Println("  (none configured)")
			}
			for _, t := range r.Tiers {
				fmt.Printf("  %-16s %s\n", t.Tier, t.State)
			}
			fmt.Println()
			fmt.Println("Active pins:")
			if len(r.Pins) == 0 {
				fmt.Println("  (none)")
			}
			for _, p := range r.Pins {
				printPin(cmd.OutOrStdout(), p)
			}
			fmt.Println()
			fmt.Println("30d cost summary:")
			if len(r.Costs) == 0 {
				fmt.Println("  (none)")
			}
			for _, c := range r.Costs {
				fmt.Printf("  %-16s $%.4f (%s)\n", c.Tier, c.Total, c.Window)
			}
			return nil
		},
	}
}

func orchPinCmd() *cobra.Command {
	var scope, project, session, tier, provider, forStr, reason string
	c := &cobra.Command{
		Use:   "pin",
		Short: "Pin a tier for a scope (session|project|global)",
		Long: `Pin a tier for a scope. The orchestrator resolves pins in the
hierarchy session > project > global; the first non-expired match wins.

Required flags: --scope, --tier
Required for scope=project: --project
Required for scope=session: --session
Optional: --provider, --for (e.g. 1h30m), --reason`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validatePinFlags(scope, project, session, tier); err != nil {
				return err
			}
			req := client.OrchestratorPinReq{
				Scope:    scope,
				Project:  project,
				Session:  session,
				Tier:     tier,
				Provider: provider,
				TTL:      forStr,
				Reason:   reason,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := bypassNewClient(cmd).OrchestratorPin(ctx, req); err != nil {
				return err
			}
			summary := scope
			switch scope {
			case "project":
				summary += "/" + project
			case "session":
				summary += "/" + session
			}
			fmt.Printf("pinned: scope=%s tier=%s", summary, tier)
			if forStr != "" {
				fmt.Printf(" ttl=%s", forStr)
			}
			fmt.Println()
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "", "session|project|global")
	c.Flags().StringVar(&project, "project", "", "project id (required for scope=project)")
	c.Flags().StringVar(&session, "session", "", "session id (required for scope=session)")
	c.Flags().StringVar(&tier, "tier", "", "canonical tier name (e.g. in-house, openclaude)")
	c.Flags().StringVar(&provider, "provider", "", "optional provider name within the tier")
	c.Flags().StringVar(&forStr, "for", "", "optional TTL (Go duration syntax, e.g. 1h30m)")
	c.Flags().StringVar(&reason, "reason", "", "optional free-text audit trail")
	return c
}

func validatePinFlags(scope, project, session, tier string) error {
	if scope == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope is required (session|project|global)"))
	}
	if tier == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--tier is required"))
	}
	switch scope {
	case "global":
		if project != "" || session != "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=global must NOT specify --project or --session"))
		}
	case "project":
		if project == "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=project requires --project"))
		}
		if session != "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=project must NOT specify --session"))
		}
	case "session":
		if session == "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=session requires --session"))
		}
		if project != "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=session must NOT specify --project"))
		}
	default:
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope must be one of session|project|global; got %q", scope))
	}
	return nil
}

func orchUnpinCmd() *cobra.Command {
	var scope, project, session string
	var all bool
	c := &cobra.Command{
		Use:   "unpin",
		Short: "Unpin a scope (or --all to clear every active pin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateUnpinFlags(scope, project, session, all); err != nil {
				return err
			}
			req := client.OrchestratorUnpinReq{
				Scope:   scope,
				Project: project,
				Session: session,
				All:     all,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := bypassNewClient(cmd).OrchestratorUnpin(ctx, req); err != nil {
				return err
			}
			if all {
				fmt.Println("unpinned: all active pins cleared")
			} else {
				summary := scope
				switch scope {
				case "project":
					summary += "/" + project
				case "session":
					summary += "/" + session
				}
				fmt.Printf("unpinned: scope=%s\n", summary)
			}
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "", "session|project|global")
	c.Flags().StringVar(&project, "project", "", "project id (for scope=project)")
	c.Flags().StringVar(&session, "session", "", "session id (for scope=session)")
	c.Flags().BoolVar(&all, "all", false, "clear every active pin (mutually exclusive with --scope)")
	return c
}

func validateUnpinFlags(scope, project, session string, all bool) error {
	if all && (scope != "" || project != "" || session != "") {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--all is mutually exclusive with --scope/--project/--session"))
	}
	if !all && scope == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("either --all OR --scope is required"))
	}
	if all {
		return nil
	}
	switch scope {
	case "global":
		if project != "" || session != "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=global must NOT specify --project or --session"))
		}
	case "project":
		if project == "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=project requires --project"))
		}
	case "session":
		if session == "" {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope=session requires --session"))
		}
	default:
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--scope must be one of session|project|global; got %q", scope))
	}
	return nil
}

func orchPinsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pins",
		Short: "List every active (non-expired) pin",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).OrchestratorPins(ctx)
			if err != nil {
				return err
			}
			if len(r.Pins) == 0 {
				fmt.Println("no active pins")
				return nil
			}
			for _, p := range r.Pins {
				printPin(cmd.OutOrStdout(), p)
			}
			return nil
		},
	}
}

func orchProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe",
		Short: "Trigger circuit-breaker recovery on each non-Closed tier",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).OrchestratorProbe(ctx)
			if err != nil {
				return err
			}
			fmt.Println("Probe result:")
			if len(r.Tiers) == 0 {
				fmt.Println("  (no tiers configured)")
				return nil
			}
			for _, t := range r.Tiers {
				fmt.Printf("  %-16s %s\n", t.Tier, t.State)
			}
			return nil
		},
	}
}

func orchHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Show per-tier state (post-rescope: current state, transition history not tracked yet)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, err := bypassNewClient(cmd).OrchestratorHistory(ctx)
			if err != nil {
				return err
			}
			if r.Note != "" {
				fmt.Printf("Note: %s\n\n", r.Note)
			}
			fmt.Println("Tier states:")
			if len(r.Tiers) == 0 {
				fmt.Println("  (none configured)")
				return nil
			}
			for _, t := range r.Tiers {
				fmt.Printf("  %-16s %s\n", t.Tier, t.State)
			}
			return nil
		},
	}
}

func printPin(w io.Writer, p client.OrchestratorPinSummary) {
	scope := p.Scope
	if p.ScopeID != "" {
		scope += "/" + p.ScopeID
	}
	parts := []string{
		fmt.Sprintf("scope=%s", scope),
		fmt.Sprintf("tier=%s", p.Tier),
	}
	if p.Provider != "" {
		parts = append(parts, fmt.Sprintf("provider=%s", p.Provider))
	}
	if p.ExpiresAt == nil {
		parts = append(parts, "ttl=permanent")
	} else {
		remaining := time.Until(*p.ExpiresAt).Round(time.Second)
		parts = append(parts, fmt.Sprintf("expires_in=%s", remaining))
	}
	if p.Reason != "" {
		parts = append(parts, fmt.Sprintf("reason=%q", p.Reason))
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(parts, " "))
}
