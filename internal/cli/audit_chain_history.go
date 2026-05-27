// SPDX-License-Identifier: MIT
// Package cli — audit_chain_history.go.
//
// `zen audit-chain history` queries release audit_events_raw augmented with
// the release chain extension columns via GET /v1/audit-chain/history:
//
// PrevHash — previous record's hash (chain linkage)
// RecordHash — SHA-256 of this event's canonical bytes
// TesseraLeafID — namespaced leaf id in the Tessera tile-log (*string, nil if not yet tiled)
// PartitionID — monthly partition tag (e.g. "2026_05")
//
// Output table (default) or json/yaml (--format flag). Table adds three
// chain-proof columns beyond the release `zen audit events` surface:
//
// REC_HASH (truncated 16 chars) TESSERA_LEAF PARTITION
//
// Flags --project, --filter (type-prefix), --since, --limit. All are
// optional — no project filter returns events across all projects.
//
// Plan deviation: plan-file sketched AuditChainHistory / AuditChainHistoryEntry;
// H-7 shipped AuditHistory / AuditHistoryEntry. File uses H-7 actuals.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func newAuditChainHistoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "history",
		Short: "Query Plan 5 eventlog with chain proofs",
		Long: `Stream audit events from the Plan 5 event log enriched with chain
proofs: each row includes the Tessera Merkle path + witness signature cover
so the operator can verify event authenticity without re-running verify-chain.
Supports time-bound and event-type prefix filters.

Chain-proof columns added vs ` + "`zen audit events`" + `:
  REC_HASH     — first 16 chars of record_hash (full in --format json)
  TESSERA_LEAF — leaf index in the Tessera tile-log (nil = not yet tiled)
  PARTITION    — monthly partition tag (e.g. 2026_05)`,
		Example: `  zen audit-chain history --project zen-swarm
  zen audit-chain history --filter audit_review --since 24h --format json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			filterStr, _ := cmd.Flags().GetString("filter")
			sinceStr, _ := cmd.Flags().GetString("since")
			limit, _ := cmd.Flags().GetInt("limit")

			var sinceUnix int64
			if sinceStr != "" {
				ts, err := format.ParseSince(sinceStr)
				if err != nil {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--since: %w", err))
				}
				if !ts.IsZero() {
					sinceUnix = ts.Unix()
				}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).AuditHistory(ctx, client.AuditHistoryFilter{
				ProjectID: project,
				Filter:    filterStr,
				Since:     sinceUnix,
				Limit:     limit,
			})
			if err != nil {
				return err
			}

			cols := []format.Column{
				{Header: "ID", Field: func(r any) string {
					return shortID(r.(client.AuditHistoryEntry).ID)
				}},
				{Header: "PROJECT", Field: func(r any) string {
					return r.(client.AuditHistoryEntry).ProjectID
				}},
				{Header: "TYPE", Field: func(r any) string {
					return r.(client.AuditHistoryEntry).Type
				}},
				{Header: "EMITTED", Field: func(r any) string {
					return client.FormatUnix(r.(client.AuditHistoryEntry).EmittedAt)
				}},
				{Header: "REC_HASH", Field: func(r any) string {
					return truncateHash(r.(client.AuditHistoryEntry).RecordHash, 16)
				}},
				{Header: "TESSERA_LEAF", Field: func(r any) string {
					leafID := r.(client.AuditHistoryEntry).TesseraLeafID
					if leafID == nil {
						return "-"
					}
					return *leafID
				}},
				{Header: "PARTITION", Field: func(r any) string {
					p := r.(client.AuditHistoryEntry).PartitionID
					if p == "" {
						return "-"
					}
					return p
				}},
			}

			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	c.Flags().String("filter", "", "Event type prefix filter (e.g. audit_review)")
	c.Flags().String("since", "", "Time-bound filter (e.g. 24h, 2026-05-01T00:00:00Z)")
	c.Flags().String("project", "", "Project ID")
	return c
}

func truncateHash(h string, n int) string {
	if len(h) <= n {
		return h
	}
	return h[:n]
}
