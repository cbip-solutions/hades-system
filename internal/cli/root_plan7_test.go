package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootMountsAllPlan7Commands(t *testing.T) {
	root := NewRootCmd()
	want := []string{
		"projects", "project", "attach", "sessions", "layout",
		"schedule", "inbox", "quiet", "recap", "knowledge",
	}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("root command %q not mounted; check AddCommand wiring in root.go", w)
		}
	}
}

func TestRootMountsPlan19CaronteVerbs(t *testing.T) {
	root := NewRootCmd()
	want := []string{"why", "risk", "cochange", "impl"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("root command %q not mounted; check AddCommand wiring in root.go (Plan 19 Phase K Task K-7)", w)
		}
	}
}

// TestRootPlan7CommandsHelpExitsZero verifies `--help` works on each
// from Use/Short/Long fields wired in ; Task L-1 just verifies
// the wiring did not leave any constructor returning nil or unparented.
//
// Each case is a sub-test so a single drift surfaces with the exact
// argv that failed (cobra error messages alone do not include the
// invocation context).
func TestRootPlan7CommandsHelpExitsZero(t *testing.T) {
	cases := [][]string{
		{"projects", "ls", "--help"},
		{"project", "doctor", "--help"},
		{"project", "archive", "--help"},
		{"project", "rm", "--help"},
		{"project", "priority", "--help"},
		{"attach", "--help"},
		{"sessions", "ls", "--help"},
		{"layout", "repaint", "--help"},
		{"schedule", "routine", "--help"},
		{"schedule", "task", "--help"},
		{"schedule", "loop", "--help"},
		{"schedule", "history", "--help"},
		{"schedule", "queue", "--help"},
		{"inbox", "--help"},
		{"inbox", "ack", "--help"},
		{"inbox", "snooze", "--help"},
		{"quiet", "--help"},
		{"day", "--help"},
		{"recap", "--help"},
		{"knowledge", "query", "--help"},
		{"knowledge", "reindex", "--help"},
		{"knowledge", "stats", "--help"},
		{"doctor", "knowledge", "--help"},
		{"doctor", "scheduler", "--help"},
		{"doctor", "inbox", "--help"},
		{"doctor", "tmux", "--help"},
	}
	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			root := NewRootCmd()
			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(errOut)
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute %v: err=%v stderr=%q", args, err, errOut.String())
			}
			if out.Len() == 0 && errOut.Len() == 0 {
				t.Errorf("Execute %v: produced no output", args)
			}

			combined := out.String() + errOut.String()
			if !strings.Contains(combined, "Usage:") {
				t.Errorf("Execute %v: help text missing 'Usage:' marker; got %q", args, combined)
			}
		})
	}
}
