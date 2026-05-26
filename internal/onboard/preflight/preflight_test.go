package preflight_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
)

func TestPreflightRunReturnsResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pf := preflight.New()
	results, err := pf.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Run: got %d results, want 3 (hermes, plugin_format, daemon); results=%+v", len(results), results)
	}

	gotNames := make(map[string]bool)
	for _, r := range results {
		gotNames[r.Name] = true
	}
	for _, want := range []string{"hermes", "plugin_format", "daemon"} {
		if !gotNames[want] {
			t.Errorf("Run: missing check %q in results", want)
		}
	}
}

func TestPreflightStatusEnumExhaustive(t *testing.T) {
	statuses := []preflight.Status{
		preflight.StatusPass,
		preflight.StatusWarn,
		preflight.StatusFail,
		preflight.StatusSkip,
	}
	for _, s := range statuses {
		if s.String() == "" {
			t.Errorf("Status(%d).String() empty", s)
		}
	}

	if got := preflight.StatusUnknown.String(); got != "unknown" {
		t.Errorf("StatusUnknown.String() = %q, want unknown", got)
	}
	if got := preflight.Status(99).String(); got != "unknown" {
		t.Errorf("Status(99).String() = %q, want unknown", got)
	}
}

func TestPreflightStatusValuesAreDistinct(t *testing.T) {

	wantOrdered := map[preflight.Status]string{
		preflight.StatusUnknown: "unknown",
		preflight.StatusPass:    "pass",
		preflight.StatusWarn:    "warn",
		preflight.StatusFail:    "fail",
		preflight.StatusSkip:    "skip",
	}
	for s, want := range wantOrdered {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestPreflightAnyFailHelper(t *testing.T) {
	results := []preflight.Result{
		{Name: "hermes", Status: preflight.StatusPass},
		{Name: "plugin_format", Status: preflight.StatusFail},
	}
	if !preflight.AnyFail(results) {
		t.Error("AnyFail with one Fail: got false, want true")
	}
	results[1].Status = preflight.StatusWarn
	if preflight.AnyFail(results) {
		t.Error("AnyFail with Warn only: got true, want false")
	}

	if preflight.AnyFail(nil) {
		t.Error("AnyFail(nil): got true, want false")
	}
}

func TestPreflightAnyWarnHelper(t *testing.T) {
	results := []preflight.Result{
		{Name: "hermes", Status: preflight.StatusPass},
		{Name: "plugin_format", Status: preflight.StatusWarn},
	}
	if !preflight.AnyWarn(results) {
		t.Error("AnyWarn with one Warn: got false, want true")
	}
	results[1].Status = preflight.StatusPass
	if preflight.AnyWarn(results) {
		t.Error("AnyWarn with all Pass: got true, want false")
	}
	if preflight.AnyWarn(nil) {
		t.Error("AnyWarn(nil): got true, want false")
	}
}

type fakeCheck struct {
	name   string
	status preflight.Status
	sleep  time.Duration
}

func (c *fakeCheck) Name() string { return c.name }
func (c *fakeCheck) Run(ctx context.Context) preflight.Result {
	if c.sleep > 0 {
		select {
		case <-time.After(c.sleep):
		case <-ctx.Done():
			return preflight.Result{Name: c.name, Status: preflight.StatusFail, ExitCode: 3, Summary: "ctx cancelled"}
		}
	}
	return preflight.Result{Name: c.name, Status: c.status}
}

func TestPreflightWithChecksOrderingPreserved(t *testing.T) {
	pf := preflight.NewWithChecks(
		&fakeCheck{name: "alpha", status: preflight.StatusPass},
		&fakeCheck{name: "beta", status: preflight.StatusWarn},
		&fakeCheck{name: "gamma", status: preflight.StatusSkip},
	)
	results, err := pf.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if results[i].Name != w {
			t.Errorf("results[%d].Name = %q, want %q (registration order broken)", i, results[i].Name, w)
		}
	}
}

func TestPreflightEmptyChecksReturnsEmpty(t *testing.T) {
	pf := preflight.NewWithChecks()
	results, err := pf.Run(context.Background())
	if err != nil {
		t.Fatalf("Run empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Run empty: got %d results, want 0", len(results))
	}
}

func TestPreflightRespectsCtxCancellation(t *testing.T) {
	pf := preflight.NewWithChecks(
		&fakeCheck{name: "slow", status: preflight.StatusPass, sleep: 5 * time.Second},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := pf.Run(ctx)
	if err != nil {
		t.Fatalf("Run cancelled: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != preflight.StatusFail {
		t.Errorf("cancelled ctx: Status = %v, want StatusFail", results[0].Status)
	}
	if results[0].ExitCode != 3 {
		t.Errorf("cancelled ctx: ExitCode = %d, want 3", results[0].ExitCode)
	}
}

func TestErrExecNotFoundExported(t *testing.T) {
	if preflight.ErrExecNotFound() == nil {
		t.Error("ErrExecNotFound returned nil; compliance tests need a non-nil sentinel")
	}
}
