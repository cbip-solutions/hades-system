// SPDX-License-Identifier: MIT
// Package cli — sshexec.go.
//
// `zen ssh-exec` exposes the security-grade ssh-exec MCP. Validation
// and allowlist queries are handled locally via internal/mcp/sshexec
// (no daemon round-trip needed); audit-log reads route through the
// daemon's /v1/audit/events endpoint filtered to type=sshexec prefix.
//
// Cobra layout (6 leaves):
//
// zen ssh-exec validate --cmd --project [--toml]
// zen ssh-exec allowlist show --project [--toml]
// zen ssh-exec allowlist add --project --pattern --yes
// zen ssh-exec allowlist remove --project --pattern --yes
// zen ssh-exec audit-log --project --since --limit
// zen ssh-exec exec --host --cmd --cwd --timeout --project --toml
//
// Option A adaptation: invokes internal/mcp/sshexec.Validate /
// ResolveAllowlist directly. exec uses sshexec.Run with explicit auth
// (operator's local SSH agent). The plan-doc described an SSE streaming
// /v1/sshexec/exec daemon route that proxies the MCP child via
// Streamable-HTTP framing; that wiring is deferred to a future plan
// . delivers a
// complete operator surface using the same package the MCP uses.
package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
	"github.com/spf13/cobra"
)

func NewSSHExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh-exec",
		Short: "Security-grade SSH command execution (Plan 4)",
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(sshExecValidateCmd())
	cmd.AddCommand(sshExecAllowlistCmd())
	cmd.AddCommand(sshExecAuditLogCmd())
	cmd.AddCommand(sshExecExecCmd())
	return cmd
}

func loadAllowlistForCmd(cmd *cobra.Command) (*sshexec.Allowlist, error) {
	project, _ := cmd.Flags().GetString("project")
	tomlPath, _ := cmd.Flags().GetString("toml")
	if project == "" {
		return nil, ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project required"))
	}
	if tomlPath == "" {
		tomlPath = "zenswarm.toml"
	}
	// Review F-8: the source-of-truth comment in
	// internal/mcp/sshexec/allowlist.go::ResolveAllowlist says
	// "projectTOMLPath may be empty: doctrine alone is the source".
	// Make the CLI honour that contract by falling through to "" when
	// the default./zenswarm.toml is absent. Operators can still pass
	// an explicit --toml=/path/that/must/exist and that case is treated
	// as required (we do NOT silently fall through if the operator
	// asked for a specific file).
	if _, statErr := os.Stat(tomlPath); statErr != nil {
		if os.IsNotExist(statErr) {
			explicit := false
			if f := cmd.Flag("toml"); f != nil && f.Changed {
				explicit = true
			}
			if !explicit {
				tomlPath = ""
			} else {
				return nil, fmt.Errorf("--toml=%q: %w", tomlPath, statErr)
			}
		}
	}
	doc := resolveActiveDoctrineForCmd(cmd)
	allow, err := sshexec.ResolveAllowlist(&doc, tomlPath, project)
	if err != nil {
		return nil, err
	}
	return allow, nil
}

// resolveActiveDoctrineForCmd queries the daemon /v1/doctrine/state to
// determine the active doctrine schema (review F-6). Falls back to
// doctrine.DefaultBuiltin() when the daemon is unreachable or returns
// an unrecognised doctrine name. Operators on max-scope or capa-
// firewall MUST see their doctrine's ssh-exec ceiling — defaulting to
// the default-builtin's narrower ceiling effectively denies legitimate
// commands (and on capa-firewall, the OPPOSITE: would over-grant).
func resolveActiveDoctrineForCmd(cmd *cobra.Command) doctrine.Schema {
	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()
	state, err := newClientFromCmd(cmd).DoctrineStateCall(ctx)
	if err == nil {
		name := lookupDoctrineName(state)
		if name != "" {
			if s, berr := doctrine.Builtin(name); berr == nil {
				return s
			}
		}
	}
	return doctrine.DefaultBuiltin()
}

func lookupDoctrineName(state client.DoctrineState) string {
	if v, ok := state["name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := state["Name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func sshExecValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Dry-run allowlist + forbidden-chars check on a command",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmdStr, _ := cmd.Flags().GetString("cmd")
			if cmdStr == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--cmd required"))
			}
			allow, err := loadAllowlistForCmd(cmd)
			if err != nil {
				return err
			}
			result := sshexec.Validate(cmdStr, allow.Patterns)
			out := cmd.OutOrStdout()
			if result.OK {
				fmt.Fprintf(out, "ok: matched pattern %q\n", result.Pattern)
				return nil
			}
			fmt.Fprintf(out, "deny: %s\n", result.Reason)
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("validation rejected"))
		},
	}
	cmd.Flags().String("cmd", "", "Command to validate")
	cmd.Flags().String("project", "", "Project ID (required)")
	cmd.Flags().String("toml", "", "Path to zenswarm.toml (default: ./zenswarm.toml)")
	return cmd
}

func sshExecAllowlistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowlist",
		Short: "ssh-exec allowlist (show | add | remove)",
	}
	cmd.AddCommand(sshExecAllowlistShowCmd())
	cmd.AddCommand(sshExecAllowlistAddCmd())
	cmd.AddCommand(sshExecAllowlistRemoveCmd())
	return cmd
}

func sshExecAllowlistShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show effective allowlist for a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			allow, err := loadAllowlistForCmd(cmd)
			if err != nil {
				return err
			}
			opts := format.OptionsFromFlags(cmd)
			type entry struct {
				Kind  string `json:"kind" yaml:"kind"`
				Value string `json:"value" yaml:"value"`
			}
			rows := []entry{}
			for _, p := range allow.Patterns {
				rows = append(rows, entry{Kind: "pattern", Value: p})
			}
			for _, h := range allow.Hosts {
				rows = append(rows, entry{Kind: "host", Value: h})
			}
			cols := []format.Column{
				{Header: "KIND", Field: func(r any) string { return r.(entry).Kind }},
				{Header: "VALUE", Field: func(r any) string { return r.(entry).Value }},
			}
			out := cmd.OutOrStdout()
			if opts.Format == "table" {
				fmt.Fprintf(out, "Project: %s (source: %s)\n\n", allow.Project, allow.Source)
			}
			return format.Render(out, opts, rows, cols)
		},
	}
	cmd.Flags().String("project", "", "Project ID (required)")
	cmd.Flags().String("toml", "", "Path to zenswarm.toml (default: ./zenswarm.toml)")
	return cmd
}

func sshExecAllowlistAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a pattern to per-project allowlist (--yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, _ := cmd.Flags().GetString("project")
			pattern, _ := cmd.Flags().GetString("pattern")
			tomlPath, _ := cmd.Flags().GetString("toml")
			yes, _ := cmd.Flags().GetBool("yes")
			if project == "" || pattern == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project and --pattern required"))
			}
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm allowlist mutation"))
			}
			if tomlPath == "" {
				tomlPath = "zenswarm.toml"
			}

			if _, err := validateZenswarmTOMLPath(tomlPath); err != nil {
				return err
			}
			outcome, err := mutateAllowlistTOML(tomlPath, project, pattern, true)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			switch outcome {
			case OutcomeAdded:
				fmt.Fprintf(out, "added: %q [project=%s] in %s\n", pattern, project, tomlPath)
			case OutcomeAlreadyPresent:
				fmt.Fprintf(out, "already-present: %q [project=%s] in %s\n", pattern, project, tomlPath)
			default:

				fmt.Fprintf(out, "outcome=%v: %q [project=%s] in %s\n", outcome, pattern, project, tomlPath)
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "Project ID")
	cmd.Flags().String("pattern", "", "Allowlist pattern (e.g. 'alembic *')")
	cmd.Flags().String("toml", "", "Path to zenswarm.toml (default: ./zenswarm.toml)")
	cmd.Flags().Bool("yes", false, "Confirm mutation")
	return cmd
}

func sshExecAllowlistRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a pattern from per-project allowlist (--yes required)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, _ := cmd.Flags().GetString("project")
			pattern, _ := cmd.Flags().GetString("pattern")
			tomlPath, _ := cmd.Flags().GetString("toml")
			yes, _ := cmd.Flags().GetBool("yes")
			if project == "" || pattern == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project and --pattern required"))
			}
			if !yes {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm allowlist mutation"))
			}
			if tomlPath == "" {
				tomlPath = "zenswarm.toml"
			}

			if _, err := validateZenswarmTOMLPath(tomlPath); err != nil {
				return err
			}
			outcome, err := mutateAllowlistTOML(tomlPath, project, pattern, false)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch outcome {
			case OutcomeRemoved:
				fmt.Fprintf(out, "removed: %q [project=%s] in %s\n", pattern, project, tomlPath)
			case OutcomeNotPresent:
				fmt.Fprintf(out, "not present: %q [project=%s] in %s\n", pattern, project, tomlPath)
			default:
				fmt.Fprintf(out, "outcome=%v: %q [project=%s] in %s\n", outcome, pattern, project, tomlPath)
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "Project ID")
	cmd.Flags().String("pattern", "", "Allowlist pattern to remove")
	cmd.Flags().String("toml", "", "Path to zenswarm.toml (default: ./zenswarm.toml)")
	cmd.Flags().Bool("yes", false, "Confirm mutation")
	return cmd
}

type AllowlistMutationOutcome int

const (
	OutcomeUnknown AllowlistMutationOutcome = iota

	OutcomeAdded

	OutcomeAlreadyPresent

	OutcomeRemoved

	OutcomeNotPresent
)

// mutateAllowlistTOML adds or removes a pattern from the project's
// zenswarm.toml [ssh_exec.allowlist].patterns. Atomic: writes to a
// .tmp file then renames. Idempotent: removing a non-existent pattern
// or adding a duplicate is a no-op success that returns the
// corresponding OutcomeAlreadyPresent / OutcomeNotPresent value.
//
// SECURITY (review C-1, defense-in-depth): rejects any path outside cwd
// BEFORE any read/write. The cobra layer (sshExecAllowlistAddCmd /
// sshExecAllowlistRemoveCmd) calls validateZenswarmTOMLPath as layer-1;
// this is layer-2 so direct invocations (tests, future callers) cannot
// bypass the check.
func mutateAllowlistTOML(path, project, pattern string, add bool) (AllowlistMutationOutcome, error) {

	if _, err := validateZenswarmTOMLPath(path); err != nil {
		return OutcomeUnknown, err
	}
	type tomlShape struct {
		Project struct {
			ID string `toml:"id"`
		} `toml:"project"`
		SSHExec struct {
			Allowlist struct {
				Patterns []string `toml:"patterns"`
				Hosts    []string `toml:"hosts"`
			} `toml:"allowlist"`
		} `toml:"ssh_exec"`
	}
	var doc tomlShape
	if data, err := os.ReadFile(path); err == nil {
		if _, derr := toml.Decode(string(data), &doc); derr != nil {
			return OutcomeUnknown, fmt.Errorf("parse %s: %w", path, derr)
		}
	} else if !os.IsNotExist(err) {
		return OutcomeUnknown, fmt.Errorf("read %s: %w", path, err)
	}
	if doc.Project.ID == "" {
		doc.Project.ID = project
	} else if doc.Project.ID != project {
		return OutcomeUnknown, fmt.Errorf("project mismatch: file has %q, --project=%q", doc.Project.ID, project)
	}
	patterns := doc.SSHExec.Allowlist.Patterns
	exists := false
	out := patterns[:0]
	for _, p := range patterns {
		if p == pattern {
			exists = true
			if !add {
				continue
			}
		}
		out = append(out, p)
	}
	var outcome AllowlistMutationOutcome
	switch {
	case add && exists:
		outcome = OutcomeAlreadyPresent
	case add && !exists:
		out = append(out, pattern)
		outcome = OutcomeAdded
	case !add && exists:
		outcome = OutcomeRemoved
	default:
		outcome = OutcomeNotPresent
	}
	doc.SSHExec.Allowlist.Patterns = out
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return OutcomeUnknown, err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(doc); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return OutcomeUnknown, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return OutcomeUnknown, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return OutcomeUnknown, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return OutcomeUnknown, err
	}
	return outcome, nil
}

func sshExecAuditLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-log",
		Short: "Recent ssh-exec audit events (filtered from audit_events_raw)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			limit, _ := cmd.Flags().GetInt("limit")
			sinceStr, _ := cmd.Flags().GetString("since")
			var sinceUnix int64
			if sinceStr != "" {
				t, err := format.ParseSince(sinceStr)
				if err != nil {
					return err
				}
				if !t.IsZero() {
					sinceUnix = t.Unix()
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			// invariant (review F-5): MUST filter on the canonical
			// `ssh_exec.` prefix (underscore) — this matches what the
			// MCP Emitter at internal/mcp/sshexec/emit.go::EmitStarted/
			// Completed/Denied/InteractiveBlocked actually writes
			// (`ssh_exec.started`, etc.) AND what the dispatcher
			// emitter in this file produces. Pre-fix the CLI used
			// "sshexec" (no underscore) which matched zero rows
			// because no emitter ever uses that prefix.
			events, err := newClientFromCmd(cmd).AuditEvents(ctx, "ssh_exec", project, sinceUnix, limit)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "ID", Field: func(r any) string { return shortID(r.(client.AuditEvent).ID) }},
				{Header: "PROJECT", Field: func(r any) string { return r.(client.AuditEvent).ProjectID }},
				{Header: "TYPE", Field: func(r any) string { return r.(client.AuditEvent).Type }},
				{Header: "EMITTED", Field: func(r any) string { return client.FormatUnix(r.(client.AuditEvent).EmittedAt) }},
				{Header: "PAYLOAD", Field: func(r any) string { return truncatePayload(r.(client.AuditEvent).PayloadRaw, 50) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, events, cols)
		},
	}
	cmd.Flags().String("project", "", "Filter by project (optional)")
	return cmd
}

func sshExecExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Execute a command on a remote host (allowlist-checked)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, _ := cmd.Flags().GetString("host")
			cmdStr, _ := cmd.Flags().GetString("cmd")
			cwd, _ := cmd.Flags().GetString("cwd")
			timeoutStr, _ := cmd.Flags().GetString("timeout")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			yes, _ := cmd.Flags().GetBool("yes")
			if host == "" || cmdStr == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--host and --cmd required"))
			}
			if !yes && !dryRun {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--yes required to confirm SSH execution (or --dry-run)"))
			}
			allow, err := loadAllowlistForCmd(cmd)
			if err != nil {
				return err
			}

			vr := sshexec.Validate(cmdStr, allow.Patterns)
			if !vr.OK {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("validation rejected: %s", vr.Reason))
			}

			hostOK := false
			for _, h := range allow.Hosts {
				if h == host {
					hostOK = true
					break
				}
			}
			if !hostOK {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("host %q not in allowlist", host))
			}
			req := sshexec.ExecRequest{
				Host: host, Command: cmdStr, Cwd: cwd, Project: allow.Project,
			}
			if timeoutStr != "" {
				d, err := format.ParseDuration(timeoutStr)
				if err != nil {
					return err
				}
				req.Timeout = d
			}
			req.ApplyDefaultsFrom(&allow.Defaults)
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "DRY-RUN: would exec %q on %s (cwd=%q timeout=%s project=%s)\n",
					cmdStr, host, cwd, req.Timeout, allow.Project)
				return nil
			}

			auth, err := sshexec.AgentAuth()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("wizard.mcp-spawn-fail"), fmt.Errorf("ssh agent: %w", err))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), req.Timeout+30*time.Second)
			defer cancel()
			sink := &cliStreamSink{out: cmd.OutOrStdout(), errw: cmd.ErrOrStderr()}
			// invariant (review F-1): wire a real audit emitter that
			// POSTs ssh_exec.{started,completed,denied,interactive_blocked}
			// events to the daemon /v1/audit/emit endpoint. Pre-fix the
			// CLI passed &sshexec.NopAuditEmitter{} which silently
			// discarded every event, so a security-grade `zen ssh-exec
			// exec --yes` produced ZERO audit rows — operators couldn't
			// reconstruct the session afterwards. The dispatcher uses a
			// detached context (Background) so emission survives past
			// the parent ctx cancellation (mirrors the Emitter
			// design in internal/mcp/sshexec/emit.go::emit).
			emitter := newDispatcherAuditEmitter(newClientFromCmd(cmd), allow.Project)
			result, err := sshexec.Run(ctx, req, vr, allow, auth, sink, emitter)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "\nexit_code=%d duration=%s reason=%s\n",
				result.ExitCode, result.Duration, result.ExitReason)
			if result.ExitCode != 0 {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("remote exit %d (%s)", result.ExitCode, result.ExitReason))
			}
			return nil
		},
	}
	cmd.Flags().String("host", "", "Remote host[:port]")
	cmd.Flags().String("cmd", "", "Command to execute (allowlist-checked)")
	cmd.Flags().String("cwd", "", "Remote cwd (forwarded as ZEN_CWD)")
	cmd.Flags().String("timeout", "", "Wall-clock timeout (default: doctrine 60s)")
	cmd.Flags().String("project", "", "Project ID (required)")
	cmd.Flags().String("toml", "", "Path to zenswarm.toml (default: ./zenswarm.toml)")
	cmd.Flags().Bool("dry-run", false, "Validate only; do not connect")
	cmd.Flags().Bool("yes", false, "Confirm execution")
	return cmd
}

type cliStreamSink struct {
	out, errw interface{ Write([]byte) (int, error) }
}

func (s *cliStreamSink) Emit(chunk sshexec.StreamChunk) error {
	if chunk.Stream == sshexec.StreamStdout {
		_, err := s.out.Write(chunk.Data)
		return err
	}
	if chunk.Stream == sshexec.StreamStderr {
		_, err := s.errw.Write(chunk.Data)
		return err
	}
	return nil
}

// dispatcherAuditEmitter implements sshexec.AuditEmitter by POSTing
// each event to the daemon's /v1/audit/emit endpoint via *client.Client.
//
// invariant (review F-1): every Run call MUST emit
// ssh_exec.{started,completed,denied,interactive_blocked} so operators
// can reconstruct security-grade SSH sessions after the fact. The
// pre-fix CLI used &sshexec.NopAuditEmitter{} which silently dropped
// every event — a regression even when the daemon is unreachable
// because there was no fallback path either.
//
// Design choices:
//
// - Fire-and-forget audit emission with a fresh 5s deadline derived
// from context.Background(). Parent ctx cancellation does NOT
// affect audit emission — this is INTENTIONAL: audit must outlive
// operator-cancelled commands so the security trail is always
// produced. Mirrors the Emitter design in
// internal/mcp/sshexec/emit.go::emit.
// - Errors are returned to sshexec.Run which treats them as
// best-effort and never fails the parent command. A daemon-down
// state should not block a successful command. The daemon's
// EmitClient at internal/mcp/client/emit.go provides invariant
// no-loss buffering for the MCP path; the CLI path is used by
// direct operators and a daemon-down state surfaces via
// `zen doctor sshexec` rather than command failure.
// - Payload schema matches internal/mcp/sshexec/emit.go: same keys
// so audit-log queries don't need to discriminate by source.
//
// Review C-2: pre-fix the struct stored a `parentCtx` field
// that was never consulted by emit() (which always derives from
// context.Background()). The dead field + misleading docstring are
// removed; the constructor signature drops the redundant ctx parameter.
type dispatcherAuditEmitter struct {
	c         *client.Client
	projectID string
}

// newDispatcherAuditEmitter constructs the CLI-side audit dispatcher.
//
// Each emit call uses a fresh 5s deadline derived from
// context.Background() (NOT the caller's ctx) so audit emission survives
// parent ctx cancellation — this is intentional per invariant: the
// security trail must always be produced even if the operator cancels
// the surrounding command.
func newDispatcherAuditEmitter(c *client.Client, projectID string) *dispatcherAuditEmitter {
	return &dispatcherAuditEmitter{c: c, projectID: projectID}
}

func (e *dispatcherAuditEmitter) EmitStarted(req sshexec.ExecRequest) error {
	return e.emit("ssh_exec.started", map[string]any{
		"host":        req.Host,
		"cmd_preview": cmdPreview(req.Command, 80),
		"project":     req.Project,
		"timeout_ms":  req.Timeout.Milliseconds(),
	})
}

func (e *dispatcherAuditEmitter) EmitCompleted(req sshexec.ExecRequest, res sshexec.ExecResult) error {
	return e.emit("ssh_exec.completed", map[string]any{
		"host":             req.Host,
		"cmd_preview":      cmdPreview(req.Command, 80),
		"project":          req.Project,
		"exit_code":        res.ExitCode,
		"exit_reason":      string(res.ExitReason),
		"duration_ms":      res.Duration.Milliseconds(),
		"stdout_bytes":     res.StdoutBytes,
		"stderr_bytes":     res.StderrBytes,
		"stdout_truncated": res.StdoutTruncated,
		"stderr_truncated": res.StderrTruncated,
	})
}

func (e *dispatcherAuditEmitter) EmitDenied(req sshexec.ExecRequest, reason string) error {
	return e.emit("ssh_exec.denied", map[string]any{
		"host":        req.Host,
		"cmd_preview": cmdPreview(req.Command, 80),
		"project":     req.Project,
		"reason":      reason,
	})
}

func (e *dispatcherAuditEmitter) EmitInteractiveBlocked(req sshexec.ExecRequest, snippet []byte) error {
	return e.emit("ssh_exec.interactive_blocked", map[string]any{
		"host":                    req.Host,
		"cmd_preview":             cmdPreview(req.Command, 80),
		"project":                 req.Project,
		"interactive_snippet_b64": base64.StdEncoding.EncodeToString(snippet),
	})
}

func (e *dispatcherAuditEmitter) emit(eventType string, payload map[string]any) error {
	if e.c == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := e.c.AuditEmit(ctx, client.AuditEmitReq{
		ProjectID: e.projectID,
		Type:      eventType,
		Payload:   payload,
	})
	return err
}

func cmdPreview(cmd string, n int) string {
	if len(cmd) <= n {
		return cmd
	}
	return cmd[:n] + "..."
}

// validateZenswarmTOMLPath resolves --toml; rejects paths outside cwd
// (security, prevents `--toml=/etc/passwd`) by absolute-pathing it.
//
// Used by per-project allowlist mutators to defend against path-escape.
//
// macOS quirk: `/var/folders/...` (TempDir) is a symlink to
// `/private/var/folders/...`. `os.Getwd()` post-Chdir returns the
// resolved form while `filepath.Abs` of a `/var/...` path keeps the
// unresolved form. We canonicalise both sides via filepath.EvalSymlinks
// before comparison so legitimate paths under TempDir aren't falsely
// rejected. EvalSymlinks errors only when the path doesn't exist; in
// that case we fall back to the lexical-prefix check.
func validateZenswarmTOMLPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	resolvedAbs := abs
	if r, rerr := filepath.EvalSymlinks(filepath.Dir(abs)); rerr == nil {
		resolvedAbs = filepath.Join(r, filepath.Base(abs))
	}
	resolvedCwd := cwd
	if r, rerr := filepath.EvalSymlinks(cwd); rerr == nil {
		resolvedCwd = r
	}

	cwdSep := resolvedCwd + string(filepath.Separator)
	defaultAtCwd := filepath.Join(resolvedCwd, "zenswarm.toml")
	rawCwdSep := cwd + string(filepath.Separator)
	rawDefaultAtCwd := filepath.Join(cwd, "zenswarm.toml")
	if strings.HasPrefix(resolvedAbs, cwdSep) || resolvedAbs == defaultAtCwd ||
		strings.HasPrefix(abs, rawCwdSep) || abs == rawDefaultAtCwd {
		return abs, nil
	}
	return "", fmt.Errorf("--toml must be under cwd: %s", abs)
}
