// SPDX-License-Identifier: MIT
// Package cli — adr_propose.go.
//
// `hades adr propose <topic>` is the interactive ADR draft workflow:
// 1. POST /v1/adr/propose → daemon auto-assigns next ID, returns ADR stub.
// 2. Write prefilled MADR YAML frontmatter + empty sections to temp file.
// 3. Open editorRunner (resolveEditorName() → $VISUAL | $EDITOR | vi).
// 4. Read back edited content; abort if body is empty.
// 5. Print confirmation with ADR ID (accept/reject workflow is `hades adr
// accept <id> --reason <X>` — a separate I-7 command).
//
// The editorRunner package-level var enables test substitution without
// spawning a real vi process.
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func adrProposeCmd() *cobra.Command {
	var plan string
	cmd := &cobra.Command{
		Use:   "propose <topic>",
		Short: "Interactive draft (auto-assigns next id; opens $EDITOR with prefilled MADR frontmatter)",
		Args:  cobra.ExactArgs(1),
		Long: `propose drafts a new ADR with prefilled MADR YAML frontmatter.
The daemon auto-assigns the next available ID in the active plan range;
status defaults to proposed; plan auto-detected from active branch.

The operator's $EDITOR (or $VISUAL or vi) opens the draft. On save the
content is ready for review. Use 'hades adr accept <id> --reason <X>' to
formally accept or 'hades adr reject <id> --reason <X>' to reject.`,
		Example: "  hades adr propose tessera-batch-cadence-tuning\n  hades adr propose multi-tenant-auth --plan HADES design",

		RunE: func(cmd *cobra.Command, args []string) error {
			topic := args[0]
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			c := newClientFromCmd(cmd)

			draft, err := c.ADRProposeWithPlan(ctx, topic, plan)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("propose: %w", err))
			}

			planTag := plan
			if planTag == "" {
				planTag = draft.Plan
			}
			prefilled := fmt.Sprintf(`---
id: %s
status: proposed
plan: %s
risk_level: low
title: %s
---

## Context

<!-- Why is this decision necessary? What forces are at play? -->

## Decision

<!-- What is the change being proposed and/or decided? -->

## Consequences

<!-- What becomes easier or harder as a result of this change? -->
`, draft.ID, planTag, strings.ReplaceAll(topic, "-", " "))

			tmp, err := os.CreateTemp("", "hades-adr-*.md")
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("create temp file: %w", err))
			}
			path := tmp.Name()
			defer os.Remove(path)

			if _, err := tmp.WriteString(prefilled); err != nil {
				_ = tmp.Close()
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("write draft: %w", err))
			}
			if err := tmp.Close(); err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("close temp: %w", err))
			}

			if err := editorRunner(path); err != nil {
				return err
			}

			body, err := os.ReadFile(path)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read draft: %w", err))
			}
			if strings.TrimSpace(string(body)) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("empty draft; aborting"))
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"id=%s status=%s plan=%s\ndraft saved — use 'hades adr accept %s --reason <X>' to commit\n",
				draft.ID, "proposed", planTag, draft.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&plan, "plan", "", "Plan tag hint (e.g. HADES design)")
	return cmd
}
