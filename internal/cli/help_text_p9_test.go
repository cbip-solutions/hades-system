package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func p9CommandRoots() []*cobra.Command {
	return []*cobra.Command{
		NewAuditChainCmd(),
		NewKnowledge9Cmd(),
		NewAdrCmd(),
		NewStateCmd(),
	}
}

func p9ResearchLeaves(t *testing.T) []*cobra.Command {
	t.Helper()
	research := NewResearchCmd()
	var leaves []*cobra.Command
	for _, sub := range research.Commands() {
		if sub.Name() == "history" {
			leaves = append(leaves, sub)
		}
		if sub.Name() == "cache" {
			for _, cacheSub := range sub.Commands() {
				if cacheSub.Name() == "invalidate" || cacheSub.Name() == "ls" {
					leaves = append(leaves, cacheSub)
				}
			}
		}
	}
	return leaves
}

func walkP9Cobra(t *testing.T, c *cobra.Command, f func(*cobra.Command, string)) {
	t.Helper()
	walkP9Node(t, c, c.Name(), f)
}

func walkP9Node(t *testing.T, c *cobra.Command, path string, f func(*cobra.Command, string)) {
	t.Helper()
	f(c, path)
	for _, child := range c.Commands() {
		walkP9Node(t, child, path+" "+child.Name(), f)
	}
}

func TestPlan9HelpText_AllLeavesPopulated(t *testing.T) {
	checkNode := func(c *cobra.Command, path string) {
		if c.Use == "" {
			t.Errorf("%s: Use is empty", path)
		}
		if c.Short == "" {
			t.Errorf("%s: Short is empty", path)
		}

		if !c.HasSubCommands() {
			if c.Long == "" && c.Example == "" {
				t.Errorf("%s: leaf command must have Long or Example populated (operator help quality)", path)
			}
		}
	}

	for _, root := range p9CommandRoots() {
		walkP9Cobra(t, root, checkNode)
	}

	for _, leaf := range p9ResearchLeaves(t) {
		checkNode(leaf, "research "+leaf.Name())
	}
}

func TestPlan9HelpText_ReasonFlagsHaveInvZen146Label(t *testing.T) {
	checkReasonFlag := func(c *cobra.Command, path string) {
		f := c.Flags().Lookup("reason")
		if f == nil {
			return
		}
		if !strings.Contains(strings.ToLower(f.Usage), "inv-zen-146") {
			t.Errorf("%s --reason flag usage missing inv-zen-146 reference: %q", path, f.Usage)
		}
	}
	for _, root := range p9CommandRoots() {
		walkP9Cobra(t, root, checkReasonFlag)
	}
	for _, leaf := range p9ResearchLeaves(t) {
		checkReasonFlag(leaf, "research "+leaf.Name())
	}
}

func TestPlan9HelpText_NoSpanishLeak(t *testing.T) {

	spanish := []string{
		" no es ", " los ", " las ", " una ", " uno ",
		" usuario ", " antes de ", " después de ",
		" configuración ", " operador ",
	}
	checkSpanish := func(c *cobra.Command, path string) {
		combined := strings.ToLower(
			c.Use + " " + c.Short + " " + c.Long + " " + c.Example,
		)
		for _, phrase := range spanish {
			if strings.Contains(combined, phrase) {
				t.Errorf("%s help text contains Spanish phrase %q (CLAUDE.md §6.7: help text must be English)", path, phrase)
			}
		}
	}
	for _, root := range p9CommandRoots() {
		walkP9Cobra(t, root, checkSpanish)
	}
	for _, leaf := range p9ResearchLeaves(t) {
		checkSpanish(leaf, "research "+leaf.Name())
	}
}
