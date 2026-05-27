// SPDX-License-Identifier: MIT
// Package cli — project.go.
//
// `hades project {doctor,archive,rm}` is the operator-facing project
// lifecycle surface. Each subcommand sends a JSON-over-UDS
// request to the daemon (the daemon is the only process that holds
// *store.Store; the CLI never touches storage directly — inv-hades-031).
//
// Cobra layout:
//
// hades project
// doctor [<alias>] [--rebind] # cwd-based or by alias
// archive <alias> # soft-delete
// rm <alias> --yes # hard-delete (cascades path_history)
// priority --boost|--reset|--ls # Layer 3 WFQ override
//
// Exit-code mapping (per spec §6.2):
//
// 0 — healthy / OK
// 1 — alias not found OR mv-detection pending OR --yes omitted
// OR daemon-side healthy:false (operator-recoverable)
// 2 — unrecoverable error (transport, JSON, daemon crash, store error)
//
// Categorisation is implemented via the ErrRecoverable sentinel: every
// recoverable error returned from this package wraps ErrRecoverable so
// cmd/hades/main.go can decide via cli.IsRecoverable(err) whether to exit
// 1 (recoverable) or 2 (everything else). See ErrRecoverable's docstring.
//
// reserves the `--rebind` flag on `doctor` so the command shape
// is final-form day 1; the rebind body lands in Operators
// who pass --rebind today get the same diagnostic output as without it.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

// ErrRecoverable is the sentinel root any operator-recoverable error in
// the CLI MUST wrap (via fmt.Errorf("%w:...", ErrRecoverable)). The
// process entry point (cmd/hades/main.go) maps these to exit code 1, all
// other non-nil errors to exit code 2 — per spec §6.2 :
//
// 0 — success / healthy
// 1 — operator-recoverable (mv-detected, alias-missing, --yes omitted)
// 2 — unrecoverable (transport, decode, daemon crash, store error)
//
// "Recoverable" means the operator can fix the situation by changing
// invocation (pass --yes, use the right alias) or running a follow-up
// command (rebind, init). It does NOT mean "the daemon will retry";
// the CLI does not retry.
//
// Why a sentinel rather than a typed error: the wrap-and-detect pattern
// composes cleanly with fmt.Errorf("%w:...") so callers can still
// attach a per-call message ("mv-detection pending", "alias not found")
// without losing the category. errors.Is unwraps the chain.
//
// Adoption today the only callers are runProjectDoctor / runProjectArchive
// / runProjectRm. New CLI commands joining the exit-code
// contract should follow the same pattern.
var ErrRecoverable = errors.New("operator-recoverable")

// ErrPreflightFailure is the sentinel root any preflight-gate failure
// (release `hades config init`: Hermes not installed, plugin
// format remnant detected) MUST wrap. The process entry point
// (cmd/hades/main.go) maps this category to exit code 3 — distinct from
// generic recoverable errors (exit 1) and unrecoverable errors (exit 2)
// per the EXIT CODES section in `hades config init --help` and spec §6.2.
//
// Why a third code: preflight failures are operator-recoverable but
// have a fundamentally different fix-loop than other recoverable errors.
// They require the operator to install missing infrastructure (Hermes)
// or remove legacy format remnants — actions that are environment-level
// rather than invocation-level. The dedicated exit code lets shell
// scripts and CI distinguish "fix your environment" from "fix your
// command line".
//
// Wrapping pattern: `fmt.Errorf("%w: %v", ErrPreflightFailure, hermesErr)`.
// errors.Is unwraps the chain so IsPreflightFailure works regardless of
// how deeply the error has been wrapped.
var ErrPreflightFailure = errors.New("preflight-failure")

func IsRecoverable(err error) bool {
	return err != nil && errors.Is(err, ErrRecoverable)
}

func IsPreflightFailure(err error) bool {
	return err != nil && errors.Is(err, ErrPreflightFailure)
}

func recoverable(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRecoverable}, args...)...)
}

// recoverableWrap wraps an existing err as recoverable, preserving the
// original error in the chain. Used for HTTP 404 → exit 1 mapping where
// we want both the recoverable category AND the wrapped HTTPError so
// callers downstream can still introspect via errors.As. Caller MUST
// pass a non-nil err (we never wrap nil; that would be a bug at the
// call site).
func recoverableWrap(err error, msg string) error {
	return fmt.Errorf("%w: %s: %v", ErrRecoverable, msg, err)
}

func NewProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project identity lifecycle (doctor / archive / rm)",
		Long: `Manage per-project identity in the daemon's project registry.
All actions resolve aliases via projectctxadapter (sha256 canonical
identity + alias UX) so cwd-rooted invocations agree with explicit
alias invocations.

Subcommands:
  doctor    diagnose project identity (cwd-based or by alias)
  archive   soft-delete an alias (excluded from default ls)
  rm        hard-delete an alias (cascades path_history; --yes required)
  priority  Layer-3 WFQ override (boost / reset / list; spec §1 Q10)`,
		Example: `  # Diagnose the project anchored at the current cwd
  hades project doctor

  # Archive the "internal-platform-x" alias (reversible)
  hades project archive internal-platform-x

  # Permanently remove an alias and its path_history rows
  hades project rm old-prototype --yes

  # Boost the "internal-platform-x" alias for 4 hours
  hades project priority --boost internal-platform-x --duration 4h --reason "release prep"`,
	}
	cmd.AddCommand(projectDoctorCmd())
	cmd.AddCommand(projectArchiveCmd())
	cmd.AddCommand(projectRmCmd())

	cmd.AddCommand(NewPriorityCmd())
	return cmd
}

func projectDoctorCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor [<alias>]",
		Short: "Diagnose project identity (cwd-based or by alias)",
		Long: `Run a project-identity health probe against the daemon's project
registry. With no argument the CLI captures cwd; the daemon walks up
to find hadessystem.toml or .git via projectctx.FindProjectRoot. With an
alias argument the daemon resolves directly without filesystem walking.

Output reports:
  * resolved alias + sha256 (truncated to 8 chars for table use)
  * canonical path the registry agrees on
  * full path_history (every path the project has lived at, with
    first-seen and last-seen timestamps)
  * MV-DETECTED block when the daemon notices the cwd's sha256 does
    not match any registered project (operator moved the directory
    or the alias-on-disk drifted)

Exit codes (spec §6.2):
  0  healthy
  1  mv-detection pending OR alias not found OR daemon healthy:false
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: `  # Probe the project anchored at cwd
  hades project doctor

  # Probe a specific project by alias
  hades project doctor internal-platform-x

  # Phase B/J: rebind the alias to cwd's sha256 after a directory move
  hades project doctor --rebind`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := ""
			if len(args) == 1 {
				alias = args[0]
			}
			rebind, _ := cmd.Flags().GetBool("rebind")
			cli := newClientFromCmd(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			return runProjectDoctor(ctx, cli, alias, rebind, cmd.OutOrStdout())
		},
	}
	c.Flags().Bool("rebind", false, "(Phase B/J) rebind alias to current cwd's sha256 after mv-detection")
	return c
}

func projectArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <alias>",
		Short: "Archive a project alias (excluded from default ls)",
		Long: `Soft-delete a project alias by setting autonomous_state="complete"
in the daemon's project registry. The alias is preserved (so historical
references in audit / inbox / knowledge tables remain navigable) but
hidden from the default surface — ` + "`hades projects ls`" + ` shows archived
rows under a STATE=archived marker so operator can spot dead aliases.

Reversible: a future Plan 7 Phase J restore-flow will resurrect archived
aliases. In Plan 7 v0.7.0 the only un-archive path is the daemon's
direct registry edit; CLI exposes archive (forward), rm (forward),
restore comes in Phase J.

Exit codes (spec §6.2):
  0  archived
  1  alias not found (operator typo or already archived/removed)
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: `  # Archive a deprecated project
  hades project archive old-prototype

  # Inspect after archiving (archived rows show with STATE=archived)
  hades projects ls`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli := newClientFromCmd(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			return runProjectArchive(ctx, cli, args[0], cmd.OutOrStdout())
		},
	}
}

func projectRmCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rm <alias> [--yes]",
		Short: "Remove a project alias (cascade-deletes path_history; --yes required)",
		Long: `Hard-delete a project alias from the daemon's project registry.
The DELETE cascades to path_history rows (every path the project ever
lived at) but does NOT touch audit / inbox / knowledge tables — those
hold their own soft-references and remain navigable post-removal.

The --yes flag is mandatory because the operation is irreversible
(unlike archive). Without --yes the CLI prints a "refused" line + exits
1; with --yes the daemon DELETEs and the row is gone.

The CLI enforces --yes locally because the daemon's UDS surface is
trust-on-first-use within the host: any process able to open
/tmp/hades-system.sock can call /v1/projects/rm. The CLI's destructive-
confirmation discipline is the operator-facing safety net.

Exit codes (spec §6.2):
  0  removed
  1  --yes omitted OR alias not found
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: `  # Refused (no --yes; safe default)
  hades project rm old-prototype

  # Confirmed removal (cascades path_history)
  hades project rm old-prototype --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			cli := newClientFromCmd(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			return runProjectRm(ctx, cli, args[0], yes, cmd.OutOrStdout())
		},
	}
	c.Flags().Bool("yes", false, "Confirm destructive removal (required)")
	return c
}

func runProjectDoctor(ctx context.Context, c *client.Client, alias string, rebind bool, w io.Writer) error {
	cwd := ""
	if alias == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {

			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("project doctor: getwd: %w", err))
		}
	}
	resp, err := c.ProjectDoctor(ctx, alias, cwd, rebind)
	if err != nil {
		fmt.Fprintf(w, "project doctor: %s\n  status: ERROR\n  %v\n", aliasOrCwd(alias, cwd), err)

		if client.IsHTTPStatus(err, 404) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		return err
	}
	fmt.Fprintf(w, "project doctor: %s\n", resp.Alias)
	if resp.MvDetected != nil {
		fmt.Fprintf(w, "  status:    MV DETECTED\n")
		fmt.Fprintf(w, "  alias:     %s\n", resp.Alias)
		fmt.Fprintf(w, "  old path:  %s\n", resp.MvDetected.OldPath)
		fmt.Fprintf(w, "  new path:  %s\n", resp.MvDetected.NewPath)
		fmt.Fprintf(w, "  old id:    %s\n", resp.MvDetected.OldIDShort)
		fmt.Fprintf(w, "  new id:    %s\n", resp.MvDetected.NewIDShort)

		if resp.Hint != "" {
			lines := strings.Split(resp.Hint, "\n")
			for i, line := range lines {
				if i == 0 {
					fmt.Fprintf(w, "  hint:      %s\n", line)
				} else {
					fmt.Fprintf(w, "             %s\n", line)
				}
			}
		}
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("mv-detection pending"))
	}
	fmt.Fprintf(w, "  alias:        %s\n", resp.Alias)
	idShort := resp.IDSha256
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}
	fmt.Fprintf(w, "  id (sha256):  %s\n", idShort)
	fmt.Fprintf(w, "  canonical:    %s\n", resp.CanonicalPath)
	fmt.Fprintf(w, "  paths seen:   %d entries\n", len(resp.PathHistory))
	for _, e := range resp.PathHistory {
		first := time.Unix(e.FirstSeen, 0).UTC().Format("2006-01-02 15:04")
		last := time.Unix(e.LastSeen, 0).UTC().Format("2006-01-02 15:04")
		fmt.Fprintf(w, "                  %s (%s -> %s)\n", e.Path, first, last)
	}
	if resp.Healthy {
		fmt.Fprintf(w, "  status:       healthy\n")
		return nil
	}
	fmt.Fprintf(w, "  status:       UNHEALTHY\n")
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), recoverable("project unhealthy"))
}

func aliasOrCwd(alias, cwd string) string {
	if alias != "" {
		return alias
	}
	if cwd != "" {
		return "(cwd: " + cwd + ")"
	}
	return "(no alias)"
}

func runProjectArchive(ctx context.Context, c *client.Client, alias string, w io.Writer) error {
	if err := c.ProjectArchive(ctx, alias); err != nil {
		fmt.Fprintf(w, "archive failed: %v\n", err)
		if client.IsHTTPStatus(err, 404) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		return err
	}
	fmt.Fprintf(w, "archived: %s\n", alias)
	return nil
}

func runProjectRm(ctx context.Context, c *client.Client, alias string, yes bool, w io.Writer) error {
	if !yes {
		fmt.Fprintf(w, "refused: pass --yes to confirm deletion of alias %q\n", alias)
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--yes required"))
	}
	if err := c.ProjectRemove(ctx, alias); err != nil {
		fmt.Fprintf(w, "remove failed: %v\n", err)
		if client.IsHTTPStatus(err, 404) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		return err
	}
	fmt.Fprintf(w, "removed: %s\n", alias)
	return nil
}
