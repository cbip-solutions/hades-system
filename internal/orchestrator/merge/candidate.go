// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/candidate.go
package merge

import (
	"context"
	"time"
)

type CandidateOutcome struct {
	Candidate      MergeCandidate
	TestPassCount  int
	TestFailCount  int
	FlakeCount     int
	HardRejected   bool
	PatchSizeLines int
	Reason         string
	PassingSet     PassingSet
	Stderr         string
	Duration       time.Duration

	BlastRadius float64
}

type CandidateFailureType int

const (
	CandidateFailureUnknown CandidateFailureType = iota

	CandidateFailureTimeout

	CandidateFailurePanic

	CandidateFailureCrash

	CandidateFailureBaselineBreaker

	CandidateFailurePatchRejected

	CandidateFailureGitTransient
)

func (f CandidateFailureType) String() string {
	switch f {
	case CandidateFailureTimeout:
		return "Timeout"
	case CandidateFailurePanic:
		return "Panic"
	case CandidateFailureCrash:
		return "Crash"
	case CandidateFailureBaselineBreaker:
		return "BaselineBreaker"
	case CandidateFailurePatchRejected:
		return "PatchRejected"
	case CandidateFailureGitTransient:
		return "GitTransient"
	default:
		return "Unknown"
	}
}

func AllCandidateFailureTypes() []CandidateFailureType {
	return []CandidateFailureType{
		CandidateFailureTimeout,
		CandidateFailurePanic,
		CandidateFailureCrash,
		CandidateFailureBaselineBreaker,
		CandidateFailurePatchRejected,
		CandidateFailureGitTransient,
	}
}

type CandidateFailure struct {
	CandidateID string               `json:"candidate_id"`
	Type        CandidateFailureType `json:"-"`
	TypeStr     string               `json:"failure_type"`
	Reason      string               `json:"reason"`
	Stderr      string               `json:"stderr"`
}

// CandidateRunner is the → contract. engine.go
// invokes Run(...) per candidate via runner.go's goroutine fan-out
// (internal/orchestrator/merge/runner.go is territory; the
// interface lives here in because that is where the implementation
// surface lives).
//
// Implementations MUST:
// - Honor ctx cancellation (return promptly with wrapped context.Canceled
// or context.DeadlineExceeded; partial state cleanup via defer).
// - Emit EvtCandidateStarted before any side effect; either
// EvtCandidateComplete (success path) or EvtCandidateFailed (any error
// path) before returning.
// - Never return a partially populated CandidateOutcome on the error path
// (return zero-value + non-nil error so callers can rely on the dichotomy).
type CandidateRunner interface {
	Run(ctx context.Context, c MergeCandidate, baseSHA string, passingSet PassingSet, mode Mode, suite TestSuite) (CandidateOutcome, error)
}

type CandidateDeps struct {
	Pool     WorktreePool
	Executor TestExecutor
	Emitter  EventEmitter
	Git      GitClient
	GenCtr   *GenerationCounter
}

type CandidateConfig struct {
	Timeout        time.Duration
	StderrCapBytes int
}

// CandidateStartedPayload is the typed sub-payload for EvtCandidateStarted.
//
// CandidateID is the HeadSHA (the canonical candidate identifier across
// payloads + tests + cache keys); Branch is the human-readable label for
// observability surfaces (HRA dashboard, doctor reports). Mode renders via
// Mode.String() so payload consumers do not need the enum type.
type CandidateStartedPayload struct {
	CandidateID    string `json:"candidate_id"`
	Branch         string `json:"branch"`
	Mode           string `json:"mode"`
	PatchSizeBytes int    `json:"patch_size_bytes"`
}

type CandidateCompletePayload struct {
	CandidateID    string `json:"candidate_id"`
	TestPassCount  int    `json:"test_pass_count"`
	TestFailCount  int    `json:"test_fail_count"`
	FlakeCount     int    `json:"flake_count"`
	HardRejected   bool   `json:"hard_rejected"`
	PatchSizeLines int    `json:"patch_size_lines"`
	PassingSetHash string `json:"passing_set_hash"`
	DurationMs     int64  `json:"duration_ms"`
}

type CandidateFailedPayload struct {
	CandidateID string `json:"candidate_id"`
	FailureType string `json:"failure_type"`
	Reason      string `json:"reason"`
	ExitCode    int    `json:"exit_code"`
	Stderr      string `json:"stderr"`
}

type FlakeRerunStartedPayload struct {
	CandidateID string `json:"candidate_id"`
	RetryN      int    `json:"retry_n"`
	TestID      string `json:"test_id"`
}

type MergeStartedWithModePayload struct {
	RequestHash    string `json:"request_hash"`
	GenerationID   int64  `json:"generation_id"`
	Mode           string `json:"mode"`
	TriggerEventID string `json:"trigger_event_id"`
}

type MergeFailedPayload struct {
	RequestHash string `json:"request_hash"`
	Reason      string `json:"reason"`
	Detail      string `json:"detail"`
}

type MergeAllCandidatesFailedPayload struct {
	RequestHash       string             `json:"request_hash"`
	CandidateFailures []CandidateFailure `json:"candidate_failures"`
}

type MergeStragglerKilledPayload struct {
	CandidateID string `json:"candidate_id"`
	Signal      string `json:"signal"`
	GraceMs     int64  `json:"grace_ms"`
}
