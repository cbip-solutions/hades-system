// SPDX-License-Identifier: MIT
package merge

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"time"
)

type EventType int

const (
	// EvtUnknown is the zero value and MUST NOT be emitted. Defense-in-
	// depth: any subscriber receiving an EvtUnknown event treats it as
	// log corruption.
	EvtUnknown EventType = iota

	EvtMergeStartedWithMode

	EvtMergeCacheHit

	EvtMergeCompleted

	EvtMergeFailed

	EvtMergeAllCandidatesFailed

	EvtBaselineStarted

	EvtBaselineComplete

	EvtBaselineFailed

	EvtCandidateStarted

	EvtCandidateComplete

	EvtCandidateFailed

	EvtFlakeRerunStarted

	EvtScoringComplete

	EvtMergeCacheRebuilt

	EvtMergeStragglerKilled

	EvtMergeAnomalyDetected
)

func (e EventType) String() string {
	switch e {
	case EvtMergeStartedWithMode:
		return "MergeStartedWithMode"
	case EvtMergeCacheHit:
		return "MergeCacheHit"
	case EvtMergeCompleted:
		return "MergeCompleted"
	case EvtMergeFailed:
		return "MergeFailed"
	case EvtMergeAllCandidatesFailed:
		return "MergeAllCandidatesFailed"
	case EvtBaselineStarted:
		return "BaselineStarted"
	case EvtBaselineComplete:
		return "BaselineComplete"
	case EvtBaselineFailed:
		return "BaselineFailed"
	case EvtCandidateStarted:
		return "CandidateStarted"
	case EvtCandidateComplete:
		return "CandidateComplete"
	case EvtCandidateFailed:
		return "CandidateFailed"
	case EvtFlakeRerunStarted:
		return "FlakeRerunStarted"
	case EvtScoringComplete:
		return "ScoringComplete"
	case EvtMergeCacheRebuilt:
		return "MergeCacheRebuilt"
	case EvtMergeStragglerKilled:
		return "MergeStragglerKilled"
	case EvtMergeAnomalyDetected:
		return "MergeAnomalyDetected"
	default:
		return "Unknown"
	}
}

func AllEventTypes() []EventType {
	return []EventType{
		EvtMergeStartedWithMode, EvtMergeCacheHit, EvtMergeCompleted,
		EvtMergeFailed, EvtMergeAllCandidatesFailed,
		EvtBaselineStarted, EvtBaselineComplete, EvtBaselineFailed,
		EvtCandidateStarted, EvtCandidateComplete, EvtCandidateFailed,
		EvtFlakeRerunStarted,
		EvtScoringComplete,
		EvtMergeCacheRebuilt,
		EvtMergeStragglerKilled,
		EvtMergeAnomalyDetected,
	}
}

// AnomalyType is the closed enum of anomaly subtypes carried in a
// MergeAnomalyDetected event payload (spec §2.6, Q11 D). MUST be a Go
// enum (int kind) — inv-hades-110 forbids string-typed anomaly fields so
// that release amendment.proposer's per-type template dispatch is a
// compile-time switch, not a runtime string lookup.
type AnomalyType int

const (
	AnomalyUnknown AnomalyType = iota

	AnomalyScoringFormulaWinnerVetoed

	AnomalyBaselineUnstableAcrossSessions

	AnomalyFlakeRateAboveThreshold

	AnomalyTextualMergeUnresolvableRateHigh

	AnomalyModeDegradationPersistent
)

func (a AnomalyType) String() string {
	switch a {
	case AnomalyScoringFormulaWinnerVetoed:
		return "ScoringFormulaWinnerVetoed"
	case AnomalyBaselineUnstableAcrossSessions:
		return "BaselineUnstableAcrossSessions"
	case AnomalyFlakeRateAboveThreshold:
		return "FlakeRateAboveThreshold"
	case AnomalyTextualMergeUnresolvableRateHigh:
		return "TextualMergeUnresolvableRateHigh"
	case AnomalyModeDegradationPersistent:
		return "ModeDegradationPersistent"
	default:
		return "Unknown"
	}
}

func AllAnomalyTypes() []AnomalyType {
	return []AnomalyType{
		AnomalyScoringFormulaWinnerVetoed,
		AnomalyBaselineUnstableAcrossSessions,
		AnomalyFlakeRateAboveThreshold,
		AnomalyTextualMergeUnresolvableRateHigh,
		AnomalyModeDegradationPersistent,
	}
}

type Event struct {
	Type         EventType
	GenerationID int64
	RequestHash  string
	Payload      []byte
	Timestamp    time.Time
	CausalChain  []string
}

type EventEmitter interface {
	Append(ctx context.Context, e Event) error
}

type GenerationCounter struct {
	n atomic.Int64
}

func (g *GenerationCounter) Next() int64 {
	return g.n.Add(1)
}

func (g *GenerationCounter) Current() int64 {
	return g.n.Load()
}

type MergeRequest struct {
	TargetBranch   string
	BaseSHA        string
	Mode           Mode
	Candidates     []MergeCandidate
	ReviewerVotes  map[string]int
	TestSuite      TestSuite
	SessionID      string
	ProjectID      string
	EngineVersion  string
	TriggerEventID string
}

type MergeCandidate struct {
	Branch       string
	HeadSHA      string
	Patch        []byte
	ReviewerVote int
	SubmittedAt  time.Time
}

type MergeOutcome struct {
	Winner          MergeCandidate
	IntegrationSHA  string
	TestsPassed     bool
	ReviewerSummary string
	AllScores       map[string]float64
	Reverted        bool
}

type TestSuite struct {
	Smoke []string
	Full  []string
}

func (t TestSuite) Equals(other TestSuite) bool {
	return reflect.DeepEqual(t, other)
}

var (
	ErrInvalidRequest = errors.New("merge: invalid request")

	ErrTargetNotExist = errors.New("merge: target branch does not exist")

	ErrBaseNotMergeBase = errors.New("merge: BaseSHA is not git-merge-base of declared heads")

	ErrCandidatesNotUnique = errors.New("merge: candidate HeadSHAs not unique")

	ErrPoolInsufficient = errors.New("merge: worktree pool capacity insufficient")

	ErrGitVersionTooOld = errors.New("merge: git version too old (need ≥2.40)")

	ErrGitNotFound = errors.New("merge: git binary not found on PATH")

	ErrPatchRejected = errors.New("merge: git apply rejected patch")

	ErrBaselineFailed = errors.New("merge: baseline tests failed (cannot establish ground truth)")
)
