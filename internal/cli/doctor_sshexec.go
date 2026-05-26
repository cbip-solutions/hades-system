// SPDX-License-Identifier: MIT
// Package cli — doctor_sshexec.go (Plan 4 Phase N Task N-8).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func doctorSSHExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sshexec",
		Short: "ssh-exec MCP health (allowlist, audit pipeline)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "SSH-Exec (Plan 4)", runSSHExecChecks)
		},
	}
}

func runSSHExecChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkSSHExecAllowlistResolves,
		checkSSHExecAuditEvents,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkSSHExecAllowlistResolves(ctx context.Context, c *client.Client) CheckResult {

	doc := doctrine.DefaultBuiltin()
	doctrineLabel := "default doctrine (daemon-down fallback)"
	if state, err := c.DoctrineStateCall(ctx); err == nil {
		if name := lookupDoctrineName(state); name != "" {
			if s, berr := doctrine.Builtin(name); berr == nil {
				doc = s
				doctrineLabel = fmt.Sprintf("%s doctrine", name)
			}
		}
	}
	if len(doc.SSHExec.Allowlist.Patterns) == 0 {
		return CheckResult{Name: "sshexec.allowlist.resolves", Status: "warn",
			Detail: fmt.Sprintf("%s has no allowlist patterns", doctrineLabel),
			Hint:   "every operator command will be denied"}
	}
	return CheckResult{Name: "sshexec.allowlist.resolves", Status: "ok",
		Detail: fmt.Sprintf("%d patterns / %d hosts (%s)",
			len(doc.SSHExec.Allowlist.Patterns), len(doc.SSHExec.Allowlist.Hosts), doctrineLabel)}
}

func checkSSHExecAuditEvents(ctx context.Context, c *client.Client) CheckResult {

	events, err := c.AuditEvents(ctx, "ssh_exec", "", 0, 5)
	if err != nil {
		return CheckResult{Name: "sshexec.audit.events", Status: "fail", Detail: err.Error()}
	}
	return CheckResult{Name: "sshexec.audit.events", Status: "ok",
		Detail: fmt.Sprintf("%d recent ssh-exec events", len(events))}
}
