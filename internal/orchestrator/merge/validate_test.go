package merge_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func makeReq() merge.MergeRequest {
	return merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "0000000000000000000000000000000000000000",
		Mode:         merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111"},
			{Branch: "feat-B", HeadSHA: "2222222222222222222222222222222222222222"},
		},
	}
}

func TestValidateRejectsEmptyTargetBranch(t *testing.T) {
	req := makeReq()
	req.TargetBranch = ""
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
}

func TestValidateRejectsEmptyBaseSHA(t *testing.T) {
	req := makeReq()
	req.BaseSHA = ""
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
}

func TestValidateRejectsModeUnknown(t *testing.T) {
	req := makeReq()
	req.Mode = merge.ModeUnknown
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
}

func TestValidateRejectsZeroCandidates(t *testing.T) {
	req := makeReq()
	req.Candidates = nil
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
	if !strings.Contains(err.Error(), "1≤N≤5") && !strings.Contains(err.Error(), "1 <= N <= 5") {
		t.Errorf("error message %q does not surface bounds", err.Error())
	}
}

func TestValidateRejectsTooManyCandidates(t *testing.T) {
	req := makeReq()
	req.Candidates = make([]merge.MergeCandidate, 6)
	for i := range req.Candidates {
		req.Candidates[i] = merge.MergeCandidate{
			Branch:  "feat-X",
			HeadSHA: strings.Repeat(string(rune('a'+i)), 40),
		}
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
}

func TestValidateRejectsDuplicateHeadSHA(t *testing.T) {
	req := makeReq()
	req.Candidates[1].HeadSHA = req.Candidates[0].HeadSHA
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrCandidatesNotUnique) {
		t.Fatalf("err = %v want wraps ErrCandidatesNotUnique", err)
	}
}

func TestValidateRejectsEmptyHeadSHA(t *testing.T) {
	req := makeReq()
	req.Candidates[1].HeadSHA = ""
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want wraps ErrInvalidRequest", err)
	}
}

func TestValidateRejectsPoolInsufficient(t *testing.T) {
	req := makeReq()
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 1, req)
	if !errors.Is(err, merge.ErrPoolInsufficient) {
		t.Fatalf("err = %v want wraps ErrPoolInsufficient", err)
	}
}

func TestValidateRejectsTargetNotExist(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{
		Stderr: "fatal: ambiguous argument 'refs/heads/main'",
		Err:    errors.New("exit 128"),
	})
	req := makeReq()
	err := merge.Validate(context.Background(), fg, 12, req)
	if !errors.Is(err, merge.ErrTargetNotExist) {
		t.Fatalf("err = %v want wraps ErrTargetNotExist", err)
	}
}

func TestValidateRejectsBaseSHANotMergeBase(t *testing.T) {
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "deadbeef00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
	)
	req := makeReq()
	req.BaseSHA = "0123456789012345678901234567890123456789"
	err := merge.Validate(context.Background(), fg, 12, req)
	if !errors.Is(err, merge.ErrBaseNotMergeBase) {
		t.Fatalf("err = %v want wraps ErrBaseNotMergeBase", err)
	}
}

func TestValidateMergeBaseLookupFailureSurfaces(t *testing.T) {
	mergeBaseErr := errors.New("exit 128: corrupt loose object")
	fg := merge.NewFakeGit(

		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},

		merge.FakeOutput{
			Stderr: "fatal: bad object 1111111111111111111111111111111111111111",
			Err:    mergeBaseErr,
		},
	)
	req := makeReq()
	err := merge.Validate(context.Background(), fg, 12, req)
	if err == nil {
		t.Fatalf("expected error from merge-base failure, got nil")
	}
	if !errors.Is(err, mergeBaseErr) {
		t.Fatalf("err = %v want unwraps to %v", err, mergeBaseErr)
	}
	if !strings.Contains(err.Error(), "merge-base lookup failed") {
		t.Errorf("err = %q want to surface 'merge-base lookup failed' context", err.Error())
	}
}

func TestValidateAcceptsHappyPath(t *testing.T) {
	mb := "deadbeef00000000000000000000000000000000"
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: mb + "\n"},
	)
	req := makeReq()
	req.BaseSHA = mb
	if err := merge.Validate(context.Background(), fg, 12, req); err != nil {
		t.Fatalf("Validate happy: %v", err)
	}
}

func TestValidateAdversarialCorpus(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*merge.MergeRequest)
		capacity int
		fakeOuts []merge.FakeOutput
		wantErr  error
	}{
		{
			name:    "negative N (programmer bug)",
			mutate:  func(r *merge.MergeRequest) { r.Candidates = nil },
			wantErr: merge.ErrInvalidRequest,
		},
		{
			name:    "TargetBranch with whitespace",
			mutate:  func(r *merge.MergeRequest) { r.TargetBranch = "   " },
			wantErr: merge.ErrInvalidRequest,
		},
		{
			name:   "BaseSHA with non-hex content",
			mutate: func(r *merge.MergeRequest) { r.BaseSHA = "not-a-sha" },
			fakeOuts: []merge.FakeOutput{
				{Stdout: "feedface00000000000000000000000000000000\n"},
				{Stdout: "deadbeef00000000000000000000000000000000\n"},
			},
			wantErr: merge.ErrBaseNotMergeBase,
		},
		{
			name: "candidate with empty branch name",
			mutate: func(r *merge.MergeRequest) {
				r.Candidates[0].Branch = ""
			},
			wantErr: merge.ErrInvalidRequest,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := makeReq()
			c.mutate(&req)
			capacity := c.capacity
			if capacity == 0 {
				capacity = 12
			}
			fg := merge.NewFakeGit(c.fakeOuts...)
			err := merge.Validate(context.Background(), fg, capacity, req)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v want wraps %v", err, c.wantErr)
			}
		})
	}
}

func TestValidateRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fg := merge.NewFakeGit(
		merge.FakeOutput{Err: ctx.Err()},
	)
	err := merge.Validate(ctx, fg, 12, makeReq())
	if err == nil {
		t.Fatal("expected error on pre-cancelled context")
	}
}

func TestValidateAcceptsSingleCandidate(t *testing.T) {
	mb := "deadbeef00000000000000000000000000000000"
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
	)
	req := makeReq()
	req.Candidates = req.Candidates[:1]
	req.BaseSHA = mb
	if err := merge.Validate(context.Background(), fg, 12, req); err != nil {
		t.Fatalf("Validate single-candidate: %v", err)
	}
	calls := fg.Calls()
	for _, c := range calls {
		if len(c.Args) > 0 && c.Args[0] == "merge-base" {
			t.Errorf("Validate called merge-base for N=1: %v", c.Args)
		}
	}
}

func TestValidateBoundedLatencyOnFake(t *testing.T) {
	mb := "deadbeef00000000000000000000000000000000"
	req := makeReq()
	req.BaseSHA = mb
	deadline := time.Now().Add(100 * time.Millisecond)
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: mb + "\n"},
	)
	if err := merge.Validate(context.Background(), fg, 12, req); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if time.Now().After(deadline) {
		t.Errorf("Validate exceeded 100ms on FakeGit (no real IO)")
	}
}
