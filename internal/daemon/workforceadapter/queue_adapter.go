// SPDX-License-Identifier: MIT
package workforceadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

var _ queue.SharedTaskList = (*SharedTaskListImpl)(nil)

type SharedTaskListImpl struct {
	s         *store.Store
	scannerFn func(rowsScanner) ([]queue.TaskRow, error)

	claimQueryFn  func(ctx context.Context, tx *sql.Tx, taskID string) (string, error)
	claimExecFn   func(ctx context.Context, tx *sql.Tx, threadID string, now int64, taskID string) error
	claimCommitFn func(tx *sql.Tx) error

	scopedClaimQueryFn func(ctx context.Context, tx *sql.Tx, projectID, taskID string) (string, error)
	scopedClaimExecFn  func(ctx context.Context, tx *sql.Tx, threadID string, now int64, projectID, taskID string) error

	advanceQueryFn  func(ctx context.Context, tx *sql.Tx, taskID string) (string, error)
	advanceExecFn   func(ctx context.Context, tx *sql.Tx, newStatusStr string, now int64, taskID string) (sql.Result, error)
	advanceCommitFn func(tx *sql.Tx) error

	scopedAdvanceQueryFn func(ctx context.Context, tx *sql.Tx, projectID, taskID string) (string, error)
	scopedAdvanceExecFn  func(ctx context.Context, tx *sql.Tx, newStatusStr string, now int64, projectID, taskID string) (sql.Result, error)
}

func NewSharedTaskList(s *store.Store) *SharedTaskListImpl {
	if s == nil {
		panic("workforceadapter: NewSharedTaskList: store must not be nil")
	}

	_ = queue.DurableQueueOpened()
	if err := configureDurableConn(s.DB()); err != nil {
		panic(fmt.Sprintf("workforceadapter: WAL configuration failed: %v", err))
	}
	return &SharedTaskListImpl{
		s:                    s,
		scannerFn:            scanTaskRows,
		claimQueryFn:         defaultClaimQuery,
		claimExecFn:          defaultClaimExec,
		claimCommitFn:        (*sql.Tx).Commit,
		scopedClaimQueryFn:   defaultScopedClaimQuery,
		scopedClaimExecFn:    defaultScopedClaimExec,
		advanceQueryFn:       defaultAdvanceQuery,
		advanceExecFn:        defaultAdvanceExec,
		advanceCommitFn:      (*sql.Tx).Commit,
		scopedAdvanceQueryFn: defaultScopedAdvanceQuery,
		scopedAdvanceExecFn:  defaultScopedAdvanceExec,
	}
}

func defaultAdvanceQuery(ctx context.Context, tx *sql.Tx, taskID string) (string, error) {
	var statusStr string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM workforce_tasks WHERE task_id = ?`, taskID,
	).Scan(&statusStr)
	return statusStr, err
}

func defaultAdvanceExec(ctx context.Context, tx *sql.Tx, newStatusStr string, now int64, taskID string) (sql.Result, error) {
	return tx.ExecContext(ctx,
		`UPDATE workforce_tasks SET status = ?, updated_at = ? WHERE task_id = ?`,
		newStatusStr, now, taskID,
	)
}

func defaultScopedAdvanceQuery(ctx context.Context, tx *sql.Tx, projectID, taskID string) (string, error) {
	var statusStr string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM workforce_tasks WHERE project_id = ? AND task_id = ?`,
		projectID, taskID,
	).Scan(&statusStr)
	return statusStr, err
}

func defaultScopedAdvanceExec(ctx context.Context, tx *sql.Tx, newStatusStr string, now int64, projectID, taskID string) (sql.Result, error) {
	return tx.ExecContext(ctx,
		`UPDATE workforce_tasks SET status = ?, updated_at = ? WHERE project_id = ? AND task_id = ?`,
		newStatusStr, now, projectID, taskID,
	)
}

func defaultScopedClaimQuery(ctx context.Context, tx *sql.Tx, projectID, taskID string) (string, error) {
	var statusStr string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM workforce_tasks WHERE project_id = ? AND task_id = ?`,
		projectID, taskID,
	).Scan(&statusStr)
	return statusStr, err
}

func defaultScopedClaimExec(ctx context.Context, tx *sql.Tx, threadID string, now int64, projectID, taskID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE workforce_tasks
		SET status = 'in_progress', thread_id = ?, updated_at = ?
		WHERE project_id = ? AND task_id = ?`,
		threadID, now, projectID, taskID,
	)
	return err
}

func defaultClaimQuery(ctx context.Context, tx *sql.Tx, taskID string) (string, error) {
	var statusStr string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM workforce_tasks WHERE task_id = ?`, taskID,
	).Scan(&statusStr)
	return statusStr, err
}

func defaultClaimExec(ctx context.Context, tx *sql.Tx, threadID string, now int64, taskID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE workforce_tasks
		SET status = 'in_progress', thread_id = ?, updated_at = ?
		WHERE task_id = ?`,
		threadID, now, taskID,
	)
	return err
}

func configureDurableConn(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("configureDurableConn %q: %w", p, err)
		}
	}
	return nil
}

func toUnixSec(t time.Time) int64 { return t.UTC().Unix() }

func fromUnixSec(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return queue.ErrDuplicateTask
	}
	return err
}

func (impl *SharedTaskListImpl) Enqueue(ctx context.Context, row queue.TaskRow) error {
	now := toUnixSec(time.Now().UTC())
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	_, err := impl.s.DB().ExecContext(ctx, `
		INSERT INTO workforce_tasks
			(task_id, project_id, title, description, status, thread_id,
			 priority, error_detail, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(row.TaskID), row.ProjectID, row.Title, row.Description,
		row.Status.String(), row.ThreadID, row.Priority, row.ErrorDetail,
		toUnixSec(row.CreatedAt), now,
	)
	return mapSQLiteError(err)
}

func (impl *SharedTaskListImpl) Claim(ctx context.Context, taskID queue.TaskID, threadID string) error {
	db := impl.s.DB()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("Claim begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statusStr, err := impl.claimQueryFn(ctx, tx, string(taskID))
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
	if err := impl.claimExecFn(ctx, tx, threadID, now, string(taskID)); err != nil {
		return fmt.Errorf("Claim update: %w", err)
	}
	return impl.claimCommitFn(tx)
}

// Advance implements queue.SharedTaskList.
//
// Enforces Kanban transitions in a single tx (read current → check
// IsValidTransition → write). Rejects invalid transitions with
// queue.ErrInvalidTransition (I-4 fix). For example, pending → done
// (skipping in_progress/review) returns ErrInvalidTransition; advancing
// from a terminal state (done, failed) likewise errors.
//
// Note this method does NOT filter by project_id and may match a row in
// any project that shares this task_id. Production callers MUST use
// ScopedTo(projectID).Advance to guarantee project isolation. See
// internal/workforce/queue/doc.go.
func (impl *SharedTaskListImpl) Advance(ctx context.Context, taskID queue.TaskID, newStatus queue.Status) error {
	db := impl.s.DB()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("Advance begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statusStr, err := impl.advanceQueryFn(ctx, tx, string(taskID))
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
		return fmt.Errorf("%w: %s -> %s",
			queue.ErrInvalidTransition, current.String(), newStatus.String())
	}

	now := toUnixSec(time.Now().UTC())
	res, err := impl.advanceExecFn(ctx, tx, newStatus.String(), now, string(taskID))
	if err != nil {
		return fmt.Errorf("Advance update: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return queue.ErrTaskNotFound
	}
	return impl.advanceCommitFn(tx)
}

func (impl *SharedTaskListImpl) Get(ctx context.Context, taskID queue.TaskID) (queue.TaskRow, error) {
	var r queue.TaskRow
	var statusStr string
	var createdAt, updatedAt int64
	var taskIDStr string
	err := impl.s.DB().QueryRowContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks WHERE task_id = ?`, string(taskID),
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

func (impl *SharedTaskListImpl) ListByStatus(ctx context.Context, projectID string, status queue.Status) ([]queue.TaskRow, error) {
	rows, err := impl.s.DB().QueryContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks
		WHERE project_id = ? AND status = ?
		ORDER BY priority ASC, created_at ASC`,
		projectID, status.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("ListByStatus: %w", err)
	}
	defer rows.Close()
	return impl.scannerFn(rows)
}

func (impl *SharedTaskListImpl) ByThread(ctx context.Context, threadID string) ([]queue.TaskRow, error) {
	rows, err := impl.s.DB().QueryContext(ctx, `
		SELECT task_id, project_id, title, description, status, thread_id,
		       priority, error_detail, created_at, updated_at
		FROM workforce_tasks
		WHERE thread_id = ?
		ORDER BY created_at ASC`,
		threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("ByThread: %w", err)
	}
	defer rows.Close()
	return impl.scannerFn(rows)
}

type rowsScanner interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
	Close() error
}

func scanTaskRows(rows rowsScanner) ([]queue.TaskRow, error) {
	var out []queue.TaskRow
	for rows.Next() {
		var r queue.TaskRow
		var statusStr, taskIDStr string
		var createdAt, updatedAt int64
		if err := rows.Scan(&taskIDStr, &r.ProjectID, &r.Title, &r.Description,
			&statusStr, &r.ThreadID, &r.Priority, &r.ErrorDetail,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanTaskRows: %w", err)
		}
		r.TaskID = queue.TaskID(taskIDStr)
		r.CreatedAt = fromUnixSec(createdAt)
		r.UpdatedAt = fromUnixSec(updatedAt)
		st, err := queue.ParseStatus(statusStr)
		if err != nil {
			return nil, fmt.Errorf("scanTaskRows parse status: %w", err)
		}
		r.Status = st
		out = append(out, r)
	}
	return out, rows.Err()
}

var _ queue.CheckpointQueue = (*CheckpointQueueImpl)(nil)

type CheckpointQueueImpl struct {
	s             *store.Store
	scannerFn     func(rowsScanner) ([]queue.Checkpoint, []int64, error)
	drainQueryFn  func(ctx context.Context, tx *sql.Tx, taskID string) (rowsScanner, error)
	drainExecFn   func(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error
	drainCommitFn func(tx *sql.Tx) error

	scopedDrainQueryFn func(ctx context.Context, tx *sql.Tx, projectID, taskID string) (rowsScanner, error)
}

func defaultCheckpointDrainQuery(ctx context.Context, tx *sql.Tx, taskID string) (rowsScanner, error) {
	return tx.QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE task_id = ? AND consumed = 0
		ORDER BY sequence_num ASC`, taskID)
}

func NewCheckpointQueue(s *store.Store) *CheckpointQueueImpl {
	if s == nil {
		panic("workforceadapter: NewCheckpointQueue: store must not be nil")
	}
	_ = queue.DurableQueueOpened()
	return &CheckpointQueueImpl{
		s:                  s,
		scannerFn:          scanCheckpoints,
		drainQueryFn:       defaultCheckpointDrainQuery,
		drainExecFn:        defaultCheckpointDrainExec,
		drainCommitFn:      (*sql.Tx).Commit,
		scopedDrainQueryFn: defaultScopedCheckpointDrainQuery,
	}
}

func defaultScopedCheckpointDrainQuery(ctx context.Context, tx *sql.Tx, projectID, taskID string) (rowsScanner, error) {
	return tx.QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE project_id = ? AND task_id = ? AND consumed = 0
		ORDER BY sequence_num ASC`, projectID, taskID)
}

func defaultCheckpointDrainExec(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE workforce_checkpoints SET consumed = 1 WHERE id IN (`+placeholders+`)`,
		args...,
	)
	return err
}

func (impl *CheckpointQueueImpl) Put(ctx context.Context, cp queue.Checkpoint) error {
	var deadlineVal interface{}
	if !cp.DeadlineAt.IsZero() {
		deadlineVal = toUnixSec(cp.DeadlineAt)
	}
	_, err := impl.s.DB().ExecContext(ctx, `
		INSERT INTO workforce_checkpoints
			(task_id, project_id, thread_id, state_json, sequence_num,
			 deadline_at, consumed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		string(cp.TaskID), cp.ProjectID, cp.ThreadID, cp.StateJSON,
		cp.SeqNum, deadlineVal, toUnixSec(cp.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("CheckpointQueue.Put: %w", err)
	}
	return nil
}

func (impl *CheckpointQueueImpl) Drain(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	db := impl.s.DB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("Drain begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := impl.drainQueryFn(ctx, tx, string(taskID))
	if err != nil {
		return nil, fmt.Errorf("Drain query: %w", err)
	}
	cps, ids, err := impl.scannerFn(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {

		return []queue.Checkpoint{}, tx.Commit()
	}

	if err := updateConsumedInBatches(ctx, tx, impl.drainExecFn, ids); err != nil {
		return nil, fmt.Errorf("Drain mark consumed: %w", err)
	}
	if err := impl.drainCommitFn(tx); err != nil {
		return nil, fmt.Errorf("Drain commit: %w", err)
	}
	for i := range cps {
		cps[i].Consumed = true
	}
	return cps, nil
}

func updateConsumedInBatches(
	ctx context.Context,
	tx *sql.Tx,
	execFn func(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error,
	ids []int64,
) error {
	for start := 0; start < len(ids); start += drainBatchSize {
		end := start + drainBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = strings.TrimSuffix(placeholders, ",")
		args := make([]interface{}, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		if err := execFn(ctx, tx, placeholders, args); err != nil {
			return err
		}
	}
	return nil
}

const drainBatchSize = 1000

func (impl *CheckpointQueueImpl) Peek(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	rows, err := impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE task_id = ? AND consumed = 0
		ORDER BY sequence_num ASC`,
		string(taskID),
	)
	if err != nil {
		return nil, fmt.Errorf("Peek: %w", err)
	}
	defer rows.Close()
	cps, _, err := scanCheckpoints(rows)
	return cps, err
}

func (impl *CheckpointQueueImpl) ByThread(ctx context.Context, threadID string) ([]queue.Checkpoint, error) {
	rows, err := impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, thread_id, state_json, sequence_num,
		       deadline_at, created_at
		FROM workforce_checkpoints
		WHERE thread_id = ?
		ORDER BY sequence_num ASC`,
		threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("ByThread: %w", err)
	}
	defer rows.Close()
	cps, _, err := scanCheckpoints(rows)
	return cps, err
}

func scanCheckpoints(rows rowsScanner) ([]queue.Checkpoint, []int64, error) {
	var cps []queue.Checkpoint
	var ids []int64
	for rows.Next() {
		var cp queue.Checkpoint
		var id int64
		var taskIDStr, threadID string
		var createdAt int64
		var deadlineAt sql.NullInt64
		if err := rows.Scan(&id, &taskIDStr, &cp.ProjectID, &threadID,
			&cp.StateJSON, &cp.SeqNum, &deadlineAt, &createdAt); err != nil {
			return nil, nil, fmt.Errorf("scanCheckpoints: %w", err)
		}
		cp.TaskID = queue.TaskID(taskIDStr)
		cp.ThreadID = threadID
		cp.CreatedAt = fromUnixSec(createdAt)
		if deadlineAt.Valid {
			cp.DeadlineAt = fromUnixSec(deadlineAt.Int64)
		}
		ids = append(ids, id)
		cps = append(cps, cp)
	}
	return cps, ids, rows.Err()
}

var _ queue.FixPromptQueue = (*FixPromptQueueImpl)(nil)

type FixPromptQueueImpl struct {
	s             *store.Store
	scannerFn     func(rowsScanner) ([]queue.FixPrompt, []int64, error)
	drainQueryFn  func(ctx context.Context, tx *sql.Tx, workerID string) (rowsScanner, error)
	drainExecFn   func(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error
	drainCommitFn func(tx *sql.Tx) error

	scopedDrainQueryFn func(ctx context.Context, tx *sql.Tx, projectID, workerID string) (rowsScanner, error)
}

func defaultFixPromptDrainQuery(ctx context.Context, tx *sql.Tx, workerID string) (rowsScanner, error) {
	return tx.QueryContext(ctx, `
		SELECT id, task_id, project_id, worker_id, reviewer_tier, prompt_text,
		       criteria_name, severity, created_at
		FROM workforce_fix_prompts
		WHERE worker_id = ? AND consumed = 0
		ORDER BY created_at ASC`, workerID)
}

func NewFixPromptQueue(s *store.Store) *FixPromptQueueImpl {
	if s == nil {
		panic("workforceadapter: NewFixPromptQueue: store must not be nil")
	}
	_ = queue.DurableQueueOpened()
	return &FixPromptQueueImpl{
		s:                  s,
		scannerFn:          scanFixPrompts,
		drainQueryFn:       defaultFixPromptDrainQuery,
		drainExecFn:        defaultFixPromptDrainExec,
		drainCommitFn:      (*sql.Tx).Commit,
		scopedDrainQueryFn: defaultScopedFixPromptDrainQuery,
	}
}

func defaultScopedFixPromptDrainQuery(ctx context.Context, tx *sql.Tx, projectID, workerID string) (rowsScanner, error) {
	return tx.QueryContext(ctx, `
		SELECT id, task_id, project_id, worker_id, reviewer_tier, prompt_text,
		       criteria_name, severity, created_at
		FROM workforce_fix_prompts
		WHERE project_id = ? AND worker_id = ? AND consumed = 0
		ORDER BY created_at ASC`, projectID, workerID)
}

func defaultFixPromptDrainExec(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE workforce_fix_prompts SET consumed = 1 WHERE id IN (`+placeholders+`)`,
		args...,
	)
	return err
}

func (impl *FixPromptQueueImpl) Put(ctx context.Context, fp queue.FixPrompt) error {
	_, err := impl.s.DB().ExecContext(ctx, `
		INSERT INTO workforce_fix_prompts
			(task_id, project_id, worker_id, reviewer_tier, prompt_text,
			 criteria_name, severity, consumed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		string(fp.TaskID), fp.ProjectID, fp.WorkerID,
		fp.ReviewerTier.String(), fp.PromptText,
		fp.CriteriaName, fp.Severity.String(),
		toUnixSec(fp.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("FixPromptQueue.Put: %w", err)
	}
	return nil
}

func (impl *FixPromptQueueImpl) DrainByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	db := impl.s.DB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("DrainByWorker begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := impl.drainQueryFn(ctx, tx, workerID)
	if err != nil {
		return nil, fmt.Errorf("DrainByWorker query: %w", err)
	}
	fps, ids, err := impl.scannerFn(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {

		return []queue.FixPrompt{}, tx.Commit()
	}
	if err := updateConsumedInBatches(ctx, tx, impl.drainExecFn, ids); err != nil {
		return nil, fmt.Errorf("DrainByWorker mark consumed: %w", err)
	}
	if err := impl.drainCommitFn(tx); err != nil {
		return nil, fmt.Errorf("DrainByWorker commit: %w", err)
	}
	for i := range fps {
		fps[i].Consumed = true
	}
	return fps, nil
}

func (impl *FixPromptQueueImpl) PendingByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	rows, err := impl.s.DB().QueryContext(ctx, `
		SELECT id, task_id, project_id, worker_id, reviewer_tier, prompt_text,
		       criteria_name, severity, created_at
		FROM workforce_fix_prompts
		WHERE worker_id = ? AND consumed = 0
		ORDER BY created_at ASC`,
		workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("PendingByWorker: %w", err)
	}
	defer rows.Close()
	fps, _, err := scanFixPrompts(rows)
	return fps, err
}

func scanFixPrompts(rows rowsScanner) ([]queue.FixPrompt, []int64, error) {
	var fps []queue.FixPrompt
	var ids []int64
	for rows.Next() {
		var fp queue.FixPrompt
		var id int64
		var taskIDStr, reviewerTierStr, severityStr string
		var createdAt int64
		if err := rows.Scan(&id, &taskIDStr, &fp.ProjectID, &fp.WorkerID,
			&reviewerTierStr, &fp.PromptText, &fp.CriteriaName,
			&severityStr, &createdAt); err != nil {
			return nil, nil, fmt.Errorf("scanFixPrompts: %w", err)
		}
		fp.TaskID = queue.TaskID(taskIDStr)
		fp.CreatedAt = fromUnixSec(createdAt)
		tier, err := queue.ParseReviewerTier(reviewerTierStr)
		if err != nil {
			return nil, nil, fmt.Errorf("scanFixPrompts parse reviewer_tier: %w", err)
		}
		fp.ReviewerTier = tier
		sev, err := queue.ParseSeverity(severityStr)
		if err != nil {
			return nil, nil, fmt.Errorf("scanFixPrompts parse severity: %w", err)
		}
		fp.Severity = sev
		ids = append(ids, id)
		fps = append(fps, fp)
	}
	return fps, ids, rows.Err()
}
