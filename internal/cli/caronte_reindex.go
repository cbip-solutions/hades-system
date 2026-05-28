// SPDX-License-Identifier: MIT
// Package cli — caronte_reindex.go.
//
// `hades caronte reindex [project] [--all]` triggers a full reindex of one
// project (or every registered project with --all). The CLI POSTs the
// daemon's /v1/caronte/reindex endpoint with the X-HADES-Project-ID
// header; the daemon resolves the alias→canonical id_sha256 and
// delegates to the engine's IndexProject method.
//
// Resolution precedence:
//
// 1. `--all` enumerates every registered project via GET /v1/projects.
// 2. positional [project] alias-or-id used verbatim as the header value.
// 3. neither → CLI uses os.Getwd() as the alias; the daemon's resolver
// matches against projects_alias.canonical_path (the same trick
// `hades project doctor` uses for cwd-based resolution).
//
// invariant: the CLI does NOT pre-resolve aliases — the daemon-side
// handler is the single source of truth. This keeps the CLI thin (no
// store dependency) and the alias-resolution rules co-located with the
// projects_alias table.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const caronteReindexTimeout = 5 * time.Minute

type CaronteReindexClient interface {
	CaronteReindex(ctx context.Context, idOrAlias string) (*client.CaronteReindexResponse, error)
	CaronteProjectsList(ctx context.Context) (*client.CaronteProjectsListResponse, error)
}

type CaronteReindexFlags struct {
	Project string
	All     bool
	Format  string
}

type productionCaronteReindexClient struct {
	c *client.Client
}

func (p *productionCaronteReindexClient) CaronteReindex(ctx context.Context, idOrAlias string) (*client.CaronteReindexResponse, error) {
	return p.c.CaronteReindex(ctx, idOrAlias)
}

func (p *productionCaronteReindexClient) CaronteProjectsList(ctx context.Context) (*client.CaronteProjectsListResponse, error) {
	return p.c.CaronteProjectsList(ctx)
}

type caronteReindexClientFactory func(cmd interface{}) CaronteReindexClient

func NewCaronteReindexCmd(factory caronteReindexClientFactory) *cobra.Command {
	flags := CaronteReindexFlags{}
	cmd := &cobra.Command{
		Use:   "reindex [project]",
		Short: "Trigger initial / full reindex of a project's caronte graph",
		Long: `Trigger a full reindex of a project's Caronte graph.

With no argument: the CLI resolves the project from cwd (the daemon's
projects_alias.canonical_path lookup). With an explicit [project]
positional: alias or canonical id_sha256 passed verbatim. With --all:
the CLI enumerates every registered project via /v1/projects and
reindexes each sequentially.

The endpoint is blocking: returns when the per-project reindex
completes (or the operator's context times out — default 5 minutes).
Output is the IndexReport (counts, languages, duration); --format json
emits raw JSON for piping.

Closes the dangling hint at internal/cli/doctor_caronte.go:52
("trigger reindex: hades caronte reindex <project>").`,
		Example: " # Reindex the project anchored at cwd\n  hades caronte reindex\n\n # Reindex an explicit alias\n  hades caronte reindex <alias>\n\n # Reindex by canonical id\n  hades caronte reindex abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789\n\n # Reindex every registered project (sequential)\n  hades caronte reindex --all\n\n # JSON output for piping into jq\n  hades caronte reindex --format json | jq '.files_indexed'",

		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Project = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), caronteReindexTimeout)
			defer cancel()
			return RunCaronteReindex(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.All, "all", false, "reindex every registered project (sequential)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewCaronteReindexCmdProd() *cobra.Command {
	return NewCaronteReindexCmd(func(cmd interface{}) CaronteReindexClient {

		if cc, ok := cmd.(*cobra.Command); ok {
			return &productionCaronteReindexClient{c: newClientFromCmd(cc)}
		}
		return nil
	})
}

func RunCaronteReindex(ctx context.Context, c CaronteReindexClient, flags CaronteReindexFlags, w io.Writer) error {

	if flags.All && flags.Project != "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"),
			recoverable("--all and [project] are mutually exclusive"))
	}
	format := flags.Format
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"),
			recoverable("--format %q must be text or json", format))
	}

	if flags.All {
		list, err := c.CaronteProjectsList(ctx)
		if err != nil {
			return fmt.Errorf("caronte reindex --all: list projects: %w", err)
		}
		for _, p := range list.Projects {
			rep, rerr := c.CaronteReindex(ctx, p.Alias)
			if rerr != nil {

				fmt.Fprintf(w, "%s: ERROR %v\n", p.Alias, classifyCaronteReindexError(rerr))
				continue
			}
			renderCaronteReindexReport(w, format, rep)
		}
		return nil
	}

	idOrAlias := flags.Project
	if idOrAlias == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"),
				fmt.Errorf("caronte reindex: getwd: %w", err))
		}
		idOrAlias = cwd
	}
	rep, err := c.CaronteReindex(ctx, idOrAlias)
	if err != nil {
		return classifyCaronteReindexError(err)
	}
	renderCaronteReindexReport(w, format, rep)
	return nil
}

func renderCaronteReindexReport(w io.Writer, format string, rep *client.CaronteReindexResponse) {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}

	fmt.Fprintf(w, "caronte reindex: %s\n", rep.ProjectID)
	fmt.Fprintf(w, "  files_indexed:  %d\n", rep.FilesIndexed)
	fmt.Fprintf(w, "  nodes_created:  %d\n", rep.NodesCreated)
	if rep.EdgesCreated > 0 {
		fmt.Fprintf(w, "  edges_created:  %d\n", rep.EdgesCreated)
	}
	if len(rep.LanguageCounts) > 0 {
		fmt.Fprintf(w, "  languages:\n")
		for lang, n := range rep.LanguageCounts {
			fmt.Fprintf(w, "    %s: %d files\n", lang, n)
		}
	}
	fmt.Fprintf(w, "  duration_ms:    %d\n", rep.DurationMillis)
	if rep.Completed {
		fmt.Fprintf(w, "  status:         completed\n")
	} else {
		fmt.Fprintf(w, "  status:         INCOMPLETE\n")
	}
}

func classifyCaronteReindexError(err error) error {
	if err == nil {
		return nil
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"),
			recoverableWrap(err, "caronte reindex: project not found"))
	}
	if client.IsHTTPStatus(err, http.StatusBadRequest) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"),
			recoverableWrap(err, "caronte reindex: bad request"))
	}
	if client.IsHTTPStatus(err, http.StatusServiceUnavailable) {
		return ierrors.Wrap(ierrors.Code("plugin.mcp-handshake-fail"),
			recoverableWrap(err, "caronte reindex: daemon engine not configured (daemon will retry)"))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"),
		fmt.Errorf("caronte reindex: %w", err))
}
