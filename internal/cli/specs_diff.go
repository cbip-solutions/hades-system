// SPDX-License-Identifier: MIT
// specs_diff.go — Task F-5 subcommand `zen specs diff <id>`.
//
// Renders OpenSpec deltas for a change directory. In OpenSpec's canonical
// layout (openspec/changes/<id>/), the deltas/ subdirectory IS the diff —
// each *.md file describes "what changes" against the corresponding
// openspec/specs/<area>.md when archived.
//
// `zen specs diff <change-id>` walks openspec/changes/<id>/deltas/ and
// concatenates each delta with a header. This is the read-only "what's
// pending" view of an in-flight change. Pure filesystem — no daemon call.
//
// `--v <from>..<to>` (git-version range) is NOT yet supported. The spec
// surface reserves the flag for a future revision that taps git history;
// until then, passing --v returns ErrRecoverable with a roadmap pointer.
//
// Read-only boundary per spec §0.2.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type SpecsDiffFlags struct {
	VersionRange string
}

func RunSpecsDiff(changesDir, changeID string, flags SpecsDiffFlags, w io.Writer) error {
	changeID = strings.TrimSpace(changeID)
	if changeID == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs diff: <change-id> is required"))
	}
	if err := validateSpecID(changeID); err != nil {
		return err
	}
	if strings.TrimSpace(flags.VersionRange) != "" {

		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable(
			"specs diff: --v <from>..<to> not yet supported in this revision "+
				"(reserved for a future plan; deltas/ directory is the canonical "+
				"diff for an in-flight change)",
		))
	}

	changeRoot := filepath.Join(changesDir, changeID)
	if _, err := os.Stat(changeRoot); errors.Is(err, os.ErrNotExist) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs diff: change %q not found in %s", changeID, changesDir))
	} else if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("specs diff: stat %s: %w", changeRoot, err))
	}

	deltasDir := filepath.Join(changeRoot, "deltas")
	entries, err := os.ReadDir(deltasDir)
	if errors.Is(err, os.ErrNotExist) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs diff: no deltas/ directory under %s", changeRoot))
	}
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("specs diff: read %s: %w", deltasDir, err))
	}

	type deltaFile struct {
		name string
		path string
	}
	deltas := make([]deltaFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		deltas = append(deltas, deltaFile{name: e.Name(), path: filepath.Join(deltasDir, e.Name())})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].name < deltas[j].name })

	if len(deltas) == 0 {
		_, werr := fmt.Fprintf(w, "(no delta files in %s)\n", deltasDir)
		return werr
	}

	for _, d := range deltas {
		if _, werr := fmt.Fprintf(w, "=== %s ===\n", d.name); werr != nil {
			return werr
		}
		data, err := os.ReadFile(d.path)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("specs diff: read %s: %w", d.path, err))
		}
		if _, werr := w.Write(data); werr != nil {
			return werr
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			if _, werr := fmt.Fprintln(w); werr != nil {
				return werr
			}
		}
		if _, werr := fmt.Fprintln(w); werr != nil {
			return werr
		}
	}
	return nil
}

func newSpecsDiffCmd(getDir specsChangesDirResolver) *cobra.Command {
	flags := SpecsDiffFlags{}
	cmd := &cobra.Command{
		Use:   "diff <change-id>",
		Short: "Render OpenSpec deltas for an in-flight change",
		Long: `Walk openspec/changes/<change-id>/deltas/ and render each delta
file's content. In OpenSpec's canonical layout, the deltas/ directory IS
the diff (what changes against openspec/specs/<area>.md when archived).

The --v <from>..<to> flag is reserved for a future revision that taps
git history; until then it returns exit 1 with a roadmap pointer.`,
		Example: `  zen specs diff zen-swarm-bootstrap
  zen specs diff feature-x`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunSpecsDiff(getDir(cmd), args[0], flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.VersionRange, "v", "", "Git version range (e.g. v1..v2) — reserved, not yet supported")
	return cmd
}
