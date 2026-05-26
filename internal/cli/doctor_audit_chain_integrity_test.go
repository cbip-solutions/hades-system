package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestDoctorAuditChainIntegrityRegisteredAsSubcommand(t *testing.T) {
	root := NewDoctorCmd()
	sub := findDoctorSubcommand(root, "audit.chain-integrity")
	if sub == nil {
		t.Fatal("audit.chain-integrity subcommand missing — wiring regression")
	}
	if sub.Short == "" {
		t.Error("audit.chain-integrity Short docstring missing")
	}
	if sub.RunE == nil {
		t.Error("audit.chain-integrity RunE not wired")
	}
}

func TestDoctorAuditChainIntegrityRunEEmitsRow(t *testing.T) {
	root := NewDoctorCmd()
	sub := findDoctorSubcommand(root, "audit.chain-integrity")
	if sub == nil {
		t.Fatal("audit.chain-integrity subcommand missing")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "Audit chain integrity") {
		t.Errorf("expected `Audit chain integrity` section header, got %q", got)
	}
}

func TestRunAuditChainIntegrityChecksEmptyProjectsReturnsWarn(t *testing.T) {
	c := &client.Client{}
	results := runAuditChainIntegrityChecks(context.Background(), c)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (warn fallback)", len(results))
	}
	if results[0].Status != "warn" {
		t.Errorf("status = %q, want warn", results[0].Status)
	}
	if !strings.Contains(results[0].Hint, "verify-chain") {
		t.Errorf("hint should mention verify-chain, got %q", results[0].Hint)
	}
}
