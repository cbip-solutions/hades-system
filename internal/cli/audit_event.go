// SPDX-License-Identifier: MIT
// Package cli — audit_event.go
//
// zen://audit/<id> deep-link by dialing the daemon's GET
// /v1/audit/event/<id> endpoint shipped by Phase D
// (internal/daemon/handlers/audit_event.go).
//
// The command accepts both bare event-IDs ("evt-abc123") and the URL
// form ("zen://audit/evt-abc123"). The URL prefix is stripped client-
// side BEFORE the round-trip so the daemon always receives the bare
// id. Output formats: text (default; human-readable summary) or json.
//
// inv-zen-172: daemon validates event-id existence + auth + doctrine-
// aware filtering. The CLI is a transparent surface — recoverable
// vs unrecoverable error mapping mirrors knowledge/inbox patterns.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract):
//
//   - 0 success
//   - 1 operator-recoverable: empty ID, daemon 404 (event missing),
//     daemon 422 (auth/doctrine rejection)
//   - 2 unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const auditEventTimeout = 5 * time.Second

type AuditEventClient interface {
	AuditEventResolve(ctx context.Context, id string) (*client.AuditEventResolveResponse, error)
}

type AuditEventClientFactory func(cmd *cobra.Command) AuditEventClient

type AuditEventFlags struct {
	ID     string
	Format string
}

type productionAuditEventClient struct {
	c *client.Client
}

func (p *productionAuditEventClient) AuditEventResolve(ctx context.Context, id string) (*client.AuditEventResolveResponse, error) {
	return p.c.AuditEventResolve(ctx, id)
}

func NewAuditEventCmd(factory AuditEventClientFactory) *cobra.Command {
	flags := AuditEventFlags{}
	cmd := &cobra.Command{
		Use:   "event <id>",
		Short: "Resolve zen://audit/<id> deep-link to the structured audit event",
		Long: `Resolve a Plan 9 Tessera-anchored audit event by ID. Accepts both
bare event-IDs ("evt-abc123") and zen://audit URL form
("zen://audit/evt-abc123"); the URL prefix is stripped client-side.

The daemon enforces inv-zen-172: auth check + doctrine-aware filtering.

Output formats:
  text  human-readable summary (default)
  json  structured JSON for jq pipelines`,
		Example: `  # Bare ID
  zen audit event evt-abc123

  # zen:// URL form (e.g. paste from a Hermes citation)
  zen audit event "zen://audit/evt-abc123"

  # JSON for tooling
  zen audit event evt-abc123 --format json | jq '.detail.tokens_used'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.ID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), auditEventTimeout)
			defer cancel()
			return RunAuditEvent(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewAuditEventCmdProd() *cobra.Command {
	return NewAuditEventCmd(func(cmd *cobra.Command) AuditEventClient {
		return &productionAuditEventClient{c: newClientFromCmd(cmd)}
	})
}

func stripZenAuditURL(s string) string {
	const prefix = "zen://audit/"
	if strings.HasPrefix(s, prefix) {
		return strings.TrimPrefix(s, prefix)
	}
	return s
}

func RunAuditEvent(ctx context.Context, c AuditEventClient, flags AuditEventFlags, w io.Writer) error {
	id := strings.TrimSpace(stripZenAuditURL(flags.ID))
	if id == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("audit event: <id> is required (positional or zen://audit/<id> form)"))
	}
	format := flags.Format
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", format))
	}

	resp, err := c.AuditEventResolve(ctx, id)
	if err != nil {
		return classifyAuditEventError(err)
	}

	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	default:
		return writeAuditEventText(w, resp)
	}
}

func writeAuditEventText(w io.Writer, r *client.AuditEventResolveResponse) error {
	ts := time.Unix(r.TimestampUnix, 0).UTC().Format(time.RFC3339)
	fmt.Fprintf(w, "id:           %s\n", r.ID)
	fmt.Fprintf(w, "type:         %s\n", r.Type)
	fmt.Fprintf(w, "tessera_leaf: %s\n", r.TessLeaf)
	fmt.Fprintf(w, "timestamp:    %s\n", ts)
	if r.ProjectAlias != "" {
		fmt.Fprintf(w, "project:      %s\n", r.ProjectAlias)
	}
	if r.DoctrineName != "" {
		fmt.Fprintf(w, "doctrine:     %s\n", r.DoctrineName)
	}
	if len(r.Detail) > 0 {
		b, _ := json.MarshalIndent(r.Detail, "", "  ")
		fmt.Fprintf(w, "detail:       %s\n", string(b))
	}
	return nil
}

func classifyAuditEventError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "audit event: id not found (verify with: zen audit list --since 24h)"))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "audit event: daemon rejected request (auth or doctrine filter)"))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("audit event: %w", err))
}
