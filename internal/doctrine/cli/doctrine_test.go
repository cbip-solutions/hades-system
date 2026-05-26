package cli_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func TestNewRoot_Returns_DoctrineCommand(t *testing.T) {
	root := cli.NewRoot()
	if root.Use != "doctrine" {
		t.Fatalf("want Use=\"doctrine\", got %q", root.Use)
	}
	wantGroups := map[string]bool{
		"read":      false,
		"write":     false,
		"amendment": false,
		"debug":     false,
	}
	for _, g := range root.Groups() {
		if _, ok := wantGroups[g.ID]; !ok {
			t.Errorf("unexpected group %q (allowed: read/write/amendment/debug)", g.ID)
			continue
		}
		wantGroups[g.ID] = true
	}
	for id, found := range wantGroups {
		if !found {
			t.Errorf("missing cobra.Group id=%q", id)
		}
	}
}

func TestNewRoot_HelpTextSpanish(t *testing.T) {
	root := cli.NewRoot()
	short := root.Short
	for _, esWord := range []string{"doctrina", "Doctrina"} {
		if strings.Contains(short, esWord) {
			return
		}
	}
	t.Fatalf("root.Short = %q does not contain Spanish keyword (CLAUDE.md §6.6)", short)
}

func TestNewRoot_UniversalFlagsAttached(t *testing.T) {
	root := cli.NewRoot()
	for _, name := range []string{"json", "quiet", "verbose", "since", "limit", "filter", "format"} {
		if root.PersistentFlags().Lookup(name) == nil && root.Flags().Lookup(name) == nil {
			t.Errorf("universal flag --%s not attached on doctrine root", name)
		}
	}
}

func TestNewRoot_AmendmentGroupRegistration(t *testing.T) {
	root := cli.NewRoot()
	allow := map[string]struct{}{
		"propose-list": {},
		"ack":          {},
		"deny":         {},
		"revert":       {},
		"propose":      {},
	}
	count := 0
	for _, sub := range root.Commands() {
		if sub.GroupID != "amendment" {
			continue
		}
		leaf := strings.Fields(sub.Use)[0]
		if _, ok := allow[leaf]; !ok {
			t.Errorf("unexpected amendment leaf %q (allowed: propose-list, ack, deny, revert, propose)", leaf)
		}
		count++
	}
	if count == 0 {
		t.Errorf("expected ≥1 amendment leaf after Phase K-1..K-4 land; got 0")
	}
}

func TestNewRoot_ReadWriteDebugGroupsHaveLeaves(t *testing.T) {
	root := cli.NewRoot()
	counts := map[string]int{"read": 0, "write": 0, "debug": 0}
	for _, sub := range root.Commands() {
		if c, ok := counts[sub.GroupID]; ok {
			counts[sub.GroupID] = c + 1
		}
	}
	if counts["read"] < 6 {
		t.Errorf("read group: want ≥6 leaves, got %d", counts["read"])
	}
	if counts["write"] < 4 {
		t.Errorf("write group: want ≥4 leaves, got %d", counts["write"])
	}
	if counts["debug"] < 1 {
		t.Errorf("debug group: want ≥1 leaf, got %d", counts["debug"])
	}
}

func TestNewRoot_ReachableFromZenRoot(t *testing.T) {
	root := cli.NewRoot()
	if root.GroupID != "" {
		t.Errorf("root.GroupID should be empty, got %q", root.GroupID)
	}
	parent := &cobra.Command{Use: "zen"}
	parent.AddCommand(root)
	got := parent.Commands()
	if len(got) != 1 || got[0].Use != "doctrine" {
		t.Fatalf("attach to parent failed: %+v", got)
	}
}
