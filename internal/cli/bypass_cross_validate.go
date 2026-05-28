// SPDX-License-Identifier: MIT
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func newBypassCrossValidateCmd() *cobra.Command {
	var configPath, plugin, reportPath string
	cmd := &cobra.Command{
		Use:   "cross-validate",
		Short: "Diff extracted bypass-config against a community plugin source",
		Long:  "Cross-validate the extracted bypass-config.json (from\n'hades bypass extract-config') against a community plugin source\n(meridian or griffinmartin). Emits a per-field diff report\n(MATCH / DIFF / MISSING). Implements spec §2 design choice-C cold-start\ncross-validation.",

		RunE: func(cmd *cobra.Command, args []string) error {
			if plugin != "meridian" && plugin != "griffinmartin" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--plugin must be one of: meridian, griffinmartin (got %q)", plugin))
			}
			return runToolBinary("cross-validate", "-config", configPath, "-plugin", plugin, "-report", reportPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "./bypass-config.json", "path to extracted config")
	cmd.Flags().StringVar(&plugin, "plugin", "meridian", "community plugin: meridian | griffinmartin")
	cmd.Flags().StringVar(&reportPath, "report", "./cross-validate-report.txt", "diff report output path")
	return cmd
}
