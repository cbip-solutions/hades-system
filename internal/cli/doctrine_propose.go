// SPDX-License-Identifier: MIT
// Package cli — doctrine_propose.go.
//
// Adds the amendment-lifecycle sub-commands to the existing hades doctrine
// namespace (release N-7 owns show/list/validate/which/reload/
// diff/schema). Per Q10 C + spec §6.1:
//
// hades doctrine propose-list
// hades doctrine propose-show <id>
// hades doctrine ack <id>
// hades doctrine deny <id> --reason...
//
// All four route to the daemon /v1/doctrine/{propose-list,propose-show,
// ack,deny} endpoints. Deny additionally records reason in the eventlog
// as EvtOperatorAmendmentDeny (invariant operator-override audit;
// daemon-side enforcement).
package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func doctrineProposeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "propose-list",
		Short: "List pending and recent doctrine amendment proposals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			list, err := plan5ClientFromCmd(cmd).DoctrineProposeList(ctx)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			out := cmd.OutOrStdout()
			if len(list.Proposals) == 0 {
				fmt.Fprintln(out, "no proposals")
				return nil
			}
			for _, p := range list.Proposals {
				fmt.Fprintf(out,
					"%s [%s] %s   (cooldown remaining: %ds)\n",
					p.ID, p.Status, p.Title, p.CooldownRemain)
			}
			return nil
		},
	}
}

func doctrineProposeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "propose-show <id>",
		Short: "Display ADR proposal body + rationale",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p, err := plan5ClientFromCmd(cmd).DoctrineProposeShow(ctx, args[0])
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "# %s — %s\n", p.ID, p.Title)
			fmt.Fprintf(out, "Status: %s\n", p.Status)
			fmt.Fprintf(out, "Proposed at: %d\n", p.ProposedAt)
			fmt.Fprintf(out, "Cooldown remaining: %ds\n", p.CooldownRemain)
			if p.AppliedAt > 0 {
				fmt.Fprintf(out, "Applied at: %d\n", p.AppliedAt)
			}
			if p.RevertedAt > 0 {
				fmt.Fprintf(out, "Reverted at: %d\n", p.RevertedAt)
			}
			if p.OperatorReason != "" {
				fmt.Fprintf(out, "Operator reason: %s\n", p.OperatorReason)
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, p.BodyMarkdown)
			return nil
		},
	}
}

func doctrineAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack <id>",
		Short: "Operator-approve a proposal (calls amendment.Applier daemon-side)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.DoctrineDecision{ID: args[0]}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := plan5ClientFromCmd(cmd).DoctrineAck(ctx, req); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s applied\n", args[0])
			return nil
		},
	}
}

func doctrineDenyCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "deny <id> --reason <text>",
		Short: "Operator-reject a proposal (records reason in eventlog)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if reason == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), errors.New("--reason is required for deny (audited via EvtOperatorAmendmentDeny)"))
			}
			req := client.DoctrineDecision{ID: args[0], Reason: reason}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := plan5ClientFromCmd(cmd).DoctrineDeny(ctx, req); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s denied (reason: %s)\n", args[0], reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for denial (required, audited)")
	return cmd
}
