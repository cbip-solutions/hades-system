// SPDX-License-Identifier: MIT
// Package cli — projects.go.
//
// `zen projects ls` lists every project the daemon's project registry
// knows about, including archived (autonomous_state="complete") rows so
// the operator can see the full alias set with archive markers.
//
// Cobra layout:
//
// zen projects
// ls list known projects (alias, sha8, path, last-active, state)
//
// Distinction from `zen project` (singular): `zen project doctor / archive
// / rm / priority` operate on a single alias; `zen projects
// ls` is the cross-fleet view. The split mirrors `git remote` (singular
// management) vs `git remote ls` (plural inspection) — both surfaces
// matter and live as siblings, neither subsumes the other.
//
// Spec §6.2 column order (stable across releases so scripts can grep /
// awk the output): ALIAS, SHA8, PATH, LAST-ACTIVE, STATE.
//
// Drift from spec §6.2 example output: the spec shows a "QUOTA" + "PRIORITY"
// pair. Those columns belong to release quota substrate (live in
// `zen project priority --ls` per ) and would require a daemon-side
// JOIN that GET /v1/projects intentionally avoids ( cap: pure project
// registry; quota state surfaces via /v1/priority/* in ). Adding
// QUOTA + PRIORITY here would either widen the wire shape (cross-cutting
// concern, breaks SRP) or trigger a second round-trip per row (N+1 query).
// Operators wanting both views chain: `zen projects ls && zen project
// priority --ls`. The STATE column captures the archived/active distinction
// that operators need to spot dead aliases.
//
// Exit-code mapping (per spec §6.2 + the project.go ErrRecoverable sentinel):
// - 0 success (any row count, including zero)
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503/501 gap
//
// gap acknowledgment: the daemon-side `GET /v1/projects` is still
// the release 501-stub at HEAD (`internal/daemon/handlers/projects.go
// ProjectsList` returns notImplemented). release plan §"Step 4"
// scheduled the replacement (ProjectsP7List) but that closeout has not
// landed at HEAD f4b69c6. Until it does, operators see exit 2 with the
// 501 body — the CLI surface is final-shape day 1 nonetheless, mirroring
// the C-12 / D-13 / E-12 / G-11 graceful-degradation pattern.
package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

type ProjectRow = client.Project

type ProjectsClient interface {
	ListProjects(ctx context.Context) ([]ProjectRow, error)
}

type ProjectsClientFactory func(cmd *cobra.Command) ProjectsClient

const projectsTimeout = 5 * time.Second

func NewProjectsCmd(factory ProjectsClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "projects",
		Short: "Inspect known projects across the daemon (ls)",
		Long: `Cross-fleet view of the daemon's project registry.

Currently only "ls" is registered; future subcommands (Plan 7+ /
Plan 14) will extend this namespace without breaking the existing
surface.

Distinct from "zen project" (singular): plural form lists; singular
form acts on one alias (doctor / archive / rm / priority). The split
mirrors ` + "`git remote`" + ` (singular management) vs ` + "`git remote ls`" + `
(plural inspection) — both surfaces matter and live as siblings,
neither subsumes the other.`,
		Example: `  # List every alias the daemon knows about (active + archived)
  zen projects ls`,
	}
	root.AddCommand(newProjectsLsCmd(factory))
	return root
}

func NewProjectsCmdProd() *cobra.Command {
	return NewProjectsCmd(func(cmd *cobra.Command) ProjectsClient {
		return &productionProjectsClient{c: newClientFromCmd(cmd)}
	})
}

func newProjectsLsCmd(factory ProjectsClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List known projects (alias, sha8, path, last-active, state)",
		Long: `List every project the daemon's project registry knows about,
including archived rows so the operator can see the full alias set
with archive markers.

Columns (stable across releases for sed/awk pipelines per spec §6.2):
  ALIAS         operator-facing name (zenswarm.toml [project].alias)
  SHA8          first 8 hex chars of project_id (sha256)
  PATH          canonical absolute path the registry agrees on
  LAST-ACTIVE   relative time since last activation (e.g. "2h ago")
  STATE         "active" or "archived" (autonomous_state column)

Quota + priority intentionally omitted from this view — they live on
` + "`zen project priority --ls`" + ` to keep this command a pure registry
listing (no N+1 round-trips, no cross-cutting JOIN).

Exit codes (spec §6.2):
  0  success (any row count, including zero)
  2  unrecoverable: transport, decode, daemon 5xx, daemon 503/501`,
		Example: `  # List every known project
  zen projects ls

  # Pipe through awk for the alias column only
  zen projects ls | awk 'NR>1 {print $1}'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), projectsTimeout)
			defer cancel()
			rows, err := c.ListProjects(ctx)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("projects ls: %w", err))
			}
			renderProjectsList(cmd.OutOrStdout(), rows, time.Now())
			return nil
		},
	}
}

func renderProjectsList(w interface{ Write([]byte) (int, error) }, rows []ProjectRow, now time.Time) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no projects registered")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tSHA8\tPATH\tLAST-ACTIVE\tSTATE")
	for _, r := range rows {
		state := "active"
		if r.IsArchived() {
			state = "archived"
		}
		lastActive := "never"
		if !r.LastActivatedAt.IsZero() {
			lastActive = humanizeRelative(now, r.LastActivatedAt)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.Alias, sha8Of(r.ID), r.Path, lastActive, state)
	}
	_ = tw.Flush()
}

func sha8Of(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func humanizeRelative(now, t time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	if d < 30*24*time.Hour {
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
	return t.UTC().Format("2006-01-02")
}

type productionProjectsClient struct {
	c *client.Client
}

func (p *productionProjectsClient) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	return p.c.ProjectsListAll(ctx)
}
