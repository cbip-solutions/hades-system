package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// TestClassifyCapaFirewallErrorRendersCrossProjectHint sister-pins policy:
// the rendered hint MUST contain the actionable substrings the operator can grep.
func TestClassifyCapaFirewallErrorRendersCrossProjectHint(t *testing.T) {
	err := fmt.Errorf("wrap: %w", store.ErrCrossProjectDenied)
	out := classifyCapaFirewallError(err, "contract")
	if out == nil {
		t.Fatal("classifyCapaFirewallError returned nil; want hint")
	}
	if !errors.Is(out, ErrRecoverable) {
		t.Errorf("hint does not wrap ErrRecoverable: %v", out)
	}
	wantSubstrs := []string{"cross-project access denied", "workspace privacy policy is locked", "zen workspace policy set"}
	for _, s := range wantSubstrs {
		if !strings.Contains(out.Error(), s) {
			t.Errorf("hint missing substring %q in %q", s, out.Error())
		}
	}
}

func TestClassifyCapaFirewallErrorRendersUnauthorizedHint(t *testing.T) {
	err := fmt.Errorf("wrap: %w", store.ErrUnauthorizedProject)
	out := classifyCapaFirewallError(err, "workspace")
	if out == nil {
		t.Fatal("classifyCapaFirewallError returned nil; want hint")
	}
	wantSubstrs := []string{"project not on workspace roster", "zen workspace members", "zen workspace link"}
	for _, s := range wantSubstrs {
		if !strings.Contains(out.Error(), s) {
			t.Errorf("hint missing substring %q in %q", s, out.Error())
		}
	}
}

func TestClassifyCapaFirewallErrorFallsThroughTo503(t *testing.T) {
	err := fmt.Errorf("daemon unreachable")
	out := classifyCapaFirewallError(err, "contract")
	if out == nil {
		t.Fatal("classifyCapaFirewallError returned nil; want fall-through")
	}

	if !strings.Contains(out.Error(), "daemon unreachable") {
		t.Errorf("fall-through dropped original error: %v", out)
	}
}

func TestClassifyCapaFirewallErrorIsExportedForReuse(t *testing.T) {

	var _ func(err error, op string) error = classifyCapaFirewallError
}

// TestAllPlan20VerbsRegisterFormatFlag sister-pins policy: every
// CLI verb MUST register `--format` (NOT `-o` / `-O`). Iterates the 12
// verb constructors + asserts each cobra.Command exposes a flag named
// `format`. Guards against a regressive refactor that adds a POSIX `-o`
// shorthand on one verb but forgets the others (uniform agent surface).
func TestAllPlan20VerbsRegisterFormatFlag(t *testing.T) {
	type verbCase struct {
		name string
		cmd  *cobra.Command
	}

	cases := []verbCase{
		{"zen contract", NewContractCmd(func(*cobra.Command) ContractClient { return &fakeContractClient{} })},
		{"zen contract validate", NewContractValidateCmd(func(*cobra.Command) ContractClient { return &fakeContractClient{} })},
		{"zen contract why", NewContractWhyCmd(func(*cobra.Command) ContractClient { return &fakeContractClient{} })},
		{"zen workspace init", NewWorkspaceInitCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace list", NewWorkspaceListCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace members", NewWorkspaceMembersCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace link", NewWorkspaceLinkCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace remove", NewWorkspaceRemoveCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace policy get", NewWorkspacePolicyGetCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen workspace policy set", NewWorkspacePolicySetCmd(func(*cobra.Command) WorkspaceClient { return &fakeWorkspaceClient{} })},
		{"zen federation health", NewFederationHealthCmd(func(*cobra.Command) FederationClient { return &fakeFederationClient{} })},
		{"zen api-impact", NewAPIImpactCmd(func(*cobra.Command) FederationClient { return &fakeFederationClient{} })},
	}
	for _, c := range cases {
		if f := c.cmd.Flags().Lookup("format"); f == nil {
			t.Errorf("%s: missing --format flag (DECISION 1)", c.name)
		}

		if f := c.cmd.Flags().ShorthandLookup("o"); f != nil {
			t.Errorf("%s: registered -o shorthand; DECISION 1 forbids until cross-CLI rollout", c.name)
		}
		if f := c.cmd.Flags().ShorthandLookup("O"); f != nil {
			t.Errorf("%s: registered -O shorthand; DECISION 1 forbids until cross-CLI rollout", c.name)
		}
	}
}
