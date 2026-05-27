// Package cli — help_v0_7_0_test.go.
//
// Golden audit: every cobra subcommand MUST ship man-page-quality
// help text per spec §6 ("operator UX") and the project doctrine
// "build the final product, not the stages". This test walks the
// command tree under `zen` and asserts that for every path the
// `Long` description is multi-paragraph (≥80 chars after trim) and the
// `Example` block contains at least one runnable `zen <path>...` line.
//
// Why three checks (presence + minimum length + binary-name marker):
// - presence catches accidental drops where a phase-writer left the
// field empty.
// - minimum length (80 chars) catches the trivial "Long is just a
// copy of Short" anti-pattern that would technically pass a non-empty
// check.
// - "zen " marker catches Examples that show flags-only or pseudocode
// instead of an actual paste-able invocation.
//
// CI gating: this file runs as part of `go test./internal/cli/...`,
// so any future phase that adds a subcommand without filling
// Long+Example breaks this test before merge. Future plans (8+) extend
// the audit set by appending paths to `plan7HelpAuditPaths`.
//
// Earlier subcommand families are
// intentionally excluded — they are policed by their own release-time
// tests; this file is the equivalent for the audit/inbox/scheduler
// substrate added later.
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type helpAuditEntry struct {
	path    string
	long    string
	example string
}

func walkHelpAudit(c *cobra.Command, parent string, sink *[]helpAuditEntry) {
	path := c.Name()
	if parent != "" {
		path = parent + " " + path
	}
	*sink = append(*sink, helpAuditEntry{
		path:    path,
		long:    c.Long,
		example: c.Example,
	})
	for _, sub := range c.Commands() {
		if sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		walkHelpAudit(sub, path, sink)
	}
}

var plan7HelpAuditPaths = map[string]struct{}{
	"zen projects":          {},
	"zen projects ls":       {},
	"zen project":           {},
	"zen project doctor":    {},
	"zen project archive":   {},
	"zen project rm":        {},
	"zen project priority":  {},
	"zen attach":            {},
	"zen sessions":          {},
	"zen sessions ls":       {},
	"zen layout":            {},
	"zen layout repaint":    {},
	"zen schedule":          {},
	"zen schedule routine":  {},
	"zen schedule task":     {},
	"zen schedule loop":     {},
	"zen schedule history":  {},
	"zen schedule queue":    {},
	"zen inbox":             {},
	"zen inbox ack":         {},
	"zen inbox snooze":      {},
	"zen quiet":             {},
	"zen day":               {},
	"zen recap":             {},
	"zen knowledge":         {},
	"zen knowledge query":   {},
	"zen knowledge reindex": {},
	"zen knowledge stats":   {},
	"zen doctor knowledge":  {},
	"zen doctor scheduler":  {},
	"zen doctor inbox":      {},
	"zen doctor tmux":       {},
}

const helpAuditMinLongLen = 80

// TestPlan7CommandsHaveLongAndExample is the gate. Failures list every
// "all green" — if even one fails, the release MUST add the missing
// help text before tagging v0.7.0.
func TestPlan7CommandsHaveLongAndExample(t *testing.T) {
	root := NewRootCmd()
	var entries []helpAuditEntry
	walkHelpAudit(root, "", &entries)

	got := map[string]helpAuditEntry{}
	for _, e := range entries {
		got[e.path] = e
	}

	for path := range plan7HelpAuditPaths {
		e, ok := got[path]
		if !ok {
			t.Errorf("path %q not present in command tree (check root.go AddCommand wiring + sub-command mounts)", path)
			continue
		}
		if strings.TrimSpace(e.long) == "" {
			t.Errorf("%s: Long help text empty; spec §6 requires multi-paragraph description", path)
			continue
		}
		if len(strings.TrimSpace(e.long)) < helpAuditMinLongLen {
			t.Errorf("%s: Long help text shorter than %d chars (got %d); add a paragraph describing semantics + observable side-effects",
				path, helpAuditMinLongLen, len(strings.TrimSpace(e.long)))
		}
		if strings.TrimSpace(e.example) == "" {
			t.Errorf("%s: Example block empty; spec §6 requires at least one real-world invocation", path)
			continue
		}
		if !strings.Contains(e.example, "zen ") {
			t.Errorf("%s: Example block does not contain `zen ` invocation; should look like `zen %s ...`",
				path, strings.TrimPrefix(path, "zen "))
		}
	}
}
