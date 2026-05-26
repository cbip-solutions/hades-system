package checks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestLintClean_HappyPath(t *testing.T) {
	x := newFakeExec()
	x.setOK("make", "lint", "-q")
	c := checks.NewLintClean(checks.Deps{Exec: x})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckPass {
		t.Fatalf("want pass; got %v", status)
	}
	if c.Name() != autonomy.CheckLintClean {
		t.Fatalf("Name: %q", c.Name())
	}
}

func TestLintClean_NonZeroExit_Fails(t *testing.T) {
	x := newFakeExec()
	x.setFail("make", 1, "lint findings: 3", "lint", "-q")
	c := checks.NewLintClean(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+reason; got %v %q", status, reason)
	}
}

func TestLintClean_NonZeroExit_EmptyStdout_FallbackReason(t *testing.T) {
	x := newFakeExec()
	x.setFail("make", 1, "", "lint", "-q")
	c := checks.NewLintClean(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("want fail+fallback reason; got %v %q", status, reason)
	}
}

func TestLintClean_ExecError_Fails(t *testing.T) {
	x := newFakeExec()
	x.setErr("make", errors.New("signal: killed"), "lint", "-q")
	c := checks.NewLintClean(checks.Deps{Exec: x})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail || reason == "" {
		t.Fatalf("exec err: want fail+reason; got %v %q", status, reason)
	}
}

func TestLintClean_NotConfigured_Skips(t *testing.T) {
	c := checks.NewLintClean(checks.Deps{})
	status, _, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckSkip {
		t.Fatalf("want skip; got %v", status)
	}
}
