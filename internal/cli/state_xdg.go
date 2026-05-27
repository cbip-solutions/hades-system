// SPDX-License-Identifier: MIT
// Package cli — state_xdg.go.
//
// `zen state list` + `zen state cleanup` are the XDG state-retention
// surface per Q12=D + invariant. The manifest leaves
// (show/regenerate/verify/pin/history) live in state.go + sibling
// files; this file isolates the XDG-state retention leaves.
//
// XDG state paths resolved per spec §2.12:
//
// $XDG_STATE_HOME/zen-swarm (default ~/.local/state/zen-swarm)
// $XDG_CACHE_HOME/zen-swarm (default ~/.cache/zen-swarm)
//
// Retention defaults: doctor-backups 30d / migrate-backups 30d /
// spike-artifacts indefinite / cache 7d. release doctrine TOML override
// at [state.backup_retention] tunes per-subsystem (release+ wiring
// loads the override from the active doctrine bundle).
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/state/cleanup"
)

func newStateListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Enumerate XDG state dirs with sizes + ages + paths (Plan 13 F7)",
		Long: `List entries under $XDG_STATE_HOME/zen-swarm + $XDG_CACHE_HOME/zen-swarm.

Subsystems enumerated:
  doctor-backups   ($XDG_STATE_HOME/zen-swarm/doctor-backups/)
  migrate-backups  ($XDG_STATE_HOME/zen-swarm/migrate-backups/)
  spike-artifacts  ($XDG_STATE_HOME/zen-swarm/spike-artifacts/)
  cache            ($XDG_CACHE_HOME/zen-swarm/)

Use --json for schemaVersion=1.0 machine-parseable output.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stateDir, cacheDir := resolveXDGPaths()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			entries, err := cleanup.Enumerate(ctx, stateDir, cacheDir)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("state list: %w", err))
			}
			if jsonOutput {
				return cleanup.RenderJSON(ctx, cmd.OutOrStdout(), entries)
			}
			renderHumanList(cmd.OutOrStdout(), entries)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output (schemaVersion=1.0)")
	return cmd
}

func newStateCleanupCmd() *cobra.Command {
	var dryRun bool
	var keepIDs []string
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Apply retention policy to XDG state dirs (Q12=D + inv-zen-187)",
		Long: `Apply the retention policy to XDG state dirs (doctor-backups,
migrate-backups, spike-artifacts, cache).

Defaults (per spec §2.12):
  doctor-backups   30d
  migrate-backups  30d
  spike-artifacts  indefinite
  cache            7d LRU

Plan 8 doctrine TOML [state.backup_retention] tunes per-subsystem (Plan
14+ wiring will load the override automatically; the current command
applies the default policy unless --override-* flags are added).

Use --dry-run to preview without deleting. Use --keep=ID (repeatable)
to except specific backup IDs from expiration.

Audit chain emits evt.state.cleanup.deleted per expired entry (best-effort;
daemon-down logs warning + continues — the cleanup itself is the
authoritative effect; the audit emit is for forensic trace).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stateDir, cacheDir := resolveXDGPaths()
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			emitter := NewDaemonAuditEmitter(newClientFromCmd(cmd), nil)
			expired, err := cleanup.Apply(ctx, cleanup.Options{
				StateDir: stateDir,
				CacheDir: cacheDir,
				Policy:   cleanup.DefaultPolicy(),
				KeepIDs:  keepIDs,
				DryRun:   dryRun,
				Emitter:  emitter,
			})
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("state cleanup: %w", err))
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Would expire %d state entries (--dry-run; nothing deleted)\n", expired)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Expired %d state entries\n", expired)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview expiration without deleting")
	cmd.Flags().StringSliceVar(&keepIDs, "keep", nil, "preserve specific backup IDs even past retention (repeatable)")
	return cmd
}

func resolveXDGPaths() (stateDir, cacheDir string) {
	xdgState := os.Getenv("XDG_STATE_HOME")
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgState == "" || xdgCache == "" {
		home, _ := os.UserHomeDir()
		if xdgState == "" {
			xdgState = filepath.Join(home, ".local", "state")
		}
		if xdgCache == "" {
			xdgCache = filepath.Join(home, ".cache")
		}
	}
	return filepath.Join(xdgState, "zen-swarm"), filepath.Join(xdgCache, "zen-swarm")
}

func renderHumanList(w io.Writer, entries []cleanup.StateEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "(no state entries)")
		return
	}
	const widthSubsystem = 18
	const widthID = 22
	const widthSize = 12
	const widthAge = 14
	fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  PATH\n",
		widthSubsystem, "SUBSYSTEM", widthID, "ID", widthSize, "SIZE", widthAge, "AGE")
	for _, e := range entries {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  %s\n",
			widthSubsystem, e.Subsystem,
			widthID, e.ID,
			widthSize, formatBytes(e.Size),
			widthAge, formatDuration(e.Age),
			e.Path,
		)
	}
}

func formatBytes(n int64) string {
	const KB = 1024
	const MB = 1024 * KB
	const GB = 1024 * MB
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatDuration(d time.Duration) string {
	switch {
	case d > 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d > time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d > time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
