package evolution

import (
	"errors"
	"testing"
)

func TestDefaultParamsMatchSpec(t *testing.T) {
	p := DefaultParams()
	if p.MinRevisions != 5 {
		t.Errorf("MinRevisions = %d; want 5", p.MinRevisions)
	}
	if p.MinSharedRevisions != 3 {
		t.Errorf("MinSharedRevisions = %d; want 3", p.MinSharedRevisions)
	}
	if p.MinCouplingPercent != 30 {
		t.Errorf("MinCouplingPercent = %v; want 30", p.MinCouplingPercent)
	}
	if p.MaxChangesetSize != 50 {
		t.Errorf("MaxChangesetSize = %d; want 50", p.MaxChangesetSize)
	}
	if p.MinTotalCommits != 50 {
		t.Errorf("MinTotalCommits = %d; want 50", p.MinTotalCommits)
	}
	if p.WindowDays != 90 {
		t.Errorf("WindowDays = %d; want 90 (default window)", p.WindowDays)
	}
	if !p.FollowRenames {
		t.Error("FollowRenames = false; want true (git log --follow / -M)")
	}
}

func TestParamsValidateAcceptsDefaults(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Errorf("DefaultParams().Validate() = %v; want nil", err)
	}
}

func TestParamsValidateEnforcesFloors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Params)
	}{
		{"min_revisions below floor", func(p *Params) { p.MinRevisions = 4 }},
		{"min_shared below floor", func(p *Params) { p.MinSharedRevisions = 2 }},
		{"min_coupling below floor", func(p *Params) { p.MinCouplingPercent = 29.9 }},
		{"max_changeset above ceiling", func(p *Params) { p.MaxChangesetSize = 51 }},
		{"min_total below floor", func(p *Params) { p.MinTotalCommits = 49 }},
		{"window non-positive", func(p *Params) { p.WindowDays = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := DefaultParams()
			c.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Errorf("%s: Validate() = nil; want a floor violation error", c.name)
			} else if !errors.Is(err, ErrParamsBelowFloor) {
				t.Errorf("%s: Validate() = %v; want ErrParamsBelowFloor", c.name, err)
			}
		})
	}
}

func TestParamsValidateAcceptsTuningAboveFloor(t *testing.T) {
	p := DefaultParams()
	p.MinRevisions = 10
	p.MinSharedRevisions = 6
	p.MinCouplingPercent = 50
	p.MaxChangesetSize = 20
	p.MinTotalCommits = 100
	p.WindowDays = 180
	if err := p.Validate(); err != nil {
		t.Errorf("stricter-than-floor Params rejected: %v", err)
	}
}

type fakeParamsAccessor struct{ p Params }

func (f fakeParamsAccessor) CoChangeParams(projectID string) Params { return f.p }

func TestParamsAccessorSeam(t *testing.T) {
	var acc ParamsAccessor = fakeParamsAccessor{p: DefaultParams()}
	got := acc.CoChangeParams("proj-1")
	if got.WindowDays != 90 {
		t.Errorf("accessor returned WindowDays = %d; want 90", got.WindowDays)
	}
}
