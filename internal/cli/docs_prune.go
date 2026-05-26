// SPDX-License-Identifier: MIT
// Package cli — docs_prune.go (Plan 14 Phase G Task G-5b).
//
// `zen docs prune --ecosystem <X> --version <Y>` hard-removes the
// (ecosystem, version) row and cascade-deletes its chunks, chunks_fp32,
// symbols, changes, and FTS5 entries from ecosystem.db. The deleted data
// is rebuildable via `zen docs reindex --ecosystem <X> --version <Y>`.
//
// Spec §0.2 + §2.9 Q9=A: "Never auto-prune". This command enforces a
// LOAD-BEARING SAFETY GATE: the command refuses to run unless the
// operator passes EXACTLY one of --dry-run (preview) or --confirm
// (execute). Both flags simultaneously → error (mutually exclusive).
// Neither flag → error explaining the gate.
//
// On --confirm, after rendering the preview, an additional promptYN
// confirmation gate is shown. Blank input aborts without side-effect.
//
// Pinned versions cannot be pruned: daemon returns 409 Conflict. The
// CLI surfaces the operator-guidance "unpin first". The retention-aware
// behaviour (refusing pinned versions) is daemon-enforced; this CLI
// surface relies on the daemon's contract per spec §2.9 Q9=A and
// classifyDocsError to map 409 → recoverable.
//
// G-5 SUPERSEDES F-6: F-6 shipped `zen docs prune --dry-run|--confirm`
// with a daemon-side DryRun flag in a single POST. G-5 splits preview
// (GET /v1/ecosystem/prune-preview) from commit
// (DELETE /v1/ecosystem/version), with a promptYN gate between them.
// History NewDocsPruneCmd previously took a factory + DryRun/Confirm
// bools on a single POST; now uses preview + prompt + DELETE.
//
// Boundary (inv-zen-031): does NOT import internal/research/ecosystem.
// Architecture CLI calls daemon HTTP; daemon owns the cascaded write.
//
// Exit codes (per spec §6.2):
//
//	0  success (dry-run preview OR confirmed delete OR operator-aborted prompt)
//	1  recoverable: neither --dry-run nor --confirm passed (safety gate),
//	   both passed (mutual exclusion), invalid --ecosystem, missing --version,
//	   daemon 404 (unknown tuple), daemon 409 (pinned version)
//	2  unrecoverable: transport, decode, daemon 5xx
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

const docsPruneTimeout = 60 * time.Second

// DocsPruneFlags carries `zen docs prune --ecosystem X --version Y` arguments.
//
// G-5 evolution from F-6: replaces the per-ecosystem-only DryRun/Confirm
// pair with (Ecosystem, Version, DryRun, Confirm). DryRun and Confirm are
// MUTUALLY GATED: exactly one MUST be true at call time (the safety gate).
type DocsPruneFlags struct {
	Ecosystem string
	Version   string
	DryRun    bool
	Confirm   bool
}

func NewDocsPruneCmd(factory DocsClientFactory) *cobra.Command {
	flags := DocsPruneFlags{}
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Hard-remove an ecosystem version (cascade-deletes chunks/symbols/changes)",
		Long: `Prune hard-removes a version from ecosystem.db and cascade-deletes:
  - All ecosystem_chunks rows for this version
  - All ecosystem_chunks_fp32 rows for those chunks
  - All ecosystem_symbols rows introduced in this version
  - All ecosystem_changes rows with version_from or version_to matching
  - All FTS5 index entries for the deleted chunks

The deleted data is rebuildable via:
  zen docs reindex --ecosystem <X> --version <Y>

Pinned versions cannot be pruned. The daemon returns 409 Conflict; unpin first:
  zen docs unpin --ecosystem <X> --version <Y>

SAFETY GATE: this command refuses to run without an explicit
--dry-run (preview only, no deletion) or --confirm (execute after prompt).
Both flags passed simultaneously → error (mutually exclusive). Neither
passed → error with usage hint. On --confirm, an additional y/N prompt is
shown after rendering the preview.

Required flags:
  --ecosystem   one of: go, python, typescript, rust
  --version     semver string (e.g. "1.18.0")
  --dry-run     preview row counts (mutually exclusive with --confirm)
  --confirm     execute the prune after promptYN gate`,
		Example: `  # Preview what would be deleted (no mutation)
  zen docs prune --ecosystem go --version 1.18.0 --dry-run

  # Execute the prune (renders preview, prompts y/N, commits on yes)
  zen docs prune --ecosystem python --version 3.9.0 --confirm`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsPruneTimeout)
			defer cancel()
			return RunDocsPrune(ctx, c, flags, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Ecosystem, "ecosystem", "", "ecosystem to prune (go|python|typescript|rust)")
	cmd.Flags().StringVar(&flags.Version, "version", "", "version to hard-remove (e.g. 1.21.0)")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "preview row counts without deleting")
	cmd.Flags().BoolVar(&flags.Confirm, "confirm", false, "perform hard delete (requires explicit flag + y/N prompt)")
	_ = cmd.MarkFlagRequired("ecosystem")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func RunDocsPrune(ctx context.Context, c DocsClient, flags DocsPruneFlags, in io.Reader, w io.Writer) error {
	eco := strings.TrimSpace(flags.Ecosystem)
	ver := strings.TrimSpace(flags.Version)

	if eco == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: --ecosystem is required"))
	}
	if !isValidEcosystemArg(eco) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: invalid --ecosystem %q; must be one of: %s",
			eco, strings.Join(validDocsEcosystems, ", ")))
	}
	if ver == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: --version is required and must be non-empty"))
	}

	if flags.DryRun && flags.Confirm {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: --dry-run and --confirm are mutually exclusive; pass exactly one"))
	}
	if !flags.DryRun && !flags.Confirm {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: pass --dry-run to preview or --confirm to execute; refusing silent prune"))
	}

	preview, err := c.EcosystemPrunePreview(ctx, eco, ver)
	if err != nil {
		return classifyDocsError(err, "prune")
	}

	if flags.DryRun {
		fmt.Fprintf(w, "Prune preview: %s@%s\n", eco, ver)
		renderPrunePreview(w, preview)
		if preview.Pinned {
			fmt.Fprintf(w, "\nNOTE: %s@%s is pinned (indefinite_retain=true). Cannot be pruned.\n", eco, ver)
			fmt.Fprintf(w, "To unpin: zen docs unpin --ecosystem %s --version %s\n", eco, ver)
			return nil
		}
		fmt.Fprintf(w, "\nTo execute: zen docs prune --ecosystem %s --version %s --confirm\n", eco, ver)
		return nil
	}

	if preview.Pinned {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("docs prune: %s@%s is pinned (indefinite_retain=true); run `zen docs unpin --ecosystem %s --version %s` first",
			eco, ver, eco, ver))
	}

	fmt.Fprintf(w, "About to HARD-REMOVE %s@%s from ecosystem.db:\n", eco, ver)
	renderPrunePreview(w, preview)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This action is NOT reversible without running:")
	fmt.Fprintf(w, "  zen docs reindex --ecosystem %s --version %s\n\n", eco, ver)

	ok, err := promptYN(in, w, "Confirm hard-delete?")
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("docs prune: prompt: %w", err))
	}
	if !ok {
		fmt.Fprintln(w, "Prune aborted by operator.")
		return nil
	}

	if err := c.EcosystemPrune(ctx, eco, ver); err != nil {
		return classifyDocsError(err, "prune")
	}
	fmt.Fprintf(w, "pruned: %s@%s (cascade-deleted chunks/symbols/changes/fts5)\n", eco, ver)
	fmt.Fprintf(w, "rebuild: zen docs reindex --ecosystem %s --version %s\n", eco, ver)
	return nil
}

func renderPrunePreview(w io.Writer, p *client.EcosystemPrunePreview) {
	var chunks, fp32, syms, chgs, fts int
	if p != nil {
		chunks = p.ChunkCount
		fp32 = p.ChunkFP32Count
		syms = p.SymbolCount
		chgs = p.ChangeCount
		fts = p.FTS5Count
	}
	fmt.Fprintf(w, "  chunks:       %d rows\n", chunks)
	fmt.Fprintf(w, "  chunks_fp32:  %d rows\n", fp32)
	fmt.Fprintf(w, "  symbols:      %d rows\n", syms)
	fmt.Fprintf(w, "  changes:      %d rows\n", chgs)
	fmt.Fprintf(w, "  fts5 entries: %d rows\n", fts)
}
