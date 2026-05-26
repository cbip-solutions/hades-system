package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestVerifyDocs_HappyPath(t *testing.T) {
	x := newFakeExec()
	x.setOK("zen", "doctor", "verify-docs")
	c := checks.NewVerifyDocs(checks.Deps{Exec: x})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckVerifyDocs {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestVerifyDocs_NonZeroExit_Fails(t *testing.T) {
	x := newFakeExec()
	x.setFail("zen", 1, "broken link in plan-3", "doctor", "verify-docs")
	c := checks.NewVerifyDocs(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+reason; got %v %q", status, reason)
	}
}

func TestVerifyDocs_NonZeroExit_EmptyStdout_FallbackReason(t *testing.T) {
	x := newFakeExec()
	x.setFail("zen", 2, "", "doctor", "verify-docs")
	c := checks.NewVerifyDocs(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+fallback reason; got %v %q", status, reason)
	}
}

func TestVerifyDocs_ExecError_Fails(t *testing.T) {
	x := newFakeExec()
	x.setErr("zen", errors.New("no such file"), "doctor", "verify-docs")
	c := checks.NewVerifyDocs(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("exec err: want fail+reason; got %v %q", status, reason)
	}
}

func TestVerifyDocs_NotConfigured_Skips(t *testing.T) {
	c := checks.NewVerifyDocs(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
