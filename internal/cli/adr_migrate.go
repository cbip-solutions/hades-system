// SPDX-License-Identifier: MIT
// Package cli — adr_migrate.go.
//
// `zen adr migrate [--dry-run] [--plan <range>]` is a one-time operator
// tool: walk architecture records parse legacy markdown headers, emit
// Structured MADR YAML frontmatter. Idempotent — already-migrated files
// are skipped.
//
// Implementation note: migrate calls internal/adr.MigrateDirectory directly
// (no daemon round-trip). There is no /v1/adr/migrate daemon route; migration
// is a local filesystem operation that the operator runs once from the repo
// root. This approach avoids the complexity of streaming file I/O through an
// HTTP route for a one-time offline task.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/adr"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func adrMigrateCmd() *cobra.Command {
	var dryRun bool
	var planRange string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "One-time legacy ADR migration tool (markdown headers → MADR YAML)",
		Long: `migrate is a ONE-TIME operator tool. Walks docs/decisions/*.md,
parses each existing markdown header (status, plan, etc.), and emits
Structured MADR YAML frontmatter at top of file. Idempotent — running
again on already-migrated ADRs is a no-op (skipped).

Migration runs entirely on local files (no daemon round-trip). Run from
any directory inside the zen-swarm repo. Use --dry-run first to preview
per-file results before committing the bulk migration.

Output per file:
  success <path>   — migrated and written
  skipped <path>   — already has frontmatter; not modified
  failed  <path>   — parse or write error (see error line below)`,
		Example: `  zen adr migrate --dry-run          # preview without writing
  zen adr migrate --plan plan-1      # supply default plan tag for ADRs missing **Plan**: header
  zen adr migrate                    # migrate all legacy ADRs in docs/decisions/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := findRepoRoot()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("adr migrate: %w", err))
			}
			dir := filepath.Join(root, "docs", "decisions")
			opts := adr.MigrateOptions{
				DryRun:        dryRun,
				PlanFromRange: planRange,
			}
			report, err := adr.MigrateDirectory(cmd.Context(), dir, opts)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("adr migrate: %w", err))
			}
			out := cmd.OutOrStdout()
			for _, r := range report.Files {
				fmt.Fprintf(out, "%s %s\n", r.Status, r.Path)
				if r.Error != nil {
					fmt.Fprintf(out, "  error: %v\n", r.Error)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview migration without writing files")
	cmd.Flags().StringVar(&planRange, "plan", "", "default plan range for ADRs without **Plan** header (e.g. plan-1)")
	return cmd
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {

			return "", fmt.Errorf("could not locate repo root (no go.mod found walking up from %s)", dir)
		}
		dir = parent
	}
}
