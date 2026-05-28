// SPDX-License-Identifier: MIT
// Package cli implements the hades CLI commands.
//
// All subcommands talk to hades-ctld via internal/client.
// scaffolds all ~25 commands; real implementations land in HADES design
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
			fmt.Printf("hades %s: command available; implementation pending Plan %d\n", use, plan)
			fmt.Printf("  ref: %s\n", planRef)
			fmt.Printf("  see: design records %d\n", plan)
			return nil
		},
	}
}

func notImplementedSubcommand(use, short string, plan int, planRef string) *cobra.Command {
	return notImplementedCmd(use, short, plan, planRef)
}
