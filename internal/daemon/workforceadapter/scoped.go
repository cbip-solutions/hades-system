// SPDX-License-Identifier: MIT
package workforceadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

// scoped.go — project_id isolation wrappers (I-1 fix).
//
// Background workforce_tasks UNIQUE is on (project_id, task_id), so the same
// task_id can legally exist in two different projects. workforce_checkpoints
// and workforce_fix_prompts have no UNIQUE on task_id at all. Single-row
// methods on the unscoped Impls (Get/Claim/Advance/ByThread/Drain/Peek/
// DrainByWorker/PendingByWorker) filter only by task_id/thread_id/worker_id
// and silently return rows from any project_id when collisions exist.
//
// Resolution ScopedTo(projectID) wraps the underlying *Impl and injects
// project_id = ? into every WHERE clause. Higher layers (orchestrator,
// SubprocessManager, reviewers) MUST call ScopedTo(projectID) before any
// queue operation to guarantee project isolation (spec §7.1
// "project_id on every row" — empirically enforced by these wrappers).
//
// Wrapper pattern preserves the queue.SharedTaskList / CheckpointQueue /
// FixPromptQueue interfaces unchanged (Phase B signatures stay ratified).

type ScopedSharedTaskList struct {
	impl      *SharedTaskListImpl
	projectID string
}

var _ queue.SharedTaskList = (*ScopedSharedTaskList)(nil)

func (impl *SharedTaskListImpl) ScopedTo(projectID string) *ScopedSharedTaskList {
	if projectID == "" {
		panic("workforceadapter: ScopedTo: projectID must be non-empty")
	}
	return &ScopedSharedTaskList{impl: impl, projectID: projectID}
}

func (s *ScopedSharedTaskList) ProjectID() string { return s.projectID }

func (s *ScopedSharedTaskList) Enqueue(ctx context.Context, row queue.TaskRow) error {
	if row.ProjectID == "" {
		row.ProjectID = s.projectID
	} else if row.ProjectID != s.projectID {
		return fmt.Errorf("%w: row.ProjectID=%q, scope=%q",
			queue.ErrProjectIDMismatch, row.ProjectID, s.projectID)
	}
	return s.impl.Enqueue(ctx, row)
}

func (s *ScopedSharedTaskList) Claim(ctx context.Context, taskID queue.TaskID, threadID string) error {
	db := s.impl.s.DB()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("Claim begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statusStr, err := s.impl.scopedClaimQueryFn(ctx, tx, s.projectID, string(taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return queue.ErrTaskNotFound
	}
	if err != nil {
		return fmt.Errorf("Claim read status: %w", err)
	}

	st, err := queue.ParseStatus(statusStr)
	if err != nil {
		return fmt.Errorf("Claim parse status %q: %w", statusStr, err)
	}
	if st != queue.StatusPending {
		return queue.ErrTaskNotPending
	}

	now := toUnixSec(time.Now().UTC())
	if err := s.impl.scopedClaimExecFn(ctx, tx, threadID, now, s.projectID, string(taskID)); err != nil {
		return fmt.Errorf("Claim update: %w", err)
	}
	return s.impl.claimCommitFn(tx)
}

func (s *ScopedSharedTaskList) Advance(ctx context.Context, taskID queue.TaskID, newStatus queue.Status) error {
	db := s.impl.s.DB()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("Advance begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statusStr, err := s.impl.scopedAdvanceQueryFn(ctx, tx, s.projectID, string(taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return queue.ErrTaskNotFound
	}
	if err != nil {
		return fmt.Errorf("Advance read status: %w", err)
	}

	current, err := queue.ParseStatus(statusStr)
	if err != nil {
		return fmt.Errorf("Advance parse status %q: %w", statusStr, err)
	}
	if !queue.IsValidTransition(current, newStatus) {
		return fmt.Errorf("%w: %s -> %s", queue.ErrInvalidTransition, current.String(), newStatus.String())
	}

	now := toUnixSec(time.Now().UTC())
	res, err := s.impl.scopedAdvanceExecFn(ctx, tx, newStatus.String(), now, s.projectID, string(taskID))
	if err != nil {
		return fmt.Errorf("Advance update: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return queue.ErrTaskNotFound
	}
	return s.impl.advanceCommitFn(tx)
}

func (s *ScopedSharedTaskList) Get(ctx context.Context, taskID queue.TaskID) (queue.TaskRow, error) {
	var r queue.TaskRow
	var statusStr string
	var createdAt, updatedAt int64
	var taskIDStr string
	err := s.impl.s.DB().QueryRowContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks WHERE project_id = ? AND task_id = ?`,
		s.projectID, string(taskID),
	).Scan(&taskIDStr, &r.ProjectID, &r.Title, &r.Description,
		&statusStr, &r.ThreadID, &r.Priority, &r.ErrorDetail,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return queue.TaskRow{}, queue.ErrTaskNotFound
	}
	if err != nil {
		return queue.TaskRow{}, fmt.Errorf("Get: %w", err)
	}
	r.TaskID = queue.TaskID(taskIDStr)
	r.CreatedAt = fromUnixSec(createdAt)
	r.UpdatedAt = fromUnixSec(updatedAt)
	st, err := queue.ParseStatus(statusStr)
	if err != nil {
		return queue.TaskRow{}, fmt.Errorf("Get parse status: %w", err)
	}
	r.Status = st
	return r, nil
}

func (s *ScopedSharedTaskList) ListByStatus(ctx context.Context, projectID string, status queue.Status) ([]queue.TaskRow, error) {
	if projectID != "" && projectID != s.projectID {

		return []queue.TaskRow{}, nil
	}
	rows, err := s.impl.s.DB().QueryContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks
		WHERE project_id = ? AND status = ?
		ORDER BY priority ASC, created_at ASC`,
		s.projectID, status.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("ListByStatus: %w", err)
	}
	defer rows.Close()
	return s.impl.scannerFn(rows)
}

func (s *ScopedSharedTaskList) ByThread(ctx context.Context, threadID string) ([]queue.TaskRow, error) {
	rows, err := s.impl.s.DB().QueryContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks
		WHERE project_id = ? AND thread_id = ?
		ORDER BY created_at ASC`,
		s.projectID, threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("ByThread: %w", err)
	}
	defer rows.Close()
	return s.impl.scannerFn(rows)
}

type ScopedCheckpointQueue struct {
	impl      *CheckpointQueueImpl
	projectID string
}

var _ queue.CheckpointQueue = (*ScopedCheckpointQueue)(nil)

func (impl *CheckpointQueueImpl) ScopedTo(projectID string) *ScopedCheckpointQueue {
	if projectID == "" {
		panic("workforceadapter: CheckpointQueueImpl.ScopedTo: projectID must be non-empty")
	}
	return &ScopedCheckpointQueue{impl: impl, projectID: projectID}
}

func (s *ScopedCheckpointQueue) ProjectID() string { return s.projectID }

func (s *ScopedCheckpointQueue) Put(ctx context.Context, cp queue.Checkpoint) error {
	if cp.ProjectID == "" {
		cp.ProjectID = s.projectID
	} else if cp.ProjectID != s.projectID {
		return fmt.Errorf("%w: cp.ProjectID=%q, scope=%q",
			queue.ErrProjectIDMismatch, cp.ProjectID, s.projectID)
	}
	return s.impl.Put(ctx, cp)
}

func (s *ScopedCheckpointQueue) Drain(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	db := s.impl.s.DB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("Drain begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := s.impl.scopedDrainQueryFn(ctx, tx, s.projectID, string(taskID))
	if err != nil {
		return nil, fmt.Errorf("Drain query: %w", err)
	}
	cps, ids, err := s.impl.scannerFn(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []queue.Checkpoint{}, tx.Commit()
	}
	if err := updateConsumedInBatches(ctx, tx, s.impl.drainExecFn, ids); err != nil {
		return nil, fmt.Errorf("Drain mark consumed: %w", err)
	}
	if err := s.impl.drainCommitFn(tx); err != nil {
		return nil, fmt.Errorf("Drain commit: %w", err)
	}
	for i := range cps {
		cps[i].Consumed = true
	}
	return cps, nil
}

func (s *ScopedCheckpointQueue) Peek(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	rows, err := s.impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE project_id = ? AND task_id = ? AND consumed = 0
		ORDER BY sequence_num ASC`,
		s.projectID, string(taskID),
	)
	if err != nil {
		return nil, fmt.Errorf("Peek: %w", err)
	}
	defer rows.Close()
	cps, _, err := scanCheckpoints(rows)
	return cps, err
}

func (s *ScopedCheckpointQueue) ByThread(ctx context.Context, threadID string) ([]queue.Checkpoint, error) {
	rows, err := s.impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE project_id = ? AND thread_id = ?
		ORDER BY sequence_num ASC`,
		s.projectID, threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("ByThread: %w", err)
	}
	defer rows.Close()
	cps, _, err := scanCheckpoints(rows)
	return cps, err
}

type ScopedFixPromptQueue struct {
	impl      *FixPromptQueueImpl
	projectID string
}

var _ queue.FixPromptQueue = (*ScopedFixPromptQueue)(nil)

func (impl *FixPromptQueueImpl) ScopedTo(projectID string) *ScopedFixPromptQueue {
	if projectID == "" {
		panic("workforceadapter: FixPromptQueueImpl.ScopedTo: projectID must be non-empty")
	}
	return &ScopedFixPromptQueue{impl: impl, projectID: projectID}
}

func (s *ScopedFixPromptQueue) ProjectID() string { return s.projectID }

func (s *ScopedFixPromptQueue) Put(ctx context.Context, fp queue.FixPrompt) error {
	if fp.ProjectID == "" {
		fp.ProjectID = s.projectID
	} else if fp.ProjectID != s.projectID {
		return fmt.Errorf("%w: fp.ProjectID=%q, scope=%q",
			queue.ErrProjectIDMismatch, fp.ProjectID, s.projectID)
	}
	return s.impl.Put(ctx, fp)
}

func (s *ScopedFixPromptQueue) DrainByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	db := s.impl.s.DB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("DrainByWorker begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := s.impl.scopedDrainQueryFn(ctx, tx, s.projectID, workerID)
	if err != nil {
		return nil, fmt.Errorf("DrainByWorker query: %w", err)
	}
	fps, ids, err := s.impl.scannerFn(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []queue.FixPrompt{}, tx.Commit()
	}
	if err := updateConsumedInBatches(ctx, tx, s.impl.drainExecFn, ids); err != nil {
		return nil, fmt.Errorf("DrainByWorker mark consumed: %w", err)
	}
	if err := s.impl.drainCommitFn(tx); err != nil {
		return nil, fmt.Errorf("DrainByWorker commit: %w", err)
	}
	for i := range fps {
		fps[i].Consumed = true
	}
	return fps, nil
}

func (s *ScopedFixPromptQueue) PendingByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	rows, err := s.impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, worker_id, reviewer_tier, prompt_text,
		       criteria_name, severity, created_at
		FROM workforce_fix_prompts
		WHERE project_id = ? AND worker_id = ? AND consumed = 0
		ORDER BY created_at ASC`,
		s.projectID, workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("PendingByWorker: %w", err)
	}
	defer rows.Close()
	fps, _, err := scanFixPrompts(rows)
	return fps, err
}
