// SPDX-License-Identifier: MIT
// Package cli — error_render.go.
//
// Render is the SINGLE error-rendering entry point for the hades + hades
// CLI binaries. Every cobra RunE boundary routes through this function
// via the cobra integration in cmd/hades/main.go + cmd/hades/main.go.
//
// Output format:
//
// HADES <short title from catalog> (#c41e3a crimson)
// <gray body template, sprintf'd with context vars> (#999 gray)
// → <green recovery hint from catalog> (#10b981 green)
//
// invariant boundary: Render is PURE. It does NOT make network/IO
// calls; it only formats. The catalog lookup is in-process (
// internal/errors package).
//
// invariant boundary: Render's output is HADES-branded by construction
// — the catalog entries supply HADES-branded titles + bodies
// + recovery hints; Render only reformats them.
//
// Defense in depth: uncoded errors (raw fmt.Errorf
// or panics caught by recover() in main.go) are rendered via the
// reserved "internal-uncaught" code with the cause embedded in the body.
package cli

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cbip-solutions/hades-system/internal/errors"
)

type RenderOpts struct {
	Verbose bool

	NoColor bool

	Stream io.Writer
}

const (
	ansiCrimson = "\x1b[38;2;196;30;58m"

	ansiGray = "\x1b[38;2;153;153;153m"

	ansiGreen = "\x1b[38;2;16;185;129m"

	ansiDivider = "\x1b[38;2;85;85;85m"

	ansiReset = "\x1b[0m"
)

func Render(err error, opts RenderOpts) string {
	if err == nil {
		return ""
	}

	useColor := shouldUseColor(opts)

	var b strings.Builder

	if coded, ok := err.(*errors.CodedError); ok {
		b.WriteString(renderCoded(coded, useColor))
	} else {

		b.WriteString(renderUncoded(err, useColor))
	}

	if opts.Verbose {
		b.WriteString("\n")
		if useColor {
			b.WriteString(ansiDivider)
		}
		b.WriteString(strings.Repeat("─", 60))
		if useColor {
			b.WriteString(ansiReset)
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%+v", err))
	}

	return b.String()
}

func shouldUseColor(opts RenderOpts) bool {
	if opts.NoColor {
		return false
	}
	if opts.Stream == nil {
		return false
	}
	type fdReader interface{ Fd() uintptr }
	fr, ok := opts.Stream.(fdReader)
	if !ok {
		return false
	}
	return term.IsTerminal(int(fr.Fd()))
}

// renderCoded formats a *CodedError into the three-line HADES block.
// useColor controls whether ANSI sequences are emitted.
//
// Body template substitution: simple {{key}} replacement from
// coded.Context map. We do NOT use text/template to avoid the import
// surface; the substitution is intentionally simple (keys are flat).
func renderCoded(coded *errors.CodedError, useColor bool) string {
	entry := errors.Lookup(coded.Code)
	if entry == nil {

		return renderCatalogMiss(coded.Code, useColor)
	}

	body := entry.BodyTemplate
	for k, v := range coded.Context {
		body = strings.ReplaceAll(body, "{{"+k+"}}", v)
	}

	var b strings.Builder

	if useColor {
		b.WriteString(ansiCrimson)
	}
	b.WriteString("HADES:")
	if useColor {
		b.WriteString(ansiReset)
	}
	b.WriteString(" ")
	b.WriteString(entry.Title)
	b.WriteString("\n")

	b.WriteString("  ")
	if useColor {
		b.WriteString(ansiGray)
	}
	b.WriteString(body)
	if useColor {
		b.WriteString(ansiReset)
	}
	b.WriteString("\n")

	b.WriteString("  ")
	if useColor {
		b.WriteString(ansiGreen)
	}
	b.WriteString("→ ")
	b.WriteString(entry.RecoveryHint)
	if useColor {
		b.WriteString(ansiReset)
	}

	return b.String()
}

func renderUncoded(err error, useColor bool) string {
	wrapped := errors.Wrap(
		errors.Code("internal-uncaught"),
		err,
	)

	base := renderCoded(wrapped, useColor)

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n  ")
	if useColor {
		b.WriteString(ansiGray)
	}
	b.WriteString("cause: ")
	b.WriteString(err.Error())
	if useColor {
		b.WriteString(ansiReset)
	}
	return b.String()
}

func renderCatalogMiss(code errors.Code, useColor bool) string {
	var b strings.Builder
	if useColor {
		b.WriteString(ansiCrimson)
	}
	b.WriteString("HADES:")
	if useColor {
		b.WriteString(ansiReset)
	}
	b.WriteString(fmt.Sprintf(" catalog miss for code %q\n  ", code))
	if useColor {
		b.WriteString(ansiGray)
	}
	b.WriteString("(this is a bug — please report)")
	if useColor {
		b.WriteString(ansiReset)
	}
	b.WriteString("\n  → report at: https://github.com/cbip-solutions/hades-system/issues/new")
	return b.String()
}

// ExitCodeFromError maps an error to the process exit code per spec §Q6.
// Used by cmd/hades/main.go and cmd/hades/main.go after catching
// cobra's Execute return value.
//
// Mapping
// - nil → 0 (success)
// - severity fatal → 2 (unrecoverable; daemon unreachable; panic; etc.)
// - severity error → 1 (operator-recoverable; bad input; daemon 4xx)
// - severity warn → 0 (warning printed but process succeeded)
// - severity info → 0 (informational)
// - uncoded error → 2 (defense-in-depth: treat unknowns as fatal)
//
// stderrors.As unwraps the chain so wrapped *CodedError still maps
// correctly even when further wrapped with fmt.Errorf.
func ExitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var coded *errors.CodedError
	if !stderrors.As(err, &coded) {

		return 2
	}
	entry := errors.Lookup(coded.Code)
	if entry == nil {

		return 2
	}
	switch entry.Severity {
	case errors.SeverityFatal:
		return 2
	case errors.SeverityError:
		return 1
	case errors.SeverityWarn:
		return 0
	case errors.SeverityInfo:
		return 0
	default:

		return 2
	}
}

func AttachVerboseFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool("verbose", false, "print full traceback after the HADES error block")
}

func AttachNoColorFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool("no-color", false, "suppress ANSI color codes in error output")
}

func RenderOptsFromCmd(cmd *cobra.Command) RenderOpts {
	verbose, _ := cmd.PersistentFlags().GetBool("verbose")
	noColor, _ := cmd.PersistentFlags().GetBool("no-color")
	return RenderOpts{
		Verbose: verbose,
		NoColor: noColor,
		Stream:  os.Stderr,
	}
}
