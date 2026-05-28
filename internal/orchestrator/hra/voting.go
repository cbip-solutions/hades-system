// SPDX-License-Identifier: MIT
// Voting algorithms for the Hierarchical Reviewer Assembly per
// spec §1 design choice B + §5.4. This file ships the Plurality classification
// vote (I-1); EMSDecide lands in I-3 and FMV in voting_fmv.go (I-4..I-7).
//
// # File map (one file per concern)
//
// voting.go — Plurality (I-1) + EMSDecide (I-3): classification voting
// voting_fmv.go — Functional Majority Voting (I-4..I-7): fix-proposal selection
//
// # design choice B references
//
// - Plurality (arXiv:2502.19130v4): classification voting; +13.2% on
// reasoning benchmarks vs. single-reviewer baseline.
// - FMV (arXiv:2604.15618): runtime test signatures as tiebreak
// for fix proposals — voting on behaviour, not text.
// - EMS (arXiv:2604.02863): early termination on majority
// convergence — partial-prefix sampling to cap reviewer fan-out.
//
// All voting in this file is a pure function: no I/O, no clock reads,
// no goroutines. The HRA layers supply already-
// gathered votes and route the resulting Decision (or sentinel error)
// into doctrine-shaped events on the eventlog substrate.

package hra

import (
	"errors"
	"fmt"
)

type Class string

const (
	ClassAck Class = "ack"

	ClassNeedsFix Class = "needs_fix"
)

type ClassificationVote struct {
	ReviewerID string

	Class Class
}

type Decision struct {
	Winner Class

	Threshold int

	ForCount int

	AgainstCount int
}

var (
	ErrNoVotes = errors.New("hra: no votes supplied")

	ErrUnknownClass = errors.New("hra: unknown classification")

	ErrPluralityTie = errors.New("hra: plurality tie — escalate L3")

	ErrFMVTie = errors.New("hra: FMV tie — escalate L3")

	ErrFMVAllFailed = errors.New("hra: all FMV candidates failed tests — escalate L3")

	ErrEMSNotConverged = errors.New("hra: EMS partial-prefix not converged — sample more")
)

func majorityThreshold(n int) int { return (n + 1 + 1) / 2 }

func Plurality(votes []ClassificationVote) (Decision, error) {
	if len(votes) == 0 {
		return Decision{}, ErrNoVotes
	}
	var ack, fix int
	for _, v := range votes {
		switch v.Class {
		case ClassAck:
			ack++
		case ClassNeedsFix:
			fix++
		default:
			return Decision{}, fmt.Errorf("%w: %q", ErrUnknownClass, v.Class)
		}
	}
	thr := majorityThreshold(len(votes))
	switch {
	case ack >= thr && ack > fix:
		return Decision{Winner: ClassAck, Threshold: thr, ForCount: ack, AgainstCount: fix}, nil
	case fix >= thr && fix > ack:
		return Decision{Winner: ClassNeedsFix, Threshold: thr, ForCount: fix, AgainstCount: ack}, nil
	default:
		return Decision{}, ErrPluralityTie
	}
}

// EMSDecide applies the EMS Majority-then-Stopping rule
// (arXiv:2604.02863) over a partial-prefix of reviewer samples.
//
// Returns
// - (decision, true, nil) ⇒ partial sample is sufficient; caller stops sampling.
// - (zero, false, nil) ⇒ caller MUST sample more reviewers and call again.
// - (zero, false, err) ⇒ programming error or final-sample tie.
//
// Convergence rule: let K = totalExpected; threshold T = ceil((K+1)/2).
// If max(ack_count, fix_count) >= T, the remaining (K - len(samples))
// reviewers cannot overturn the partial winner — return converged=true.
// If len(samples) == K and neither class reaches T → ErrPluralityTie.
//
// Cost benefit (design choice B): on converged checkpoints, the orchestrator
// avoids invoking the deep-thinking tactical reviewers for the
// remaining K - len(samples) slots, bounding spend in the live-correction
// inner loop (spec §3.2).
//
// Pure function; no side effects; safe for concurrent calls.
func EMSDecide(samples []ClassificationVote, totalExpected int) (Decision, bool, error) {
	if totalExpected <= 0 {
		return Decision{}, false, fmt.Errorf("hra: totalExpected must be > 0, got %d", totalExpected)
	}
	if len(samples) > totalExpected {
		return Decision{}, false, fmt.Errorf("hra: samples=%d exceeds totalExpected=%d", len(samples), totalExpected)
	}
	if len(samples) == 0 {
		return Decision{}, false, ErrNoVotes
	}
	var ack, fix int
	for _, v := range samples {
		switch v.Class {
		case ClassAck:
			ack++
		case ClassNeedsFix:
			fix++
		default:
			return Decision{}, false, fmt.Errorf("%w: %q", ErrUnknownClass, v.Class)
		}
	}
	thr := majorityThreshold(totalExpected)
	switch {
	case ack >= thr && ack > fix:
		return Decision{Winner: ClassAck, Threshold: thr, ForCount: ack, AgainstCount: fix}, true, nil
	case fix >= thr && fix > ack:
		return Decision{Winner: ClassNeedsFix, Threshold: thr, ForCount: fix, AgainstCount: ack}, true, nil
	case len(samples) == totalExpected:

		return Decision{}, false, ErrPluralityTie
	default:

		return Decision{}, false, nil
	}
}
