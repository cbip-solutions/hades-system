// SPDX-License-Identifier: MIT
package autonomy

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type CheckStatus uint8

const (
	CheckPass CheckStatus = iota + 1

	CheckFail
	// CheckSkip the check could not run (environment-disabled, e.g. MCP not
	// configured). Skip never blocks and never counts as a soft warning.
	CheckSkip
)

func (s CheckStatus) String() string {
	switch s {
	case CheckPass:
		return "pass"
	case CheckFail:
		return "fail"
	case CheckSkip:
		return "skip"
	default:
		return fmt.Sprintf("status(%d)", uint8(s))
	}
}

type CheckEnv struct {
	Doctrine    string
	ProjectRoot string
	Now         time.Time
}

type Check interface {
	Name() string
	Run(ctx context.Context, env CheckEnv) (status CheckStatus, reason string, err error)
}

type CheckResult struct {
	Name     string
	Status   CheckStatus
	Reason   string
	Tier     Tier
	Err      error
	Duration time.Duration
}

type RunInput struct {
	Doctrine          string
	ProjectRoot       string
	AllowSoftWarnings bool
	// PerProjectTiers is the (already-validated) tighten-only override map
	// from hadessystem.toml [autonomy.check_tiers]. nil ⇒ baseline matrix only.
	// Callers MUST run ValidateOverrides at config-load time before passing
	// this map; the engine assumes overrides are tighten-only.
	PerProjectTiers map[string]Tier
}

type RunOutcome struct {
	Proceed      bool
	Results      []CheckResult
	BypassedSoft []CheckResult
}

func (o RunOutcome) HardFailures() []CheckResult {
	var out []CheckResult
	for _, r := range o.Results {
		if r.Tier == TierHard && r.Status == CheckFail {
			out = append(out, r)
		}
	}
	return out
}

func (o RunOutcome) SoftWarnings() []CheckResult {
	var out []CheckResult
	for _, r := range o.Results {
		if r.Tier == TierSoft && r.Status == CheckFail {
			out = append(out, r)
		}
	}
	return out
}

type EngineDeps struct {
	Checks  []Check
	Now     func() time.Time
	Emitter EventEmitter
}

type CheckEngine struct {
	checks  map[string]Check
	now     func() time.Time
	emitter EventEmitter
}

func NewCheckEngine(deps EngineDeps) (*CheckEngine, error) {
	idx := make(map[string]Check, len(deps.Checks))
	for _, c := range deps.Checks {
		if c == nil {
			return nil, fmt.Errorf("autonomy: nil check in EngineDeps.Checks")
		}
		if _, dup := idx[c.Name()]; dup {
			return nil, fmt.Errorf("autonomy: duplicate check registration %q", c.Name())
		}
		idx[c.Name()] = c
	}
	for _, want := range AllCheckNames() {
		if _, ok := idx[want]; !ok {
			return nil, fmt.Errorf("autonomy: missing required check %q", want)
		}
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &CheckEngine{checks: idx, now: now, emitter: deps.Emitter}, nil
}

func (e *CheckEngine) RunCheck(ctx context.Context, in RunInput) (RunOutcome, error) {
	env := CheckEnv{
		Doctrine:    in.Doctrine,
		ProjectRoot: in.ProjectRoot,
		Now:         e.now(),
	}
	results := make([]CheckResult, 0, len(e.checks))
	for _, name := range AllCheckNames() {
		c := e.checks[name]
		baseline, err := TierForCheck(name, in.Doctrine)
		if err != nil {
			return RunOutcome{}, fmt.Errorf("autonomy: %w", err)
		}
		tier := applyOverride(baseline, in.PerProjectTiers[name])
		start := e.now()
		status, reason, runErr := c.Run(ctx, env)
		dur := e.now().Sub(start)
		if runErr != nil {
			status = CheckFail
			if reason == "" {
				reason = runErr.Error()
			}
		}
		results = append(results, CheckResult{
			Name: name, Status: status, Reason: reason, Tier: tier,
			Err: runErr, Duration: dur,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return canonicalIndex(results[i].Name) < canonicalIndex(results[j].Name)
	})

	out := RunOutcome{Results: results, Proceed: true}
	for _, r := range results {
		if r.Tier == TierHard && r.Status == CheckFail {
			out.Proceed = false
		}
		if r.Tier == TierSoft && r.Status == CheckFail && in.AllowSoftWarnings {
			out.BypassedSoft = append(out.BypassedSoft, r)
		}
	}
	// Audit emission is best-effort + cancel-survival; emitter errors do not
	// affect engine semantics. See softwarn.go for the contract.
	e.emitBypassedSoft(ctx, in.Doctrine, out.BypassedSoft)
	return out, nil
}

func canonicalIndex(name string) int {
	for i, n := range AllCheckNames() {
		if n == name {
			return i
		}
	}
	return len(AllCheckNames())
}
