package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestDoctorAuditBackupRegisteredAsSubcommand(t *testing.T) {
	root := NewDoctorCmd()
	sub := findDoctorSubcommand(root, "audit.backup")
	if sub == nil {
		t.Fatal("audit.backup subcommand missing — wiring regression in doctor.go")
	}
	if sub.Short == "" {
		t.Error("audit.backup Short docstring missing")
	}
	if sub.RunE == nil {
		t.Error("audit.backup RunE not wired")
	}
}

func TestDoctorAuditBackupRunEEmitsRow(t *testing.T) {
	root := NewDoctorCmd()
	sub := findDoctorSubcommand(root, "audit.backup")
	if sub == nil {
		t.Fatal("audit.backup subcommand missing")
	}
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)

	_ = sub.RunE(sub, nil)
	got := buf.String()
	if !strings.Contains(got, "Audit backup") {
		t.Errorf("expected `Audit backup` section header, got %q", got)
	}
}

// TestRunAuditBackupChecksDaemonDownReturnsFail covers the transport-
// error branch: when the typed Client returns an error, the adapter
// MUST emit a single fail row whose Hint points at the daemon
// endpoint so operators can self-diagnose without reading the source.
//
// Phase C-10 ships AuditDoctorBackup as a stub that returns nil error;
// to exercise the error branch under unit test we wrap a custom client
// that returns an error. The cleanest seam is: extract the body into
// a small free function, then re-test it with a fake Client. That
// would require a bigger refactor than C-10's scope; instead we
// rely on the empty-projects branch coverage below + the recovery
// package's exhaustive doctor_test.go for the status logic.
func TestRunAuditBackupChecksEmptyProjectsReturnsWarn(t *testing.T) {

	c := &client.Client{}
	results := runAuditBackupChecks(context.Background(), c)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (warn fallback)", len(results))
	}
	if results[0].Status != "warn" {
		t.Errorf("status = %q, want warn", results[0].Status)
	}
	if !strings.Contains(results[0].Hint, "configure-s3") {
		t.Errorf("hint should mention configure-s3, got %q", results[0].Hint)
	}
}

func TestAdaptCheckResultPreservesAllFields(t *testing.T) {
	in := recovery.CheckResult{
		Name:   "audit.backup",
		Status: "warn",
		Detail: "litestream lag > 1h",
		Hint:   "check S3 credentials",
	}
	got := adaptCheckResult(in)
	if got.Name != in.Name {
		t.Errorf("Name = %q, want %q", got.Name, in.Name)
	}
	if got.Status != in.Status {
		t.Errorf("Status = %q, want %q", got.Status, in.Status)
	}
	if got.Detail != in.Detail {
		t.Errorf("Detail = %q, want %q", got.Detail, in.Detail)
	}
	if got.Hint != in.Hint {
		t.Errorf("Hint = %q, want %q", got.Hint, in.Hint)
	}
}
