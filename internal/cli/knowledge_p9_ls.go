// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_ls.go.
//
// `hades knowledge-p9 ls` — list notes from the HADES design aggregator.
//
// Wire method: KnowledgeListP9(ctx, projectID, pinnedOnly) → ([]KnowledgeNote, error).
// KnowledgeNote fields: NoteID, ProjectID, Path, Pinned, UpdatedAt (no Title).
//
// # Flags
//
// --project <id> Scope to one project (empty = all).
// --pinned-only Only list pinned notes.
package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func knowledge9LsCmd() *cobra.Command {
	var project string
	var pinnedOnly bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List notes from the aggregator (per-project or pinned-only)",
		Long:  "List notes tracked by the HADES design knowledge aggregator. Without flags,\nlists all notes across all projects. Filter to a single project with --project;\nfilter to pinned notes with --pinned-only.\n\nOutput columns: NOTE_ID / PROJECT / PATH / PINNED / UPDATED",

		Example: " # All notes\n  hades knowledge-p9 ls\n\n # One project\n  hades knowledge-p9 ls --project internal-platform-x\n\n # Pinned-only across all projects\n  hades knowledge-p9 ls --pinned-only",

		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).KnowledgeListP9(ctx, project, pinnedOnly)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if len(items) == 0 {
				fmt.Fprintln(w, "(no notes)")
				return nil
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NOTE_ID\tPROJECT\tPATH\tPINNED")
			for _, n := range items {
				pinned := "false"
				if n.Pinned {
					pinned = "true"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					n.NoteID,
					n.ProjectID,
					n.Path,
					pinned,
				)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&pinnedOnly, "pinned-only", false, "Only list pinned notes")
	return cmd
}
