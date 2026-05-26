// SPDX-License-Identifier: MIT
package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNilWorktreePath = errors.New("worker: worktreePath must be non-empty (inv-zen-087: Plan 5 WorktreePool owns allocation; Plan 4 is consumer-only)")

	// ErrNilSession is returned (and panicked) by Worker constructors when
	// the subprocess.Session is nil. Phase C SubprocessManager.SpawnEphemeral
	// (or AcquirePersistent for persistent variants) MUST be called before
	// constructing the Worker.
	ErrNilSession = errors.New("worker: subprocess.Session must be non-nil (acquire via subprocess.Manager)")

	ErrNilQueues = errors.New("worker: SharedTaskList + CheckpointQueue + FixPromptQueue must all be non-nil (Phase B)")

	ErrNilDoctrineConfig = errors.New("worker: DoctrineConfig must be non-nil (Phase A)")

	ErrNilToolRelay = errors.New("worker: ToolRelay must be non-nil (Phase D ships unavailableRelay default)")

	ErrTaskNotFound = errors.New("worker: task not found in SharedTaskList")

	ErrTaskAlreadyClaimed = errors.New("worker: task already claimed by another worker")

	ErrQuotaExceeded = errors.New("worker: spec Quota exceeded mid-run")

	ErrToolNotAvailable = errors.New("worker: tool relay not configured (Phases I/J/K/L wire real relays)")

	ErrToolNotInWhitelist = errors.New("worker: tool not in WorkerSpec.Tools whitelist")

	ErrUnknownToolFamily = errors.New("worker: unknown tool family (no router for prefix)")
)

type Worker interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

type RunRequest struct {
	TaskID string

	Prompt string
}

func (r RunRequest) Validate() error {
	if strings.TrimSpace(r.TaskID) == "" {
		return errors.New("worker: RunRequest.TaskID must be non-empty")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("worker: RunRequest.Prompt must be non-empty")
	}
	return nil
}

type RunResult struct {
	Success bool

	TokensUsed int

	CostUSD float64

	ToolUseCount int

	CheckpointIDs []string

	Artifacts []string

	FailureReason string

	FinalStopReason string
}

func (r RunResult) String() string {
	status := "FAIL"
	if r.Success {
		status = "OK"
	}
	return fmt.Sprintf(
		"%s tokens=%d cost=$%.4f tool_use=%d checkpoints=%d artifacts=%d stop=%q",
		status, r.TokensUsed, r.CostUSD, r.ToolUseCount,
		len(r.CheckpointIDs), len(r.Artifacts), r.FinalStopReason,
	)
}
