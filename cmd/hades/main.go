// SPDX-License-Identifier: MIT
// Package main is the entrypoint for the hades CLI.
// All subcommands defined in internal/cli.
//
// Exit codes: the catalog severity drives the exit
// code via cli.ExitCodeFromError. Mapping:
//
// 0 — success / warn / info (cmd.Execute returned nil; or returned
// a *CodedError with Severity warn|info)
// 1 — error: operator-recoverable; bad input; daemon 4xx; etc.
// 2 — fatal: daemon unreachable; transport failure; uncaught error;
// preflight environment missing.
//
// The legacy exit-code-3 path collapses
// to exit 2 (fatal)c. Operator scripts depending on
// the 1/2/3 split must migrate to severity-based handling; see
// docs/operations/hades-entry-point.md "Error UX" section.
//
// Defense-in-depth: a defer-recover() wraps cmd.Execute() so panics
// are rendered via internal-uncaught code with the panic
// value + stack as body (verbose by default). See B-7.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/errors"
)

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = del
			if ins < curr[j] {
				curr[j] = ins
			}
			if sub < curr[j] {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func cobraUnknownSubcmdError(root *cobra.Command, err error) *errors.CodedError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	const prefix = `unknown command "`
	if !strings.HasPrefix(msg, prefix) {
		return nil
	}

	rest := msg[len(prefix):]
	closeQ := strings.IndexByte(rest, '"')
	typed := rest
	if closeQ >= 0 {
		typed = rest[:closeQ]
	}

	// Compute near-miss suggestion using our own levenshtein-based search.
	//
	// We do NOT call root.SuggestionsFor(typed) because cobra only applies the
	// default SuggestionsMinimumDistance=2 inside ExecuteC(); when called
	// directly (as in unit tests, or when error interception happens outside the
	// normal Execute path), SuggestionsMinimumDistance is still 0 — meaning only
	// prefix-matched candidates are returned, and "doctor" (ld=1) is excluded
	// while "doctrine" (ld=3, prefix of "doctr") wins. This makes the result
	// context-dependent (test binary vs production binary differ).
	//
	// Instead, we walk root.Commands() directly with our own levenshtein helper
	// (threshold=2 matching cobra's documented default, plus prefix matching to
	// stay consistent with cobra's algorithm). We then select the candidate with
	// the smallest Levenshtein distance to `typed`, with an alphabetical tiebreak
	// for full determinism across all link contexts.
	const suggestThreshold = 2
	typedLower := strings.ToLower(typed)
	var best string
	bestDist := suggestThreshold + 1
	for _, sub := range root.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		name := sub.Name()
		d := levenshtein(typed, name)
		isPrefix := strings.HasPrefix(strings.ToLower(name), typedLower)
		if d > suggestThreshold && !isPrefix {
			continue
		}
		if best == "" || d < bestDist || (d == bestDist && name < best) {
			best = name
			bestDist = d
		}
	}
	var suggLine string
	if best != "" {
		suggLine = "did you mean `hades " + best + "`? · "
	}

	cleanMsg := msg
	if idx := strings.Index(msg, "\n\nDid you mean"); idx >= 0 {
		cleanMsg = msg[:idx]
	}

	return errors.New(
		errors.Code("cli.unknown-subcommand"),
		fmt.Errorf("%s", cleanMsg),
		map[string]string{
			"suggestion_line": suggLine,
		},
	)
}

func argsHaveFlag(name string) bool {
	flag := "--" + name
	for _, a := range os.Args[1:] {
		if a == flag || a == flag+"=true" || a == "-"+name {
			return true
		}
	}
	return false
}

func main() {
	root := cli.NewRootCmd()

	var panicRendered bool
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			panicErr := fmt.Errorf("panic: %v\n%s", r, stack)
			opts := cli.RenderOpts{Verbose: true, NoColor: false, Stream: os.Stderr}
			fmt.Fprintln(os.Stderr, cli.Render(panicErr, opts))
			panicRendered = true
			os.Exit(2)
		}
		_ = panicRendered // silence unused warning; set by deferred func
	}()

	if v := os.Getenv("HADES_TEST_PANIC"); v != "" {
		panic(v)
	}

	err := root.Execute()
	if err != nil {

		opts := cli.RenderOptsFromCmd(root)
		if !opts.Verbose {
			opts.Verbose = argsHaveFlag("verbose")
		}
		if !opts.NoColor {
			opts.NoColor = argsHaveFlag("no-color")
		}

		if coded := cobraUnknownSubcmdError(root, err); coded != nil {
			err = coded
		}
		fmt.Fprintln(os.Stderr, cli.Render(err, opts))
		os.Exit(cli.ExitCodeFromError(err))
	}
}
