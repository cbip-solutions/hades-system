package hra_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func TestPlurality_Wins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		votes     []hra.ClassificationVote
		wantWin   hra.Class
		wantFor   int
		wantThr   int
		wantAgnst int
	}{
		{
			name: "3 reviewers 2-1 needs_fix",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassNeedsFix},
				{ReviewerID: "r2", Class: hra.ClassNeedsFix},
				{ReviewerID: "r3", Class: hra.ClassAck},
			},
			wantWin:   hra.ClassNeedsFix,
			wantFor:   2,
			wantThr:   2,
			wantAgnst: 1,
		},
		{
			name: "3 reviewers 3-0 ack",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassAck},
				{ReviewerID: "r2", Class: hra.ClassAck},
				{ReviewerID: "r3", Class: hra.ClassAck},
			},
			wantWin:   hra.ClassAck,
			wantFor:   3,
			wantThr:   2,
			wantAgnst: 0,
		},
		{
			name: "5 reviewers 3-2 ack (Q8 B threshold ceil((5+1)/2)=3)",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassAck},
				{ReviewerID: "r2", Class: hra.ClassAck},
				{ReviewerID: "r3", Class: hra.ClassAck},
				{ReviewerID: "r4", Class: hra.ClassNeedsFix},
				{ReviewerID: "r5", Class: hra.ClassNeedsFix},
			},
			wantWin:   hra.ClassAck,
			wantFor:   3,
			wantThr:   3,
			wantAgnst: 2,
		},
		{
			name: "2 reviewers unanimous needs_fix",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassNeedsFix},
				{ReviewerID: "r2", Class: hra.ClassNeedsFix},
			},
			wantWin:   hra.ClassNeedsFix,
			wantFor:   2,
			wantThr:   2,
			wantAgnst: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := hra.Plurality(tc.votes)
			if err != nil {
				t.Fatalf("Plurality returned err=%v, want nil", err)
			}
			if d.Winner != tc.wantWin {
				t.Errorf("Winner=%q, want %q", d.Winner, tc.wantWin)
			}
			if d.ForCount != tc.wantFor {
				t.Errorf("ForCount=%d, want %d", d.ForCount, tc.wantFor)
			}
			if d.Threshold != tc.wantThr {
				t.Errorf("Threshold=%d, want %d", d.Threshold, tc.wantThr)
			}
			if d.AgainstCount != tc.wantAgnst {
				t.Errorf("AgainstCount=%d, want %d", d.AgainstCount, tc.wantAgnst)
			}
		})
	}
}

func TestPlurality_EmptyVotesIsError(t *testing.T) {
	t.Parallel()

	_, err := hra.Plurality(nil)
	if err == nil {
		t.Fatal("Plurality(nil) returned err=nil, want ErrNoVotes")
	}
	if !errors.Is(err, hra.ErrNoVotes) {
		t.Errorf("err=%v, want errors.Is(_, ErrNoVotes)", err)
	}

	_, err = hra.Plurality([]hra.ClassificationVote{})
	if !errors.Is(err, hra.ErrNoVotes) {
		t.Errorf("Plurality([]) err=%v, want errors.Is(_, ErrNoVotes)", err)
	}
}

func TestPlurality_UnknownClassIsError(t *testing.T) {
	t.Parallel()

	votes := []hra.ClassificationVote{
		{ReviewerID: "r1", Class: hra.ClassAck},
		{ReviewerID: "r2", Class: hra.Class("garbage")},
	}
	_, err := hra.Plurality(votes)
	if err == nil {
		t.Fatal("Plurality(unknown) returned err=nil, want ErrUnknownClass")
	}
	if !errors.Is(err, hra.ErrUnknownClass) {
		t.Errorf("err=%v, want errors.Is(_, ErrUnknownClass)", err)
	}
}

func TestPlurality_TieIsError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		votes []hra.ClassificationVote
	}{
		{
			name: "2 reviewers split 1-1",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassAck},
				{ReviewerID: "r2", Class: hra.ClassNeedsFix},
			},
		},
		{
			name: "4 reviewers split 2-2",
			votes: []hra.ClassificationVote{
				{ReviewerID: "r1", Class: hra.ClassAck},
				{ReviewerID: "r2", Class: hra.ClassAck},
				{ReviewerID: "r3", Class: hra.ClassNeedsFix},
				{ReviewerID: "r4", Class: hra.ClassNeedsFix},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := hra.Plurality(tc.votes)
			if !errors.Is(err, hra.ErrPluralityTie) {
				t.Errorf("err=%v, want errors.Is(_, ErrPluralityTie)", err)
			}
		})
	}
}

func TestPlurality_RaceFree(t *testing.T) {
	t.Parallel()

	votes := []hra.ClassificationVote{
		{ReviewerID: "r1", Class: hra.ClassAck},
		{ReviewerID: "r2", Class: hra.ClassAck},
		{ReviewerID: "r3", Class: hra.ClassNeedsFix},
	}
	want, err := hra.Plurality(votes)
	if err != nil {
		t.Fatalf("baseline Plurality err=%v", err)
	}

	const N = 100
	var wg sync.WaitGroup
	got := make([]hra.Decision, N)
	errs := make([]error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = hra.Plurality(votes)
		}(i)
	}
	wg.Wait()
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d err=%v", i, errs[i])
		}
		if got[i] != want {
			t.Errorf("goroutine %d Decision=%+v, want %+v", i, got[i], want)
		}
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	t.Parallel()

	all := []error{
		hra.ErrNoVotes,
		hra.ErrUnknownClass,
		hra.ErrPluralityTie,
		hra.ErrFMVTie,
		hra.ErrFMVAllFailed,
		hra.ErrEMSNotConverged,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %d (%v) errors.Is sentinel %d (%v) — must be distinct", i, a, j, b)
			}
		}
	}
}
