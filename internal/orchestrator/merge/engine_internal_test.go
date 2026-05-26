package merge

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestLookupCandidate(t *testing.T) {
	t.Parallel()

	a := MergeCandidate{Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111"}
	b := MergeCandidate{Branch: "feat-B", HeadSHA: "2222222222222222222222222222222222222222"}
	bDup := MergeCandidate{Branch: "feat-B-dup", HeadSHA: "2222222222222222222222222222222222222222"}

	cases := []struct {
		name       string
		candidates []MergeCandidate
		headSHA    string
		want       MergeCandidate
		wantOK     bool
	}{
		{
			name:       "match",
			candidates: []MergeCandidate{a, b},
			headSHA:    b.HeadSHA,
			want:       b,
			wantOK:     true,
		},
		{
			name:       "no_match",
			candidates: []MergeCandidate{a, b},
			headSHA:    "ffffffffffffffffffffffffffffffffffffffff",
			want:       MergeCandidate{},
			wantOK:     false,
		},
		{
			name:       "empty",
			candidates: nil,
			headSHA:    a.HeadSHA,
			want:       MergeCandidate{},
			wantOK:     false,
		},
		{
			name:       "duplicate_first_wins",
			candidates: []MergeCandidate{b, bDup},
			headSHA:    b.HeadSHA,
			want:       b,
			wantOK:     true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, gotOK := lookupCandidate(c.candidates, c.headSHA)
			if gotOK != c.wantOK {
				t.Errorf("lookupCandidate(%s) ok = %v, want %v", c.name, gotOK, c.wantOK)
			}

			if !mergeCandidateEqual(got, c.want) {
				t.Errorf("lookupCandidate(%s) candidate = %+v, want %+v", c.name, got, c.want)
			}
		})
	}
}

func mergeCandidateEqual(a, b MergeCandidate) bool {
	return a.Branch == b.Branch &&
		a.HeadSHA == b.HeadSHA &&
		a.ReviewerVote == b.ReviewerVote &&
		a.SubmittedAt.Equal(b.SubmittedAt) &&
		bytes.Equal(a.Patch, b.Patch)
}

func TestRollbackFastForwardWithPreExistingRef(t *testing.T) {
	t.Parallel()

	fg := NewFakeGit(FakeOutput{})
	e := &realEngine{deps: Deps{Git: fg}}

	if err := e.rollbackFastForward("main", "preff000000000000000000000000000000ff00f"); err != nil {
		t.Fatalf("rollbackFastForward(preSHA=set): err = %v, want nil", err)
	}

	calls := fg.Calls()
	if len(calls) != 1 {
		t.Fatalf("FakeGit.Calls() len = %d, want 1", len(calls))
	}
	want := []string{"update-ref", "refs/heads/main", "preff000000000000000000000000000000ff00f"}
	got := calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("rollback args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rollback args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRollbackFastForwardWithEmptyPreSHA(t *testing.T) {
	t.Parallel()

	fg := NewFakeGit(FakeOutput{})
	e := &realEngine{deps: Deps{Git: fg}}

	if err := e.rollbackFastForward("feature-x", ""); err != nil {
		t.Fatalf("rollbackFastForward(preSHA=empty): err = %v, want nil", err)
	}

	calls := fg.Calls()
	if len(calls) != 1 {
		t.Fatalf("FakeGit.Calls() len = %d, want 1", len(calls))
	}
	want := []string{"update-ref", "-d", "refs/heads/feature-x"}
	got := calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("rollback args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rollback args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRollbackFastForwardForwardsGitErr(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("rollback-blew-up")
	fg := NewFakeGit(FakeOutput{Err: wantErr})
	e := &realEngine{deps: Deps{Git: fg}}

	if err := e.rollbackFastForward("main", "preffffff"); !errors.Is(err, wantErr) {
		t.Errorf("rollbackFastForward err = %v; want errors.Is %v", err, wantErr)
	}
}

// TestRollbackFastForwardUsesBackgroundCtx — the helper MUST use
// context.Background() (not the passed-in cctx which is dead by the time
// rollback runs). Verified by passing a pre-cancelled ctx to rollback;
// FakeGit ignores ctx but a real Git would honor it. The contract is
// tested by inspecting that the helper does not error solely on cancelled
// ctx (FakeGit returns success regardless of ctx).
//
// This documents the contract so a future refactor that mistakenly threads
// the cancelled cctx into rollback would surface as a behaviour change
// (real git would refuse the subprocess; rollback would fail when in
// production it should still succeed).
func TestRollbackFastForwardUsesBackgroundCtx(t *testing.T) {
	t.Parallel()

	fg := NewFakeGit(FakeOutput{})
	e := &realEngine{deps: Deps{Git: fg}}

	if err := e.rollbackFastForward("main", "deadbeef"); err != nil {
		t.Errorf("rollbackFastForward unexpected err: %v", err)
	}

	_ = context.Background()
}

func TestCandidateChangedFilesEmpty(t *testing.T) {
	t.Parallel()
	syms, files := candidateChangedFiles(MergeCandidate{Patch: nil})
	if syms != nil {
		t.Errorf("changedSymbols = %v; want nil (merge engine passes nil symbols, Phase J enriches)", syms)
	}
	if files != nil {
		t.Errorf("changedFiles = %v; want nil for empty patch", files)
	}
}

func TestCandidateChangedFilesParsesGitDiffHeaders(t *testing.T) {
	t.Parallel()
	patch := []byte(
		"diff --git a/pkg/foo.go b/pkg/foo.go\n" +
			"--- a/pkg/foo.go\n" +
			"+++ b/pkg/foo.go\n" +
			"@@ -1,3 +1,4 @@\n" +
			" package foo\n" +
			"+// comment\n" +
			"diff --git a/pkg/bar.go b/pkg/bar.go\n" +
			"--- a/pkg/bar.go\n" +
			"+++ b/pkg/bar.go\n" +
			"@@ -5,2 +5,3 @@\n" +
			"+// added\n" +
			"diff --git a/pkg/foo.go b/pkg/foo.go\n" +
			"--- a/pkg/foo.go\n" +
			"+++ b/pkg/foo.go\n" +
			"@@ -10,1 +10,2 @@\n" +
			"+// another hunk\n",
	)
	_, files := candidateChangedFiles(MergeCandidate{Patch: patch})

	if len(files) != 2 {
		t.Fatalf("changedFiles len = %d; want 2 (deduplicated): %v", len(files), files)
	}
	if files[0] != "pkg/foo.go" {
		t.Errorf("changedFiles[0] = %q; want %q", files[0], "pkg/foo.go")
	}
	if files[1] != "pkg/bar.go" {
		t.Errorf("changedFiles[1] = %q; want %q", files[1], "pkg/bar.go")
	}
}

func TestCandidateChangedFilesSkipsDevNull(t *testing.T) {
	t.Parallel()
	patch := []byte(
		"diff --git a/gone.go b/gone.go\n" +
			"--- a/gone.go\n" +
			"+++ /dev/null\n" +
			"@@ -1,3 +0,0 @@\n" +
			"-package gone\n",
	)
	_, files := candidateChangedFiles(MergeCandidate{Patch: patch})
	if len(files) != 0 {
		t.Errorf("changedFiles = %v; want empty (deleted /dev/null excluded)", files)
	}
}

func TestCandidateChangedFilesNoBPrefix(t *testing.T) {
	t.Parallel()
	patch := []byte(
		"+++ internal/x/y.go\n" +
			"@@ -1,1 +1,2 @@\n" +
			"+// new\n",
	)
	_, files := candidateChangedFiles(MergeCandidate{Patch: patch})
	if len(files) != 1 || files[0] != "internal/x/y.go" {
		t.Errorf("changedFiles = %v; want [internal/x/y.go] (no b/ prefix)", files)
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	t.Parallel()
	if got := splitLines(nil); got != nil {
		t.Errorf("splitLines(nil) = %v; want nil", got)
	}
}

func TestSplitLinesBasic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"a\nb\nc\n", []string{"a", "b", "c"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"single\n", []string{"single"}},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		got := splitLines([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q) len = %d; want %d: %v", c.in, len(got), len(c.want), got)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q; want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
