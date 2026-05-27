// root_p7_c12_test.go — Task C-12 wiring tests.
//
// Mirrors the C-2.5 fix (NewMergeCmd was orphan-shipped):
// the C-12 commands MUST be registered on the root via NewRootCmd so
// `zen attach`, `zen sessions`, `zen layout` are real reachable paths.
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func findCobraChild(xs []*cobra.Command, name string) *cobra.Command {
	for _, x := range xs {
		if x.Name() == name {
			return x
		}
	}
	return nil
}

func TestRootHasAttachCmd(t *testing.T) {
	root := NewRootCmd()
	c := findCobraChild(root.Commands(), "attach")
	if c == nil {
		t.Fatal("`attach` not registered on root (C-12)")
	}
	if c.Flags().Lookup("window") == nil {
		t.Error("attach subcommand missing --window flag (still a stub?)")
	}
}

func TestRootHasSessionsCmd(t *testing.T) {
	root := NewRootCmd()
	sessions := findCobraChild(root.Commands(), "sessions")
	if sessions == nil {
		t.Fatal("`sessions` not registered on root (C-12)")
	}
	if findCobraChild(sessions.Commands(), "ls") == nil {
		t.Error("`sessions` registered without `ls` subcommand (C-12)")
	}
}

func TestRootHasLayoutCmd(t *testing.T) {
	root := NewRootCmd()
	layout := findCobraChild(root.Commands(), "layout")
	if layout == nil {
		t.Fatal("`layout` not registered on root (C-12)")
	}
	if findCobraChild(layout.Commands(), "repaint") == nil {
		t.Error("`layout` registered without `repaint` subcommand (C-12)")
	}
}

func TestRootC12CmdsHelpUsageStrings(t *testing.T) {
	root := NewRootCmd()
	for _, name := range []string{"attach", "sessions", "layout"} {
		c := findCobraChild(root.Commands(), name)
		if c == nil {
			t.Errorf("%q not on root", name)
			continue
		}
		if !strings.Contains(c.Use, name) {
			t.Errorf("%q Use=%q, expected substring %q", name, c.Use, name)
		}
	}
}
