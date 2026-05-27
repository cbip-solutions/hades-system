// SPDX-License-Identifier: MIT
// Package cli — doctor_restore.go ships `zen doctor restore <ID>` Cobra
// subcommand.
//
// Operator UX:
//
// zen doctor restore 20260514T120000Z [--overwrite]
//
// Halts on conflict by default; --overwrite forces replacement.
//
// Exit codes (per spec §5.2 + cmd/zen/main.go):
// - 0 — restore succeeded
// - 1 — recoverable error (conflict halt; operator should re-run with --overwrite)
// - 2 — unrecoverable error (missing manifest, tarball corrupt, etc.)
//
// Conflict exit is mapped to 1 (recoverable) via cli.ErrRecoverable so
// the operator sees the conflict-resolution hint and can re-run.
package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctor/backup"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const DoctorRestoreAuditEventType = "evt.doctor.restore.applied"

func NewDoctorRestoreCmd() *cobra.Command {
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "restore <backup-id>",
		Short: "Restore a doctor backup by ID",
		Long: `Restore the contents of a backup created by ` + "`zen doctor full --fix`" + `.

The backup ID is the ISO8601 UTC timestamp printed alongside any
destructive fix output, e.g. "20260514T120000Z". Restoration extracts
the tarball back into the original source path; conflicts halt unless
--overwrite is supplied.

The restore command emits evt.doctor.restore.applied for forensic trace
(see ` + "`zen audit show <hash>`" + ` for the chain leaf).`,
		Example: `  zen doctor restore 20260514T120000Z
  zen doctor restore 20260514T120000Z --overwrite`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			b := backup.NewBackuper(backup.Config{})
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			manifest, err := b.LoadManifestByID(ctx, id)
			if err != nil {
				if errors.Is(err, backup.ErrNotFound) {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("restore: no backup with ID %s; check `zen state list` for available backups", id)))
				}
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("restore: load manifest %s: %w", id, err))
			}
			if err := b.RestoreFromManifest(ctx, manifest, backup.RestoreOptions{Overwrite: overwrite}); err != nil {
				if backup.IsConflictError(err) {
					fmt.Fprintln(cmd.OutOrStderr(), "Conflict: target files exist. Re-run with --overwrite to replace.")
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "restore: conflict; re-run with --overwrite"))
				}
				return err
			}

			emitter := newClientFromCmd(cmd)
			if emitter != nil {
				_, emitErr := emitter.AuditEmit(ctx, client.AuditEmitReq{
					Type: DoctorRestoreAuditEventType,
					Payload: map[string]any{
						"backupID":   id,
						"sourcePath": manifest.SourcePath,
						"overwrite":  overwrite,
						"appliedAt":  time.Now().UTC(),
					},
				})
				if emitErr != nil {

					fmt.Fprintf(cmd.OutOrStderr(), "warning: audit emit failed (daemon unreachable?); restore succeeded but no forensic trace: %v\n", emitErr)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Restored backup %s into %s\n", id, manifest.SourcePath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "force-replace existing files (default: halt on conflict)")
	return cmd
}
