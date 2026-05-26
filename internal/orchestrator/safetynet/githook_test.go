package safetynet

import (
	"context"
	"errors"
	"testing"
)

func TestRunPreCommitDrift_HardFinding_ReturnsBlockingExit(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "HEAD", Subject: "added stuff"},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rc := RunPreCommitDrift(context.Background(), d, 1)
	if rc != 1 {
		t.Fatalf("hard finding exit = %d want 1", rc)
	}
}

func TestRunPreCommitDrift_OnlySoft_AllowsCommit(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "HEAD", Subject: "feat(x): land", Body: "tech debt later"},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rc := RunPreCommitDrift(context.Background(), d, 1)
	if rc != 0 {
		t.Fatalf("soft-only exit = %d want 0 (warn, not block)", rc)
	}
}

func TestRunPreCommitDrift_Clean_AllowsCommit(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{commits: []Commit{
		{SHA: "HEAD", Subject: "feat(x): land", Body: "clean."},
	}}
	d := NewDrift(cs, &fakeEmitter{})
	rc := RunPreCommitDrift(context.Background(), d, 1)
	if rc != 0 {
		t.Fatalf("clean exit = %d want 0", rc)
	}
}

func TestRunPreCommitDrift_ValidateError_BlocksCommit(t *testing.T) {
	t.Parallel()
	cs := &fakeCommitSource{err: errors.New("git log failed")}
	d := NewDrift(cs, &fakeEmitter{})
	rc := RunPreCommitDrift(context.Background(), d, 1)
	if rc != 1 {
		t.Fatalf("validate-error exit = %d want 1 (fail-closed)", rc)
	}
}
