// SPDX-License-Identifier: MIT
// Package cli implements the zen CLI commands.
//
// All subcommands talk to zen-swarm-ctld via internal/client. Phase F
// scaffolds all ~25 commands; real implementations land in Plans 2-15.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func notImplementedCmd(use, short string, plan int, planRef string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("zen %s: command available; implementation pending Plan %d\n", use, plan)
			fmt.Printf("  ref: %s\n", planRef)
			fmt.Printf("  see: docs/superpowers/plans/2026-04-29-plan-%d-*.md\n", plan)
			return nil
		},
	}
}

func notImplementedSubcommand(use, short string, plan int, planRef string) *cobra.Command {
	return notImplementedCmd(use, short, plan, planRef)
}
