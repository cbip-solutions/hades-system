// SPDX-License-Identifier: MIT
// specs_show.go — Task F-5 subcommand `zen specs show <id>`.
//
// Pure filesystem read of openspec/specs/<id>.md — no daemon call.
// Renders the full file content. --format text|md is a no-op alias
// today (md content is the only on-disk shape); reserved for future
// adapters that render markdown to a different sink (HTML, ANSI, etc.).
//
// Read-only boundary per spec §0.2.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type SpecsShowFlags struct {
	Format string
}

func RunSpecsShow(specsDir, id, format string, w io.Writer) error {
	if format == "" {
		format = "text"
	}
	if !validSpecsFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs show: --format %q: must be one of text|json|md", format))
	}
	if format == "json" {

		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs show: --format=json not supported (use --format=text or md)"))
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs show: <id> is required"))
	}
	if err := validateSpecID(id); err != nil {
		return err
	}

	path := filepath.Join(specsDir, id+".md")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs show: spec %q not found in %s", id, specsDir))
	}
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("specs show: read %s: %w", path, err))
	}
	_, werr := w.Write(data)
	return werr
}

func validateSpecID(id string) error {
	if strings.ContainsAny(id, `/\`) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs: id %q must not contain path separators", id))
	}
	if strings.Contains(id, "..") {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs: id %q must not contain '..'", id))
	}
	if filepath.IsAbs(id) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("specs: id %q must not be an absolute path", id))
	}
	return nil
}

func newSpecsShowCmd(getDir specsDirResolver) *cobra.Command {
	flags := SpecsShowFlags{}
	cmd := &cobra.Command{
		Use:   "show <spec-id>",
		Short: "Render full spec content from openspec/specs/<id>.md",
		Long: `Read openspec/specs/<id>.md and copy the content to stdout.

Pure filesystem read — no daemon call. Reserved formats: text (default)
and md (alias today). Missing spec returns exit 1 (operator-recoverable).`,
		Example: `  zen specs show adr-0001
  zen specs show design-v1 --format md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunSpecsShow(getDir(cmd), args[0], flags.Format, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "Output format: text|md")
	return cmd
}
