package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRoot_AllPhase4NamespacesRegistered(t *testing.T) {
	root := NewRootCmd()
	want := []string{"workforce", "research", "budget", "audit", "ssh-exec", "doctrine", "doctor"}
	for _, w := range want {
		c, _, err := root.Find([]string{w})
		if err != nil || c == nil {
			t.Errorf("namespace %q not registered: err=%v", w, err)
		}
	}
}

func countLeaves(c *cobra.Command) int {
	if len(c.Commands()) == 0 {
		return 1
	}
	n := 0
	for _, sub := range c.Commands() {
		n += countLeaves(sub)
	}
	return n
}

func TestRoot_Phase4SubcommandCounts(t *testing.T) {
	root := NewRootCmd()
	tests := []struct {
		ns        string
		minLeaves int
	}{
		{"workforce", 8},
		{"research", 6},
		{"budget", 8},
		{"audit", 5},
		{"ssh-exec", 5},
		{"doctrine", 7},
		{"doctor", 8},
	}
	for _, tc := range tests {
		c, _, err := root.Find([]string{tc.ns})
		if err != nil || c == nil {
			t.Errorf("namespace %q not found", tc.ns)
			continue
		}
		got := countLeaves(c)
		if got < tc.minLeaves {
			t.Errorf("namespace %q: got %d leaves, want >= %d", tc.ns, got, tc.minLeaves)
		}
	}
}

func TestRoot_TotalLeavesAcrossPhase4(t *testing.T) {
	root := NewRootCmd()
	namespaces := []string{"workforce", "research", "budget", "audit", "ssh-exec", "doctrine", "doctor"}
	total := 0
	for _, ns := range namespaces {
		c, _, err := root.Find([]string{ns})
		if err == nil && c != nil {
			total += countLeaves(c)
		}
	}
	if total < 50 {
		t.Errorf("Plan 4 Phase N leaf count: got %d, want >= 50 (spec §6.1 target=52)", total)
	}
}

func TestRoot_UniversalFlagsAttached(t *testing.T) {
	root := NewRootCmd()
	for _, ns := range []string{"workforce", "research", "budget", "audit", "ssh-exec", "doctrine"} {
		c, _, err := root.Find([]string{ns})
		if err != nil || c == nil {
			t.Errorf("namespace %q not found", ns)
			continue
		}
		for _, f := range []string{"json", "quiet", "verbose", "since", "limit", "filter", "format"} {
			if c.PersistentFlags().Lookup(f) == nil {
				t.Errorf("namespace %q missing --%s", ns, f)
			}
		}
	}
}

func TestRoot_HelpResponds(t *testing.T) {
	for _, ns := range []string{"workforce", "research", "budget", "audit", "ssh-exec", "doctrine", "doctor"} {
		root := NewRootCmd()
		root.SetArgs([]string{ns, "--help"})

		if err := root.Execute(); err != nil {
			t.Errorf("zen %s --help: %v", ns, err)
		}
	}
}
