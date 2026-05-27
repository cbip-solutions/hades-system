// SPDX-License-Identifier: MIT
// internal/orchestrator/apply/merge_engine.go
package apply

import (
	"context"
	"time"
)

type MergeCandidate struct {
	Branch       string    `json:"branch"`
	HeadSHA      string    `json:"head_sha"`
	ReviewerVote int       `json:"reviewer_vote"`
	SubmittedAt  time.Time `json:"submitted_at"`
}

type MergeRequest struct {
	TargetBranch string           `json:"target_branch"`
	Candidates   []MergeCandidate `json:"candidates"`
	BaseSHA      string           `json:"base_sha"`
}

// MergeOutcome is what real engine returns. callers use
// the shape but never expect a non-nil value from the fake (J-5 contract:
// callers MUST guard cross-worker scenarios with the canonical t.Skip).
//
// Field semantics:
// - Winner — the chosen candidate (zero-value when no winner).
// - IntegrationSHA — the SHA on TargetBranch after fast-forward.
// - TestsPassed — true iff the winner's substrate tests passed
// after the merge.
// - ReviewerSummary — free-form rationale for audit consumers +
// hash-chain replay.
type MergeOutcome struct {
	Winner          MergeCandidate `json:"winner"`
	IntegrationSHA  string         `json:"integration_sha"`
	TestsPassed     bool           `json:"tests_passed"`
	ReviewerSummary string         `json:"reviewer_summary,omitempty"`
}

// MergeEngine is the cross-worker integration contract. ships ONLY
// this interface declaration; implements it. Callers MUST tolerate
// a fake-only mode in by skipping cross-worker scenarios in tests
// ).
//
// Q1 D rationale: live correction is single-worker-branch sequential and
// owned by ApplyEngine (this package, real). Cross-worker integration is
// the 3-way merge problem and is owned by MergeEngine. The split
// is by concern, not by phase ordering — each engine has a distinct
// failure surface and distinct SOTA reference (git-apply atomicity vs
// IntelliMerge / MergeBERT / LLMinus test-driven merge).
//
// Idempotency contract: Merge MUST
// be idempotent on (TargetBranch, BaseSHA, sorted candidate HeadSHAs)
// so that orchestrator replay reaches the
// same outcome on a re-run.
type MergeEngine interface {
	Merge(ctx context.Context, req MergeRequest) (MergeOutcome, error)
}

var ErrMergeNotImplemented = errMergeNotImpl{}

type errMergeNotImpl struct{}

func (errMergeNotImpl) Error() string {
	return "apply.MergeEngine: Plan 6 not yet shipped — fake-only mode"
}
