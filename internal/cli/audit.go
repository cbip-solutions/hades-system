// SPDX-License-Identifier: MIT
// Package cli — audit.go.
//
// `hades audit` exposes audit-event read + emit + family/criteria catalogs.
//
// Cobra layout (7 leaves):
//
// hades audit emit --type --project --payload (JSON)
// hades audit events --type --project --since --limit
// hades audit verdicts (alias: events --type=audit_review)
// hades audit types (catalog of distinct event types last 30d)
// hades audit families show
// hades audit criteria list
//
// Option A adaptation: the plan-doc enumerated `review --diff <p>` which
// requires the audit MCP wiring landed in HADES design (the audit MCP
// is the HADES design deliverable, not ). delivers
// the read/catalog surface complete + real-round-tripped; the
// `review --diff` proxy command will land additively in a future plan
// (deferred to a future plan) without changing this surface.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit emit + query + family-disjoint pool catalog (HADES design)",
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(auditEmitCmd())
	cmd.AddCommand(auditEventsCmd())
	cmd.AddCommand(auditVerdictsCmd())
	cmd.AddCommand(auditTypesCmd())
	cmd.AddCommand(auditFamiliesCmd())
	cmd.AddCommand(auditCriteriaCmd())
	cmd.AddCommand(NewAuditEventCmdProd())
	return cmd
}

func auditEmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Manually emit an audit event (operator escape hatch)",
		Long: `Write one audit event into audit_events_raw. Operators rarely
need this; provided for incident remediation and manual testing of
notification routing. The daemon enqueues + writes synchronously.

Required: --type. Optional: --project, --payload (JSON object).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			eventType, _ := cmd.Flags().GetString("type")
			projectID, _ := cmd.Flags().GetString("project")
			payloadStr, _ := cmd.Flags().GetString("payload")
			if eventType == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--type required (e.g. operator.manual_emit)"))
			}
			var payload any = map[string]any{}
			if payloadStr != "" {
				if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--payload must be valid JSON: %w", err))
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := newClientFromCmd(cmd).AuditEmit(ctx, client.AuditEmitReq{
				ProjectID: projectID, Type: eventType, Payload: payload,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id=%s accepted=%t emitted_at=%s\n",
				resp.ID, resp.Accepted, client.FormatUnix(resp.EmittedAt))
			return nil
		},
	}
	cmd.Flags().String("type", "", "Event type (e.g. operator.manual_emit, ssh_exec.started)")
	cmd.Flags().String("project", "", "Project ID (optional)")
	cmd.Flags().String("payload", "", "Payload as JSON object (e.g. '{\"k\":\"v\"}')")
	return cmd
}

func auditEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Recent audit events (filterable by type prefix + project + since)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			return runAuditEventsQuery(cmd, "")
		},
	}
	cmd.Flags().String("type", "", "Filter by type prefix (e.g. audit_review)")
	cmd.Flags().String("project", "", "Filter by project ID")
	return cmd
}

func auditVerdictsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verdicts",
		Short: "Recent audit_review verdicts (alias: events --type=audit_review)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			return runAuditEventsQuery(cmd, "audit_review")
		},
	}
	cmd.Flags().String("project", "", "Filter by project ID")
	return cmd
}

func runAuditEventsQuery(cmd *cobra.Command, defaultTypePrefix string) error {
	typePrefix := defaultTypePrefix
	if v, _ := cmd.Flags().GetString("type"); v != "" {
		typePrefix = v
	}
	projectID, _ := cmd.Flags().GetString("project")
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
	events, err := newClientFromCmd(cmd).AuditEvents(ctx, typePrefix, projectID, sinceUnix, limit)
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
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncatePayload(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func auditTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types",
		Short: "Distinct audit event types (last 30 days, with counts)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			items, err := newClientFromCmd(cmd).AuditTypes(ctx)
			if err != nil {
				return err
			}
			cols := []format.Column{
				{Header: "TYPE", Field: func(r any) string { return r.(client.AuditType).Type }},
				{Header: "COUNT_30D", Field: func(r any) string { return strconv.Itoa(r.(client.AuditType).Count) }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
}

func auditFamiliesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "families",
		Short: "Family-disjoint reviewer pool catalog (invariant)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show currently-resolved family pool",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}

			fams, activeName := resolveAuditFamilies(cmd)
			cols := []format.Column{
				{Header: "NAME", Field: func(r any) string { return r.(client.AuditFamily).Name }},
				{Header: "DEFAULT", Field: func(r any) string { return strconv.FormatBool(r.(client.AuditFamily).Default) }},
				{Header: "DESCRIPTION", Field: func(r any) string { return r.(client.AuditFamily).Description }},
			}
			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()
			if opts.Format == "table" {
				fmt.Fprintf(out, "Family-disjoint reviewer pool (invariant) — active doctrine: %s\n", activeName)
				fmt.Fprintf(out, "  - reviewer family != generator family\n")
				fmt.Fprintf(out, "  - |pool| >= 2 enforced by doctrine validator\n\n")
			}
			return format.Render(out, opts, fams, cols)
		},
	})
	return cmd
}

func resolveAuditFamilies(cmd *cobra.Command) ([]client.AuditFamily, string) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()

	state, err := newClientFromCmd(cmd).DoctrineStateCall(ctx)
	if err != nil {

		s := doctrine.DefaultBuiltin()
		return client.AuditFamiliesFromPool(s.Reviewer.FamilyDisjointPool), s.Name
	}

	fams, _ := newClientFromCmd(cmd).AuditFamiliesResolveFromState(state)

	activeName := lookupString(state, "name")
	if activeName == "" {
		activeName = lookupString(state, "Name")
	}

	if len(fams) == 0 && activeName != "" {
		if s, berr := doctrine.Builtin(activeName); berr == nil {
			fams = client.AuditFamiliesFromPool(s.Reviewer.FamilyDisjointPool)
		}
	}

	if len(fams) == 0 {
		s := doctrine.DefaultBuiltin()
		fams = client.AuditFamiliesFromPool(s.Reviewer.FamilyDisjointPool)
		if activeName == "" {
			activeName = s.Name
		}
	}
	return fams, activeName
}

func lookupString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func auditCriteriaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "criteria",
		Short: "Audit criteria template catalog (list | show)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List criteria templates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			items := client.AuditCriteria()
			cols := []format.Column{
				{Header: "NAME", Field: func(r any) string { return r.(client.AuditCriterion).Name }},
				{Header: "SOURCE", Field: func(r any) string { return r.(client.AuditCriterion).Source }},
				{Header: "DESCRIPTION", Field: func(r any) string { return r.(client.AuditCriterion).Description }},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show description for a criterion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			items := client.AuditCriteria()
			for _, c := range items {
				if c.Name == args[0] {
					out := cmd.OutOrStdout()
					fmt.Fprintf(out, "Name:        %s\n", c.Name)
					fmt.Fprintf(out, "Source:      %s\n", c.Source)
					fmt.Fprintf(out, "Description: %s\n", c.Description)
					return nil
				}
			}
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("criterion %q not found", args[0]))
		},
	})
	return cmd
}
