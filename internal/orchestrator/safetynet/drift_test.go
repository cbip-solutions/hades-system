package safetynet

import (
	"context"
	"errors"
	"testing"
)

type fakeCommitSource struct {
	commits []Commit
	err     error
}

func (f *fakeCommitSource) Recent(_ context.Context, _ int) ([]Commit, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.commits, nil
}

func TestDrift_Clean_NoFindings(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "a1", Subject: "feat(safetynet): add prev installer", Body: "Implements sha256 verify path."},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, err := d.Validate(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("clean commit produced findings: %+v", rep.Findings)
	}
	if rep.MaxSeverity != "" {
		t.Errorf("MaxSeverity = %q want empty", rep.MaxSeverity)
	}
}

func TestDrift_ClaudeAttribution_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x1", Subject: "feat(safetynet): add prev",
			Body: "Co-Authored-By: prohibited assistant <claude@anthropic.com>"},
	}}
	em := &fakeEmitter{}
	d := NewDrift(cs, em)
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("max severity = %v want hard", rep.MaxSeverity)
	}
	if len(em.events) == 0 || em.events[0].Type != EventSubstrateDriftDetected {
		t.Fatalf("expected SubstrateDriftDetected; got %+v", em.events)
	}
	if em.events[0].Payload["severity"] != string(SeverityHard) {
		t.Fatalf("payload severity = %v want hard", em.events[0].Payload["severity"])
	}
}

func TestDrift_GeneratedAI_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x-ai", Subject: "feat(x): land thing",
			Body: "Generated with prohibited assistant"},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("Generated with prohibited assistant must be hard; got %v", rep.MaxSeverity)
	}
}

func TestDrift_NonConventionalSubject_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x2", Subject: "added a thing", Body: ""},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("non-conventional subject must be hard; got %v", rep.MaxSeverity)
	}
}

func TestDrift_StubMarkers_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x3", Subject: "feat(x): new thing",
			Body: "Implements with TODO implement later in core path."},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("stub marker must be hard; got %v", rep.MaxSeverity)
	}
}

func TestDrift_PanicNotImplemented_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "p1", Subject: "feat(x): land", Body: `Implements with panic("not implemented") in fallback`},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("panic not implemented must be hard; got %v", rep.MaxSeverity)
	}
}

func TestDrift_ErrNotImplementedPlanN_SeverityHard(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "p2", Subject: "feat(x): land", Body: "returns ErrNotImplementedPlan9 placeholder"},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("ErrNotImplementedPlanN must be hard; got %v", rep.MaxSeverity)
	}
}

func TestDrift_TechDebtMarker_SeveritySoft(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x4", Subject: "feat(x): land thing",
			Body: "Note: tech debt — accept Minor and revisit in Plan 9."},
	}}
	em := &fakeEmitter{}
	d := NewDrift(cs, em)
	rep, _ := d.Validate(context.Background(), 1)
	if rep.MaxSeverity != SeveritySoft {
		t.Fatalf("tech-debt marker = soft; got %v", rep.MaxSeverity)
	}
	if len(em.events) == 0 {
		t.Fatalf("soft also emits the event (state-machine subscriber decides)")
	}
}

func TestDrift_MultipleCommits_AggregatesMaxSeverity(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "ok", Subject: "feat(x): thing", Body: ""},
		{SHA: "soft", Subject: "feat(x): land", Body: "tech debt later"},
		{SHA: "hard", Subject: "added", Body: ""},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 3)
	if rep.MaxSeverity != SeverityHard {
		t.Fatalf("aggregate must be hard when any commit hard; got %v", rep.MaxSeverity)
	}
	if len(rep.Findings) < 2 {
		t.Errorf("expected at least 2 findings; got %d", len(rep.Findings))
	}
}

func TestDrift_OnlySoft_AggregatesSoft(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "s1", Subject: "feat(x): land", Body: "tech debt later"},
		{SHA: "s2", Subject: "feat(y): land", Body: "minor: defer to plan 9"},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rep, _ := d.Validate(context.Background(), 2)
	if rep.MaxSeverity != SeveritySoft {
		t.Fatalf("only-soft set must aggregate to soft; got %v", rep.MaxSeverity)
	}
}

func TestDrift_SourceError_Propagates(t *testing.T) {
	t.Parallel()
	d := NewDrift(&fakeCommitSource{err: errors.New("git log failed")}, &fakeEmitter{})
	_, err := d.Validate(context.Background(), 1)
	if err == nil {
		t.Fatal("want propagated error")
	}
}

func TestDrift_EmitFailureSwallowed(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "x", Subject: "added a thing", Body: ""},
	}}
	d := NewDrift(cs, errEmitter{})
	rep, err := d.Validate(context.Background(), 1)
	if err != nil {
		t.Fatalf("emit failure must not block report: %v", err)
	}
	if rep.MaxSeverity != SeverityHard {
		t.Errorf("severity drift: %v", rep.MaxSeverity)
	}
}

func TestDrift_AllConventionalTypes(t *testing.T) {
	t.Parallel()
	types := []string{"feat", "fix", "chore", "docs", "refactor", "test", "perf", "build", "ci", "style", "revert"}
	for _, ty := range types {
		cs := &fakeCommitSource{commits: []Commit{
			{SHA: ty, Subject: ty + "(x): subject", Body: ""},
		}}
		d := NewDrift(cs, &fakeEmitter{})
		rep, _ := d.Validate(context.Background(), 1)
		if len(rep.Findings) != 0 {
			t.Errorf("type %q produced findings: %+v", ty, rep.Findings)
		}
	}
}
