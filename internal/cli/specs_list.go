// SPDX-License-Identifier: MIT
// specs_list.go — release Task F-5 subcommand `hades specs list`.
//
// Pure filesystem read of openspec/specs/ — no daemon call. Walks the
// directory for *.md files, extracts the first non-empty title-line,
// renders as text (tabwriter) or json.
//
// Read-only boundary per spec §0.2: specs are read-only at the CLI
// surface in release. Write-back deferred to post-v0.14.0.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type SpecsListFlags struct {
	Format string
}

var validSpecsFormats = map[string]bool{
	"text": true,
	"json": true,
	"md":   true,
}

var validSpecsListFormats = map[string]bool{
	"text": true,
	"json": true,
}

func RunSpecsList(specsDir string, format string, w io.Writer) error {
	if format == "" {
		format = "text"
	}
	if !validSpecsListFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs list: --format %q: must be one of text|json", format))
	}

	entries, err := os.ReadDir(specsDir)
	if errors.Is(err, os.ErrNotExist) {
		_, werr := fmt.Fprintln(w, "(no specs directory found)")
		return werr
	}
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("specs list: %w", err))
	}

	type specEntry struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	specs := make([]specEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		if strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		title := readSpecsFirstLine(filepath.Join(specsDir, e.Name()))
		specs = append(specs, specEntry{ID: id, Title: title})
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(specs)
	}
	if len(specs) == 0 {
		_, err := fmt.Fprintln(w, "(no specs)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE")
	for _, s := range specs {
		fmt.Fprintf(tw, "%s\t%s\n", s.ID, s.Title)
	}
	return tw.Flush()
}

func readSpecsFirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return filepath.Base(path)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
		return line
	}
	return filepath.Base(path)
}

func newSpecsListCmd(getDir specsDirResolver) *cobra.Command {
	flags := SpecsListFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List specs from openspec/specs/",
		Long: `Walk openspec/specs/ and render the spec ID + first-line title.

Pure filesystem read — no daemon call (read-only boundary per spec §0.2).
Missing directory prints "(no specs directory found)" without error so
the command is safe to run on a fresh repo.`,
		Example: `  hades specs list
  hades specs list --format json | jq '.[].id'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunSpecsList(getDir(cmd), flags.Format, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "Output format: text|json")
	return cmd
}
