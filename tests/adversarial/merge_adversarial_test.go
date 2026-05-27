// go:build adversarial

package adversarial

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestAdversarial_MalformedUnifiedDiff(t *testing.T) {
	fg := merge.NewFakeGit(merge.FakeOutput{
		Stderr: "fatal: corrupt patch at line 5",
		Err:    errors.New("exit 1"),
	})
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", []byte("garbage\n"), 512)
	if !errors.Is(err, merge.ErrPatchRejected) {
		t.Errorf("err = %v want ErrPatchRejected", err)
	}
}

func TestAdversarial_OversizedPatch(t *testing.T) {
	bigPatch := []byte(strings.Repeat("a\n", 5500))
	if got := merge.PatchSizeLines(bigPatch); got < 5500 {
		t.Errorf("PatchSizeLines = %d want ≥5500", got)
	}
}

func TestAdversarial_FakeBaseSHANotMergeBase(t *testing.T) {
	fg := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
	)
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "0123456789012345678901234567890123456789",
		Mode:         merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111"},
			{Branch: "feat-B", HeadSHA: "2222222222222222222222222222222222222222"},
		},
	}
	err := merge.Validate(context.Background(), fg, 12, req)
	if !errors.Is(err, merge.ErrBaseNotMergeBase) {
		t.Fatalf("err = %v want ErrBaseNotMergeBase", err)
	}
}

func TestAdversarial_DuplicateHeadSHAs(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "h1"},
			{Branch: "feat-B", HeadSHA: "h1"},
		},
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrCandidatesNotUnique) {
		t.Fatalf("err = %v want ErrCandidatesNotUnique", err)
	}
}

func TestAdversarial_EmptyTargetBranch(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeNormal,
		Candidates:   []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h1"}},
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want ErrInvalidRequest", err)
	}
}

func TestAdversarial_NTooLarge(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeNormal,
	}
	for i := 0; i < 6; i++ {
		req.Candidates = append(req.Candidates, merge.MergeCandidate{
			Branch:  "feat-X",
			HeadSHA: strings.Repeat(string(rune('a'+i)), 40),
		})
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want ErrInvalidRequest (N=6)", err)
	}
}

func TestAdversarial_ModeUnknown(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeUnknown,
		Candidates:   []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h1"}},
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 12, req)
	if !errors.Is(err, merge.ErrInvalidRequest) {
		t.Fatalf("err = %v want ErrInvalidRequest (Mode=Unknown)", err)
	}
}

func TestAdversarial_PoolCapacityInsufficient(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "h1"},
			{Branch: "feat-B", HeadSHA: "h2"},
			{Branch: "feat-C", HeadSHA: "h3"},
		},
	}
	err := merge.Validate(context.Background(), merge.NewFakeGit(), 1, req)
	if !errors.Is(err, merge.ErrPoolInsufficient) {
		t.Fatalf("err = %v want ErrPoolInsufficient", err)
	}
}

func TestAdversarial_EmptyCandidatePatch(t *testing.T) {
	fg := merge.NewFakeGit()
	err := merge.ApplyPatch(context.Background(), fg, "/tmp/wt", nil, 512)
	if !errors.Is(err, merge.ErrPatchRejected) {
		t.Errorf("err = %v want ErrPatchRejected on nil patch", err)
	}
}

func TestAdversarial_ConcurrentValidateRaceFree(t *testing.T) {
	req := merge.MergeRequest{
		TargetBranch: "main",
		BaseSHA:      "deadbeef",
		Mode:         merge.ModeNormal,
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "h1"},
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fg := merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
			_ = merge.Validate(context.Background(), fg, 12, req)
		}()
	}
	wg.Wait()
}
