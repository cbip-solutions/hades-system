// SPDX-License-Identifier: MIT
// Package cli — workspace.go.
//
// `hades workspace {init,list,members,link,remove,policy {get,set}}` — 7 verbs
// total. The DECISION-5 policy-set flow is the load-bearing core:
// 1. CLI emits the release audit row `policy_change_requested` FIRST
// (records operator intent regardless of prompt outcome);
// 2. interactive confirmation prompt via cmd.InOrStdin() + bufio reader;
// 3a. on confirm + daemon-OK: daemon API call performs the change + emits
// a SECOND audit row `policy_change_committed`;
// 3b. on confirm + daemon-error (e.g. capa-firewall denied): daemon API
// call fails + CLI emits `policy_change_failed` (committed intent +
// failed execution) before propagating the recoverable error;
// 3c. on abort: no daemon API call + emits a SECOND audit row
// `policy_change_aborted`.
//
// Four audit event-types total: `policy_change_requested` (always),
// `policy_change_committed` (confirm + daemon-OK), `policy_change_failed`
// (confirm + daemon-error), `policy_change_aborted` (operator declined the
// prompt). The 1+1 contract holds: `requested` always fires first; exactly
// one of {committed | failed | aborted} fires second. Pinned by
// TestRunWorkspacePolicySet* sister-tests in workspace_test.go.
//
// `--yes` bypasses the prompt for scripted runs; non-interactive without
// `--yes` is rejected (destructive-default-no per project instructions hard rule).
//
// Routes via the daemon /v1/mcpgateway/workspace/* sub-routes (inv-hades-088
// single-egress, inv-hades-129 no direct net/http). Tests inject
// WorkspaceClient fakes.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const (
	workspaceTimeout       = 15 * time.Second
	workspacePolicyTimeout = 30 * time.Second
)

type WorkspaceClient interface {
	WorkspaceInit(ctx context.Context, req client.WorkspaceInitRequest) (*client.WorkspaceInitResponse, error)
	WorkspaceList(ctx context.Context, req client.WorkspaceListRequest) (*client.WorkspaceListResponse, error)
	WorkspaceMembers(ctx context.Context, req client.WorkspaceMembersRequest) (*client.WorkspaceMembersResponse, error)
	WorkspaceLink(ctx context.Context, req client.WorkspaceLinkRequest) (*client.WorkspaceLinkResponse, error)
	WorkspaceRemove(ctx context.Context, req client.WorkspaceRemoveRequest) (*client.WorkspaceRemoveResponse, error)
	WorkspacePolicyGet(ctx context.Context, req client.WorkspacePolicyGetRequest) (*client.WorkspacePolicyGetResponse, error)
	WorkspacePolicySet(ctx context.Context, req client.WorkspacePolicySetRequest) (*client.WorkspacePolicySetResponse, error)

	EmitAudit(ctx context.Context, eventType string, payload map[string]any) error
}

type productionWorkspaceClient struct{ c *client.Client }

func (p *productionWorkspaceClient) WorkspaceInit(ctx context.Context, req client.WorkspaceInitRequest) (*client.WorkspaceInitResponse, error) {
	return p.c.WorkspaceInit(ctx, req)
}
func (p *productionWorkspaceClient) WorkspaceList(ctx context.Context, req client.WorkspaceListRequest) (*client.WorkspaceListResponse, error) {
	return p.c.WorkspaceList(ctx, req)
}
func (p *productionWorkspaceClient) WorkspaceMembers(ctx context.Context, req client.WorkspaceMembersRequest) (*client.WorkspaceMembersResponse, error) {
	return p.c.WorkspaceMembers(ctx, req)
}
func (p *productionWorkspaceClient) WorkspaceLink(ctx context.Context, req client.WorkspaceLinkRequest) (*client.WorkspaceLinkResponse, error) {
	return p.c.WorkspaceLink(ctx, req)
}
func (p *productionWorkspaceClient) WorkspaceRemove(ctx context.Context, req client.WorkspaceRemoveRequest) (*client.WorkspaceRemoveResponse, error) {
	return p.c.WorkspaceRemove(ctx, req)
}
func (p *productionWorkspaceClient) WorkspacePolicyGet(ctx context.Context, req client.WorkspacePolicyGetRequest) (*client.WorkspacePolicyGetResponse, error) {
	return p.c.WorkspacePolicyGet(ctx, req)
}
func (p *productionWorkspaceClient) WorkspacePolicySet(ctx context.Context, req client.WorkspacePolicySetRequest) (*client.WorkspacePolicySetResponse, error) {
	return p.c.WorkspacePolicySet(ctx, req)
}
func (p *productionWorkspaceClient) EmitAudit(ctx context.Context, eventType string, payload map[string]any) error {
	_, err := p.c.AuditEmit(ctx, client.AuditEmitReq{Type: eventType, Payload: payload})
	return err
}

func NewWorkspaceCmdProd() *cobra.Command {
	root := &cobra.Command{
		Use:   "workspace",
		Short: "Plan 20 federation workspace lifecycle",
		Long:  `Manage cross-repo API-contract federation workspaces: init/list/members/link/remove + policy get/set.`,
	}
	root.AddCommand(NewWorkspaceInitCmdProd())
	root.AddCommand(NewWorkspaceListCmdProd())
	root.AddCommand(NewWorkspaceMembersCmdProd())
	root.AddCommand(NewWorkspaceLinkCmdProd())
	root.AddCommand(NewWorkspaceRemoveCmdProd())

	policy := &cobra.Command{
		Use:   "policy",
		Short: "workspace privacy policy get/set (DECISION 5: audit-then-prompt)",
	}
	policy.AddCommand(NewWorkspacePolicyGetCmdProd())
	policy.AddCommand(NewWorkspacePolicySetCmdProd())
	root.AddCommand(policy)
	return root
}

type WorkspaceInitFlags struct {
	WorkspaceID   string
	OwningProject string
	Members       []string
	PolicyLocked  bool
	Format        string
}

func NewWorkspaceInitCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspaceInitFlags{}
	cmd := &cobra.Command{
		Use:   "init <workspace_id>",
		Short: "Create a new Plan-20 federation workspace",
		Long:  `Register a new workspace with an owning project + optional member roster + initial policy lock.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspaceTimeout)
			defer cancel()
			return RunWorkspaceInit(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.OwningProject, "owner", "", "owning project id (required)")
	cmd.Flags().StringSliceVar(&flags.Members, "member", nil, "initial member project id (repeatable)")
	cmd.Flags().BoolVar(&flags.PolicyLocked, "locked", false, "set initial policy_locked=true")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspaceInit(ctx context.Context, c WorkspaceClient, flags WorkspaceInitFlags, w io.Writer) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace init: <workspace_id> is required"))
	}
	if strings.TrimSpace(flags.OwningProject) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace init: --owner is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.WorkspaceInit(ctx, client.WorkspaceInitRequest{
		WorkspaceID:   flags.WorkspaceID,
		OwningProject: flags.OwningProject,
		Members:       flags.Members,
		PolicyLocked:  flags.PolicyLocked,
	})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace init")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "workspace %s created (owner %s, schema v%d, created_at %d)\n",
		resp.WorkspaceID, flags.OwningProject, resp.SchemaVersion, resp.CreatedAt)
	return nil
}

func NewWorkspaceInitCmdProd() *cobra.Command {
	return NewWorkspaceInitCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspaceListFlags struct {
	Format string
}

func NewWorkspaceListCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspaceListFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered workspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspaceTimeout)
			defer cancel()
			return RunWorkspaceList(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspaceList(ctx context.Context, c WorkspaceClient, flags WorkspaceListFlags, w io.Writer) error {
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.WorkspaceList(ctx, client.WorkspaceListRequest{})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace list")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if len(resp.Workspaces) == 0 {
		fmt.Fprintln(w, "(no workspaces registered)")
		return nil
	}
	fmt.Fprintf(w, "%-32s %-24s %-8s %-6s %s\n", "WORKSPACE", "OWNER", "LOCKED", "SCHEMA", "CREATED")
	for _, ws := range resp.Workspaces {
		locked := "no"
		if ws.PolicyLocked {
			locked = "yes"
		}
		fmt.Fprintf(w, "%-32s %-24s %-8s %-6d %d\n", ws.WorkspaceID, ws.OwningProject, locked, ws.SchemaVersion, ws.CreatedAt)
	}
	return nil
}

func NewWorkspaceListCmdProd() *cobra.Command {
	return NewWorkspaceListCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspaceMembersFlags struct {
	WorkspaceID string
	Format      string
}

func NewWorkspaceMembersCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspaceMembersFlags{}
	cmd := &cobra.Command{
		Use:   "members <workspace_id>",
		Short: "List the member roster of a workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspaceTimeout)
			defer cancel()
			return RunWorkspaceMembers(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspaceMembers(ctx context.Context, c WorkspaceClient, flags WorkspaceMembersFlags, w io.Writer) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace members: <workspace_id> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.WorkspaceMembers(ctx, client.WorkspaceMembersRequest{WorkspaceID: flags.WorkspaceID})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace members")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if len(resp.Members) == 0 {
		fmt.Fprintf(w, "workspace %s has no members\n", flags.WorkspaceID)
		return nil
	}
	for _, m := range resp.Members {
		fmt.Fprintf(w, "%s (registered %d)\n", m.ProjectID, m.RegisteredAt)
	}
	return nil
}

func NewWorkspaceMembersCmdProd() *cobra.Command {
	return NewWorkspaceMembersCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspaceLinkFlags struct {
	WorkspaceID string
	ProjectID   string
	Format      string
}

func NewWorkspaceLinkCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspaceLinkFlags{}
	cmd := &cobra.Command{
		Use:   "link <workspace_id> <project_id>",
		Short: "Add a project to a workspace's member roster",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			if len(args) > 1 {
				flags.ProjectID = args[1]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspaceTimeout)
			defer cancel()
			return RunWorkspaceLink(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspaceLink(ctx context.Context, c WorkspaceClient, flags WorkspaceLinkFlags, w io.Writer) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace link: <workspace_id> is required"))
	}
	if strings.TrimSpace(flags.ProjectID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace link: <project_id> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.WorkspaceLink(ctx, client.WorkspaceLinkRequest{
		WorkspaceID: flags.WorkspaceID, ProjectID: flags.ProjectID,
	})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace link")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "linked %s to workspace %s (registered_at %d)\n",
		resp.ProjectID, resp.WorkspaceID, resp.RegisteredAt)
	return nil
}

func NewWorkspaceLinkCmdProd() *cobra.Command {
	return NewWorkspaceLinkCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspaceRemoveFlags struct {
	WorkspaceID string
	AssumeYes   bool
	Format      string
}

func NewWorkspaceRemoveCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspaceRemoveFlags{}
	cmd := &cobra.Command{
		Use:   "remove <workspace_id>",
		Short: "Delete a workspace (destructive; requires --yes in non-interactive mode)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspaceTimeout)
			defer cancel()
			return RunWorkspaceRemove(ctx, c, flags, cmd.InOrStdin(), cmd.OutOrStdout(), isInteractiveStdin(cmd))
		},
	}
	cmd.Flags().BoolVar(&flags.AssumeYes, "yes", false, "skip interactive confirmation")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspaceRemove(ctx context.Context, c WorkspaceClient, flags WorkspaceRemoveFlags, in io.Reader, w io.Writer, isInteractive bool) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace remove: <workspace_id> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	if !isInteractive && !flags.AssumeYes {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace remove: --yes required in non-interactive mode"))
	}
	if !flags.AssumeYes {
		fmt.Fprintf(w, "Remove workspace %q? This is destructive. [y/N] ", flags.WorkspaceID)
		ans, err := readPromptAnswer(in)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "workspace remove: prompt read failed"))
		}
		if ans != "y" && ans != "yes" {
			fmt.Fprintln(w, "aborted.")
			return nil
		}
	}
	resp, err := c.WorkspaceRemove(ctx, client.WorkspaceRemoveRequest{WorkspaceID: flags.WorkspaceID})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace remove")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "workspace %s removed (rows_affected %d)\n", resp.WorkspaceID, resp.RowsAffected)
	return nil
}

func NewWorkspaceRemoveCmdProd() *cobra.Command {
	return NewWorkspaceRemoveCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspacePolicyGetFlags struct {
	WorkspaceID string
	Format      string
}

func NewWorkspacePolicyGetCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspacePolicyGetFlags{}
	cmd := &cobra.Command{
		Use:   "get <workspace_id>",
		Short: "Read the workspace's operator-mutable privacy policy",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspacePolicyTimeout)
			defer cancel()
			return RunWorkspacePolicyGet(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspacePolicyGet(ctx context.Context, c WorkspaceClient, flags WorkspacePolicyGetFlags, w io.Writer) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace policy get: <workspace_id> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.WorkspacePolicyGet(ctx, client.WorkspacePolicyGetRequest{WorkspaceID: flags.WorkspaceID})
	if err != nil {
		return classifyCapaFirewallError(err, "workspace policy get")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if resp.Policy == "" {
		fmt.Fprintf(w, "workspace %s has no policy set\n", resp.WorkspaceID)
		return nil
	}
	fmt.Fprintf(w, "workspace %s policy:\n%s\n", resp.WorkspaceID, resp.Policy)
	return nil
}

func NewWorkspacePolicyGetCmdProd() *cobra.Command {
	return NewWorkspacePolicyGetCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

type WorkspacePolicySetFlags struct {
	WorkspaceID string
	Policy      string
	AssumeYes   bool
	Format      string
}

func NewWorkspacePolicySetCmd(factory func(cmd *cobra.Command) WorkspaceClient) *cobra.Command {
	flags := WorkspacePolicySetFlags{}
	cmd := &cobra.Command{
		Use:   "set <workspace_id> <policy>",
		Short: "Set the workspace's privacy policy (audit-then-prompt; DECISION 5)",
		Long: `Mutate the workspace's operator-mutable privacy policy. Emits a
plan-14 audit row BEFORE prompting (records operator intent regardless of
prompt outcome), then prompts in interactive mode; on confirm/abort emits a
SECOND audit row encoding the outcome. --yes bypasses the prompt for
scripted runs (but BOTH audit rows still emit). Non-interactive without
--yes is rejected (destructive-default-no).`,
		Example: `  hades workspace policy set ws-1 locked
  hades workspace policy set ws-1 permissive --yes`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			if len(args) > 1 {
				flags.Policy = args[1]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), workspacePolicyTimeout)
			defer cancel()
			return RunWorkspacePolicySet(ctx, c, flags, cmd.InOrStdin(), cmd.OutOrStdout(), isInteractiveStdin(cmd))
		},
	}
	cmd.Flags().BoolVar(&flags.AssumeYes, "yes", false, "bypass the interactive confirmation prompt")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunWorkspacePolicySet(ctx context.Context, c WorkspaceClient, flags WorkspacePolicySetFlags, in io.Reader, w io.Writer, isInteractive bool) error {
	if strings.TrimSpace(flags.WorkspaceID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace policy set: <workspace_id> is required"))
	}
	if flags.Policy != "locked" && flags.Policy != "permissive" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace policy set: <policy> must be locked|permissive (got %q)", flags.Policy))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	if !isInteractive && !flags.AssumeYes {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("workspace policy set: --yes required in non-interactive mode"))
	}

	requestPayload := map[string]any{
		"workspace_id": flags.WorkspaceID,
		"new_policy":   flags.Policy,
		"assume_yes":   flags.AssumeYes,
	}
	if err := c.EmitAudit(ctx, "policy_change_requested", requestPayload); err != nil {
		return classifyCapaFirewallError(err, "workspace policy set audit-request")
	}

	if !flags.AssumeYes {
		fmt.Fprintf(w, "Set workspace %q policy to %q? This is a destructive operation. [y/N] ", flags.WorkspaceID, flags.Policy)
		ans, err := readPromptAnswer(in)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "workspace policy set: prompt read failed"))
		}
		if ans != "y" && ans != "yes" {

			abortPayload := map[string]any{"workspace_id": flags.WorkspaceID, "reason": "user_declined"}
			_ = c.EmitAudit(ctx, "policy_change_aborted", abortPayload)
			fmt.Fprintln(w, "aborted.")
			return nil
		}
	}

	resp, err := c.WorkspacePolicySet(ctx, client.WorkspacePolicySetRequest{
		WorkspaceID: flags.WorkspaceID, NewPolicy: flags.Policy,
	})
	if err != nil {

		_ = c.EmitAudit(ctx, "policy_change_failed", map[string]any{
			"workspace_id": flags.WorkspaceID, "error": err.Error(),
		})
		return classifyCapaFirewallError(err, "workspace policy set")
	}

	commitPayload := map[string]any{"workspace_id": flags.WorkspaceID, "new_policy": resp.NewPolicy}
	if err := c.EmitAudit(ctx, "policy_change_committed", commitPayload); err != nil {
		return classifyCapaFirewallError(err, "workspace policy set audit-commit")
	}

	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "workspace %s policy set to %s\n", flags.WorkspaceID, resp.NewPolicy)
	return nil
}

func NewWorkspacePolicySetCmdProd() *cobra.Command {
	return NewWorkspacePolicySetCmd(func(cmd *cobra.Command) WorkspaceClient {
		return &productionWorkspaceClient{c: newClientFromCmd(cmd)}
	})
}

func readPromptAnswer(in io.Reader) (string, error) {
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(line)), nil
}

func isInteractiveStdin(_ *cobra.Command) bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
