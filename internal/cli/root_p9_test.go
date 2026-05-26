package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootMountsAllPlan9Commands(t *testing.T) {
	root := NewRootCmd()
	want := []string{
		"audit-chain",
		"knowledge-p9",
		"adr",
		"state",
	}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("Plan 9 root command %q not mounted", w)
		}
	}
}

func TestRootPlan9HelpExitsZero(t *testing.T) {
	cases := [][]string{
		{"audit-chain", "--help"},
		{"audit-chain", "verify-chain", "--help"},
		{"audit-chain", "history", "--help"},
		{"audit-chain", "recover", "--help"},
		{"audit-chain", "checkpoint", "--help"},
		{"audit-chain", "cold-archive", "ls", "--help"},
		{"audit-chain", "cold-archive", "restore", "--help"},
		{"audit-chain", "configure-s3", "--help"},
		{"audit-chain", "witness", "rotate", "--help"},
		{"audit-chain", "witness", "pubkey", "--help"},

		{"knowledge-p9", "--help"},
		{"knowledge-p9", "query", "--help"},
		{"knowledge-p9", "promote", "--help"},
		{"knowledge-p9", "unpromote", "--help"},
		{"knowledge-p9", "ls", "--help"},
		{"knowledge-p9", "rebuild", "--help"},

		{"adr", "--help"},
		{"adr", "propose", "--help"},
		{"adr", "show", "--help"},
		{"adr", "ls", "--help"},
		{"adr", "graph", "--help"},
		{"adr", "history", "--help"},
		{"adr", "accept", "--help"},
		{"adr", "reject", "--help"},
		{"adr", "supersede", "--help"},
		{"adr", "migrate", "--help"},
		{"adr", "index", "--help"},

		{"research", "history", "--help"},
		{"research", "cache", "invalidate", "--help"},
		{"research", "cache", "ls", "--help"},

		{"state", "--help"},
		{"state", "show", "--help"},
		{"state", "regenerate", "--help"},
		{"state", "verify", "--help"},
		{"state", "pin", "--help"},
		{"state", "history", "--help"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			root := NewRootCmd()
			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(errOut)
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute %v: %v\nstderr=%s", args, err, errOut.String())
			}
			combined := out.String() + errOut.String()
			if !strings.Contains(combined, "Usage:") {
				t.Errorf("Execute %v: help text missing 'Usage:' marker", args)
			}
		})
	}
}
