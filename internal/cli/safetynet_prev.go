// SPDX-License-Identifier: MIT
// Package cli — safetynet_prev.go.
//
// `hades safetynet prev {install,show,exec}` manages the pinned-prior
// bin/hades-prev binary (design choice C element 1). The exec sub-command pipes a
// one-shot argv through bin/hades-prev so operators can validate that
// substrate regressions don't reproduce on the prior known-good build.
package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func safetynetPrevCmd() *cobra.Command {
	prev := &cobra.Command{
		Use:   "prev",
		Short: "Manage bin/hades-prev (pinned-prior binary)",
	}
	prev.AddCommand(safetynetPrevInstallCmd())
	prev.AddCommand(safetynetPrevShowCmd())
	prev.AddCommand(safetynetPrevExecCmd())
	return prev
}

func safetynetPrevInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install bin/hades-prev from the pinned-prior release tarball",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := plan5ClientFromCmd(cmd).SafetynetPrevInstall(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed: %s\n", resp["version"])
			return nil
		},
	}
}

func safetynetPrevShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show installed prev binary path + version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			resp, err := plan5ClientFromCmd(cmd).SafetynetPrevShow(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "path: %s\nversion: %s\n", resp["path"], resp["version"])
			return nil
		},
	}
}

func safetynetPrevExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <cmd> [args...]",
		Short: "Run a one-shot command through bin/hades-prev",

		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("exec requires at least one argument")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := plan5ClientFromCmd(cmd).SafetynetPrevExec(ctx, args)
			if err != nil {
				return err
			}
			if v, ok := resp["stdout"]; ok {
				fmt.Fprintln(cmd.OutOrStdout(), v)
			}
			return nil
		},
	}
}
