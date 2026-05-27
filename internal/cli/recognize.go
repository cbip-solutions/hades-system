// SPDX-License-Identifier: MIT
// Package cli — recognize.go.
//
// `zen recognize` thin wrapper consuming internal/recognize public API.
// Surface per spec §7.4:
//
// zen recognize [PATH] [--json] [--no-audit]
//
// PATH defaults to cwd. --json emits Result JSON (schemaVersion="1.0");
// default renders human-readable table. --no-audit skips Tessera emit per
// spec §3.7.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/recognize"
	"github.com/spf13/cobra"
)

func NewRecognizeCmd() *cobra.Command {
	var (
		jsonOut bool
		noAudit bool
	)
	cmd := &cobra.Command{
		Use:   "recognize [PATH]",
		Short: "Infer project type/language/framework via three-tier signal stack",
		Long: `Recognize runs the three-tier signal stack (manifest > config > glob)
plus monorepo walk-UP and maturity probe against PATH (defaults to cwd).

Output:
  - default: human-readable table
  - --json:  Result JSON with schemaVersion="1.0"

Audit:
  - default: emit evt.recognize.run via Tessera chain
  - --no-audit: skip emit (sensitive repos)
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			abs, err := filepath.Abs(target)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("recognize: resolve PATH: %w", err))
			}
			info, err := os.Stat(abs)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("recognize: stat %q: %w", abs, err))
			}
			if !info.IsDir() {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("recognize: PATH must be a directory: %q", abs))
			}
			opts := recognize.Options{
				NoAudit:     noAudit,
				RootAbsPath: abs,
			}
			rec := recognize.New(opts)
			result, err := rec.Recognize(cmd.Context(), os.DirFS(abs))
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				return writeRecognizeJSON(out, result)
			}
			return writeRecognizeHuman(out, abs, result)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON Result (schemaVersion=1.0)")
	cmd.Flags().BoolVar(&noAudit, "no-audit", false, "skip Tessera audit emit (sensitive repos)")
	return cmd
}

func writeRecognizeJSON(out io.Writer, r recognize.Result) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func writeRecognizeHuman(out io.Writer, path string, r recognize.Result) error {
	fmt.Fprintf(out, "Path: %s\n", path)
	if r.PrimaryLanguage != "" {
		fmt.Fprintf(out, "Primary language: %s (%.0f%% bytes)\n", r.PrimaryLanguage, r.PrimaryConfidence*100)
	} else {
		fmt.Fprintln(out, "Primary language: (none detected)")
	}
	if len(r.Ecosystems) > 0 {
		fmt.Fprint(out, "Ecosystems:")
		for _, e := range r.Ecosystems {
			fmt.Fprintf(out, " %s (%s, confidence %.2f)", e.Ecosystem, e.Evidence, e.Confidence)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "Ecosystems: (none)")
	}
	if len(r.Frameworks) > 0 {
		fmt.Fprint(out, "Frameworks:")
		for _, f := range r.Frameworks {
			fmt.Fprintf(out, " %s (%s, confidence %.2f)", f.Framework, f.ConfigPath, f.Confidence)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "Frameworks: (none)")
	}
	if r.Monorepo != nil {
		fmt.Fprintf(out, "Monorepo: %s at %s\n", r.Monorepo.Tool, r.Monorepo.Root)
	} else {
		fmt.Fprintln(out, "Monorepo: (none)")
	}
	if r.Maturity.CommitCount > 0 {
		fmt.Fprintf(out, "Maturity: %d commits, last %s", r.Maturity.CommitCount, r.Maturity.LastCommitISO8601)
		if r.Maturity.HasCI {
			fmt.Fprintf(out, ", CI: %s", r.Maturity.CIPlatform)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "Maturity: (no git history)")
	}
	if len(r.Languages) > 0 {
		fmt.Fprintln(out, "Languages by bytes:")
		for _, l := range r.Languages {
			fmt.Fprintf(out, "  %-12s  %8d bytes  %5d files  %.0f%%\n",
				l.Language, l.Bytes, l.Files, l.Confidence*100)
		}
	}
	if r.Ambiguous {
		fmt.Fprintln(out, "Note: top-2 languages within 10% bytes (ambiguous)")
	}
	if len(r.Rationale) > 0 {
		fmt.Fprintln(out, "Rationale:")
		for _, rationale := range r.Rationale {
			fmt.Fprintf(out, "  - %s\n", rationale)
		}
	}
	return nil
}
