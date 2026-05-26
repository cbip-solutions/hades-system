// SPDX-License-Identifier: MIT
package queue

import (
	"context"
	"fmt"
	"time"
)

type ReviewerTier int

const (
	ReviewerTierL2 ReviewerTier = iota + 2

	ReviewerTierL3

	ReviewerTierL4
)

func (rt ReviewerTier) String() string {
	switch rt {
	case ReviewerTierL2:
		return "l2"
	case ReviewerTierL3:
		return "l3"
	case ReviewerTierL4:
		return "l4"
	default:
		return fmt.Sprintf("unknown_reviewer_tier(%d)", int(rt))
	}
}

func ParseReviewerTier(s string) (ReviewerTier, error) {
	switch s {
	case "l2":
		return ReviewerTierL2, nil
	case "l3":
		return ReviewerTierL3, nil
	case "l4":
		return ReviewerTierL4, nil
	default:
		return 0, fmt.Errorf("workforce/queue: unknown reviewer_tier %q", s)
	}
}

type Severity int

const (
	SeverityMinor Severity = iota + 1

	SeverityMajor

	SeverityReject
)

func (s Severity) String() string {
	switch s {
	case SeverityMinor:
		return "minor"
	case SeverityMajor:
		return "major"
	case SeverityReject:
		return "reject"
	default:
		return fmt.Sprintf("unknown_severity(%d)", int(s))
	}
}

func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "minor":
		return SeverityMinor, nil
	case "major":
		return SeverityMajor, nil
	case "reject":
		return SeverityReject, nil
	default:
		return 0, fmt.Errorf("workforce/queue: unknown severity %q", s)
	}
}

type FixPrompt struct {
	TaskID TaskID

	ProjectID string

	WorkerID string

	ReviewerTier ReviewerTier

	PromptText string

	// CriteriaName references the audit criteria template (default/security/performance).
	CriteriaName string

	Severity Severity

	Consumed bool

	CreatedAt time.Time
}

type FixPromptQueue interface {
	Put(ctx context.Context, fp FixPrompt) error

	DrainByWorker(ctx context.Context, workerID string) ([]FixPrompt, error)

	PendingByWorker(ctx context.Context, workerID string) ([]FixPrompt, error)
}
