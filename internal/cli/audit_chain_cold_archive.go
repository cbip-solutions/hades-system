// SPDX-License-Identifier: MIT
// Package cli — audit_chain_cold_archive.go.
//
// `hades audit-chain cold-archive` manages the cold storage tier for archived
// Tessera partitions (spec §6.1 Q5 A). Two leaf commands:
//
// ls List archived partitions (per-project, table by default)
// restore Pull a partition from cold archive back to local hot tier
//
// `restore` is destructive (overwrites the local Tessera tile-log slice for
// the named partition) and therefore requires an interactive y/N confirmation
// prompt before dispatching the HTTP call. The prompt defaults to N per
// spec §6.5 (privacy-by-default; never auto-confirm destructive flows).
//
// Plan deviations (implementer brief): the plan-file sketched struct-based
// request/response types and a /v1/audit-chain/cold-archive/ls endpoint.
// H-7 actually shipped:
//
// - client.AuditColdArchiveList(ctx, projectID) []AuditColdArchiveEntry
// with AuditColdArchiveEntry{PartitionID, SizeBytes, ArchivedAt, ContentHash}
// on endpoint GET /v1/audit-chain/cold-archive/list (note: "list" not "ls")
//
// - client.AuditColdArchiveRestore(ctx, partitionID, projectID) AuditRestoreResult
// with AuditRestoreResult{Restored, BytesPulled, DurationSec}
//
// This file uses the H-7 actuals throughout.
package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func newAuditChainColdArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cold-archive",
		Short: "Manage Tessera cold archive (ls | restore)",
		Long: `Manage the cold-archive store for Tessera tile-log partitions.
Partitions are archived per YYYY_MM tag in the configured S3 bucket. Use
` + "`ls`" + ` to inventory archived partitions for a project; use ` + "`restore`" + ` to pull a
sealed partition back into the daemon's local hot tile cache.

` + "`restore`" + ` is DESTRUCTIVE — the local Tessera slice for the named partition is
overwritten. The command requires interactive confirmation (defaults to N,
spec §6.5 privacy-by-default).`,
	}
	cmd.AddCommand(newAuditChainColdArchiveLsCmd())
	cmd.AddCommand(newAuditChainColdArchiveRestoreCmd())
	return cmd
}

func newAuditChainColdArchiveLsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls",
		Short: "List archived Tessera partitions in S3 cold store",
		Long: `List all Tessera tile-log partitions available in the S3 cold archive
for the specified project. Output columns: PARTITION (YYYY_MM), BYTES,
ARCHIVED_AT (RFC3339), and CONTENT_HASH (for integrity spot-check).

Endpoint: GET /v1/audit-chain/cold-archive/list`,
		Example: `  hades audit-chain cold-archive ls --project hades-system
  hades audit-chain cold-archive ls --project hades-system --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).AuditColdArchiveList(ctx, project)
			if err != nil {
				return err
			}

			cols := []format.Column{
				{Header: "PARTITION", Field: func(r any) string {
					return r.(client.AuditColdArchiveEntry).PartitionID
				}},
				{Header: "BYTES", Field: func(r any) string {
					return strconv.FormatInt(r.(client.AuditColdArchiveEntry).SizeBytes, 10)
				}},
				{Header: "ARCHIVED_AT", Field: func(r any) string {
					return client.FormatUnix(r.(client.AuditColdArchiveEntry).ArchivedAt)
				}},
				{Header: "CONTENT_HASH", Field: func(r any) string {
					return r.(client.AuditColdArchiveEntry).ContentHash
				}},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	c.Flags().String("project", "", "Project ID (filters results to this project)")
	return c
}

func newAuditChainColdArchiveRestoreCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "restore",
		Short: "Restore a partition from cold archive into local hot tier (DESTRUCTIVE — requires confirmation)",
		Long: `Pull a sealed Tessera tile-log partition from S3 cold archive and
hydrate it into the daemon's local hot tile cache. DESTRUCTIVE: the local
Tessera slice for the named partition is overwritten.

Operator confirmation is required before the HTTP call (defaults to N;
spec §6.5 privacy-by-default). Confirm only after verifying the partition
tag and project ID are correct.

Both --partition (format YYYY_MM) and --project are required.`,
		Example: `  hades audit-chain cold-archive restore --partition 2026_05 --project hades-system`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			partition, _ := cmd.Flags().GetString("partition")
			project, _ := cmd.Flags().GetString("project")

			if strings.TrimSpace(partition) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--partition required (e.g. 2026_05)"))
			}

			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "  Partition: %s\n", partition)
			fmt.Fprintf(out, "  Project:   %s\n", project)
			fmt.Fprintf(out, "  WARNING: This OVERWRITES the local Tessera slice for the partition.\n")

			ok, err := promptYN(cmd.InOrStdin(), out, "Confirm destructive restore?")
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read confirmation: %w", err))
			}
			if !ok {
				fmt.Fprintln(out, "Restore aborted by operator.")
				return nil
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()

			res, err := newClientFromCmd(cmd).AuditColdArchiveRestore(ctx, partition, project)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "Restored partition %s: %d bytes in %ds (ok=%v)\n",
				partition, res.BytesPulled, res.DurationSec, res.Restored)
			return nil
		},
	}
	c.Flags().String("partition", "", "Partition tag YYYY_MM (required)")
	c.Flags().String("project", "", "Project ID (required)")
	_ = c.MarkFlagRequired("partition")
	_ = c.MarkFlagRequired("project")
	return c
}
