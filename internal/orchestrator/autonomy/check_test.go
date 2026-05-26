package autonomy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type fakeCheck struct {
	name   string
	status autonomy.CheckStatus
	reason string
	err    error
}

func (f *fakeCheck) Name() string { return f.name }
func (f *fakeCheck) Run(_ context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	return f.status, f.reason, f.err
}

func newEngine(t *testing.T, checks ...autonomy.Check) *autonomy.CheckEngine {
	t.Helper()
	e, err := autonomy.NewCheckEngine(autonomy.EngineDeps{
		Checks: checks,
		Now:    func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewCheckEngine: %v", err)
	}
	return e
}

func allFakeCheckNames() []autonomy.Check {
	out := make([]autonomy.Check, 0, len(autonomy.AllCheckNames()))
	for _, n := range autonomy.AllCheckNames() {
		out = append(out, &fakeCheck{name: n, status: autonomy.CheckPass})
	}
	return out
}

func TestCheckEngine_AllPass_OutcomeProceed(t *testing.T) {
	e := newEngine(t, allFakeCheckNames()...)
	out, err := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if !out.Proceed {
		t.Fatalf("all-pass must proceed; got blocked: %+v", out.HardFailures())
	}
	if len(out.Results) != len(autonomy.AllCheckNames()) {
		t.Fatalf("results count: want %d got %d", len(autonomy.AllCheckNames()), len(out.Results))
	}

	for i, r := range out.Results {
		if r.Name != autonomy.AllCheckNames()[i] {
			t.Fatalf("result[%d].Name: want %s got %s", i, autonomy.AllCheckNames()[i], r.Name)
		}
	}
}

func TestCheckEngine_HardFailure_BlocksProceed(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckResearchMCPUp {
			fc.status = autonomy.CheckFail
			fc.reason = "MCP HTTP probe returned 503"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "default"})
	if out.Proceed {
		t.Fatalf("hard failure must block proceed")
	}
	hf := out.HardFailures()
	if len(hf) != 1 || hf[0].Name != autonomy.CheckResearchMCPUp {
		t.Fatalf("hard failure: got %+v", hf)
	}
	if hf[0].Reason == "" {
		t.Fatalf("hard failure reason must propagate")
	}
}

func TestCheckEngine_SoftFailure_DoesNotBlock_ProceedTrue(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "index last update 49h ago (<48h required)"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine:          "default",
		AllowSoftWarnings: false,
	})
	if !out.Proceed {
		t.Fatalf("soft failure on default doctrine must NOT block; got blocked: %+v", out.HardFailures())
	}
	if len(out.SoftWarnings()) != 1 {
		t.Fatalf("expected 1 soft warning; got %+v", out.SoftWarnings())
	}

	if len(out.BypassedSoft) != 0 {
		t.Fatalf("AllowSoftWarnings=false: BypassedSoft must be empty; got %+v", out.BypassedSoft)
	}
}

func TestCheckEngine_SoftFailure_OnMaxScope_IsHard(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "index stale"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "max-scope"})
	if out.Proceed {
		t.Fatalf("max-scope: caronte_index_currency is hard, must block")
	}
}

func TestCheckEngine_InformationalFailure_NeverBlocks(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckAmendmentDryRunApproved {
			fc.status = autonomy.CheckFail
			fc.reason = "no Qx-4 dry-run on record"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "default"})
	if !out.Proceed {
		t.Fatalf("informational failure must never block; got %+v", out.HardFailures())
	}
}

func TestCheckEngine_CheckExecutionError_TreatedAsHardFailure(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckLintClean {
			fc.err = errors.New("exec lint: signal: killed")
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "default"})
	if out.Proceed {
		t.Fatalf("execution error must be treated as failure; tier=hard so must block")
	}
	hf := out.HardFailures()
	if len(hf) != 1 {
		t.Fatalf("expected 1 hard failure; got %d", len(hf))
	}
	if hf[0].Reason != "exec lint: signal: killed" {
		t.Fatalf("execution error reason: want err.Error(); got %q", hf[0].Reason)
	}
	if hf[0].Err == nil {
		t.Fatalf("execution error: Err must propagate")
	}
}

func TestCheckEngine_ExecutionError_PreservesExplicitReason(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckLintClean {
			fc.reason = "explicit reason from check"
			fc.err = errors.New("low-level err")
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "default"})
	hf := out.HardFailures()
	if len(hf) != 1 || hf[0].Reason != "explicit reason from check" {
		t.Fatalf("explicit reason must win over err.Error(); got %+v", hf)
	}
}

func TestCheckEngine_SkipStatus_NeverBlocks_NoFailure(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckResearchMCPUp {
			fc.status = autonomy.CheckSkip
			fc.reason = "MCP not configured"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "max-scope"})
	if !out.Proceed {
		t.Fatalf("skip must not block; got blocked: %+v", out.HardFailures())
	}
	if len(out.HardFailures()) != 0 {
		t.Fatalf("skip must not count as hard failure; got %+v", out.HardFailures())
	}
}

func TestCheckEngine_RegisterMissingCheck_ConstructorErrors(t *testing.T) {
	all := allFakeCheckNames()[1:]
	_, err := autonomy.NewCheckEngine(autonomy.EngineDeps{Checks: all})
	if err == nil {
		t.Fatalf("expected error for missing check registration")
	}
}

func TestCheckEngine_DuplicateCheck_ConstructorErrors(t *testing.T) {
	all := allFakeCheckNames()
	dup := append(all, &fakeCheck{name: autonomy.CheckLintClean, status: autonomy.CheckPass})
	_, err := autonomy.NewCheckEngine(autonomy.EngineDeps{Checks: dup})
	if err == nil {
		t.Fatalf("expected error for duplicate check registration")
	}
}

func TestCheckEngine_NilCheck_ConstructorErrors(t *testing.T) {
	checks := allFakeCheckNames()
	checks[0] = nil
	_, err := autonomy.NewCheckEngine(autonomy.EngineDeps{Checks: checks})
	if err == nil {
		t.Fatalf("expected error for nil check entry")
	}
}

func TestCheckEngine_DefaultNow_UsesTimeNow(t *testing.T) {

	e, err := autonomy.NewCheckEngine(autonomy.EngineDeps{Checks: allFakeCheckNames()})
	if err != nil {
		t.Fatalf("NewCheckEngine without Now: %v", err)
	}
	out, err := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "default"})
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatalf("results empty")
	}
}

func TestCheckEngine_UnknownDoctrine_ReturnsError(t *testing.T) {
	e := newEngine(t, allFakeCheckNames()...)
	_, err := e.RunCheck(context.Background(), autonomy.RunInput{Doctrine: "no-such"})
	if err == nil {
		t.Fatalf("expected error for unknown doctrine")
	}
}

func TestCheckEngine_AllowSoftWarnings_PopulatesBypassedSoft(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "index 60h stale"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine:          "default",
		AllowSoftWarnings: true,
	})
	if !out.Proceed {
		t.Fatalf("AllowSoftWarnings must allow proceed despite soft failure")
	}
	if len(out.BypassedSoft) != 1 {
		t.Fatalf("expected 1 bypassed soft entry; got %+v", out.BypassedSoft)
	}
	if out.BypassedSoft[0].Name != autonomy.CheckCaronteIndexCurrency {
		t.Fatalf("bypassed soft check: got %q", out.BypassedSoft[0].Name)
	}
}

func TestCheckEngine_PerProjectOverride_TightensTier(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "index stale"
		}
	}
	e := newEngine(t, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "default",
		PerProjectTiers: map[string]autonomy.Tier{
			autonomy.CheckCaronteIndexCurrency: autonomy.TierHard,
		},
	})
	if out.Proceed {
		t.Fatalf("tighten override (soft -> hard) must block on failure")
	}
}

func TestCheckStatusString(t *testing.T) {
	cases := []struct {
		s    autonomy.CheckStatus
		want string
	}{
		{autonomy.CheckPass, "pass"},
		{autonomy.CheckFail, "fail"},
		{autonomy.CheckSkip, "skip"},
	}
	for _, c := range cases {
		if c.s.String() != c.want {
			t.Fatalf("CheckStatus.String %v: want %q got %q", c.s, c.want, c.s.String())
		}
	}
	var bogus autonomy.CheckStatus = 99
	if got := bogus.String(); got != "status(99)" {
		t.Fatalf("CheckStatus.String unknown: want status(99); got %q", got)
	}
}

func TestCaronteCheckNamesReplaceGitnexus(t *testing.T) {
	if autonomy.CheckCaronteEngineUp != "caronte_engine_up" {
		t.Errorf("CheckCaronteEngineUp = %q; want caronte_engine_up", autonomy.CheckCaronteEngineUp)
	}
	if autonomy.CheckCaronteIndexCurrency != "caronte_index_currency" {
		t.Errorf("CheckCaronteIndexCurrency = %q; want caronte_index_currency", autonomy.CheckCaronteIndexCurrency)
	}

	for _, name := range autonomy.AllCheckNames() {
		if strings.Contains(name, "gitnexus") {
			t.Errorf("autonomy check %q still references gitnexus; Plan 19 renames to caronte", name)
		}
	}
}
