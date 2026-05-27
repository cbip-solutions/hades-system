// Package cli — reason_flag_p9_test.go.
//
// Cross-cutting compliance test for invariant:
// every operator-gated transition MUST require a non-empty --reason.
//
// The 8 gated leaves are tested against 2 rejection paths each:
// 1. Missing flag entirely (cobra MarkFlagRequired catches it).
// 2. Empty or whitespace-only string (RunE non-empty check catches it).
//
// Total 8 leaves × 2 inputs = 16 sub-cases.
// If any leaf returns err == nil, invariant is violated at the CLI layer.
//
// NOTE(plan-15): filename uses _p9 (not _plan9) to avoid Go's GOOS=plan9 build
// constraint pattern that excludes *_plan9.go on darwin/linux.
package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

// TestInvZen146_AllGatedLeavesRequireNonEmptyReason iterates over the 8
// operator-gated leaves defined in invariant (spec §7.2). Each leaf fires
// twice — once with no --reason flag and once with a whitespace-only value.
// The mock daemon catches any request that leaks through; if reached, the test
// fails immediately because the validation MUST happen before HTTP dispatch.
func TestInvZen146_AllGatedLeavesRequireNonEmptyReason(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("inv-zen-146 violation: gated leaf reached daemon despite missing/empty --reason; path=%s", r.URL.Path)
		http.Error(w, "should not reach daemon", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	type leafCase struct {
		name       string
		root       func() *cobra.Command
		argsNoFlag []string
		argsEmpty  []string
	}

	cases := []leafCase{
		{
			name:       "audit-chain_checkpoint",
			root:       NewAuditChainCmd,
			argsNoFlag: []string{"checkpoint"},
			argsEmpty:  []string{"checkpoint", "--reason", "   "},
		},
		{
			name:       "audit-chain_witness_rotate",
			root:       NewAuditChainCmd,
			argsNoFlag: []string{"witness", "rotate"},
			argsEmpty:  []string{"witness", "rotate", "--reason", ""},
		},
		{
			name:       "knowledge-p9_promote",
			root:       NewKnowledge9Cmd,
			argsNoFlag: []string{"promote", "internal-platform-x/x"},
			argsEmpty:  []string{"promote", "internal-platform-x/x", "--reason", "   "},
		},
		{
			name:       "knowledge-p9_unpromote",
			root:       NewKnowledge9Cmd,
			argsNoFlag: []string{"unpromote", "x"},
			argsEmpty:  []string{"unpromote", "x", "--reason", ""},
		},
		{
			name:       "adr_accept",
			root:       NewAdrCmd,
			argsNoFlag: []string{"accept", "ADR-0070"},
			argsEmpty:  []string{"accept", "ADR-0070", "--reason", "   "},
		},
		{
			name:       "adr_reject",
			root:       NewAdrCmd,
			argsNoFlag: []string{"reject", "ADR-0070"},
			argsEmpty:  []string{"reject", "ADR-0070", "--reason", "   "},
		},
		{
			name:       "adr_supersede",
			root:       NewAdrCmd,
			argsNoFlag: []string{"supersede", "ADR-0070", "ADR-0080"},
			argsEmpty:  []string{"supersede", "ADR-0070", "ADR-0080", "--reason", "   "},
		},
		{
			name:       "state_pin",
			root:       NewStateCmd,
			argsNoFlag: []string{"pin", "field", "value"},
			argsEmpty:  []string{"pin", "field", "value", "--reason", "   "},
		},
	}

	runReject := func(t *testing.T, name string, cmd *cobra.Command, args []string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader("y\n"))
		err := cmd.Execute()
		if err == nil {
			t.Errorf("inv-zen-146 violated: %s did NOT reject args %v (expected error)", name, args)
		}
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"_no_flag", func(t *testing.T) {
			runReject(t, tc.name, tc.root(), tc.argsNoFlag)
		})
		t.Run(tc.name+"_empty_or_whitespace", func(t *testing.T) {
			runReject(t, tc.name, tc.root(), tc.argsEmpty)
		})
	}
}
