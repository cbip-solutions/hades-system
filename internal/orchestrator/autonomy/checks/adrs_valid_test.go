package checks_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestADRsValid_HappyPath(t *testing.T) {
	x := newFakeExec()
	x.setOK("zen", "doctor", "adrs", "--strict", "/repo/docs/decisions")
	c := checks.NewADRsValid(checks.Deps{
		Exec:  x,
		Paths: checks.Paths{ADRsDir: "/repo/docs/decisions"},
	})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckADRsValid {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestADRsValid_NoDirArg_HappyPath(t *testing.T) {
	x := newFakeExec()
	x.setOK("zen", "doctor", "adrs", "--strict")
	c := checks.NewADRsValid(checks.Deps{Exec: x})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
}

func TestADRsValid_NonZeroExit_FailsWithStdoutTruncated(t *testing.T) {
	x := newFakeExec()
	long := strings.Repeat("X", 500)
	x.setFail("zen", 2, long, "doctor", "adrs", "--strict")
	c := checks.NewADRsValid(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("want fail; got %v", status)
	}
	if reason == "" || len(reason) > 500 {
		t.Fatalf("reason must be truncated; got len=%d", len(reason))
	}
}

func TestADRsValid_NonZeroExit_EmptyStdout_FailsWithFallbackReason(t *testing.T) {
	x := newFakeExec()
	x.setFail("zen", 1, "", "doctor", "adrs", "--strict")
	c := checks.NewADRsValid(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("empty-stdout fail: want fail+reason; got %v %q", status, reason)
	}
}

func TestADRsValid_ExecError_Fails(t *testing.T) {
	x := newFakeExec()
	x.setErr("zen", errors.New("binary not found"), "doctor", "adrs", "--strict")
	c := checks.NewADRsValid(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("exec err: want fail+reason; got %v %q", status, reason)
	}
}

func TestADRsValid_NotConfigured_Skips(t *testing.T) {
	c := checks.NewADRsValid(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
