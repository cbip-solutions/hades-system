package templates

import (
	"strings"
	"testing"
)

func TestADRProposalEmbedded(t *testing.T) {
	if ADRProposal == "" {
		t.Fatal("ADRProposal is empty — //go:embed failed")
	}
	for _, s := range []string{"# ADR", "## Context", "## Decision", "## Consequences", "## Evidence"} {
		if !strings.Contains(ADRProposal, s) {
			t.Errorf("template missing section %q", s)
		}
	}
}
