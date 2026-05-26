package workforceadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/stream"
)

type errRowsScanner struct{}

func (e *errRowsScanner) Next() bool                  { return true }
func (e *errRowsScanner) Scan(_ ...interface{}) error { return errors.New("mock scan error") }
func (e *errRowsScanner) Err() error                  { return nil }
func (e *errRowsScanner) Close() error                { return nil }

func ExportScanTaskRowsError() error {
	_, err := scanTaskRows(&errRowsScanner{})
	return err
}

func ExportScanCheckpointsError() error {
	_, _, err := scanCheckpoints(&errRowsScanner{})
	return err
}

func ExportScanFixPromptsError() error {
	_, _, err := scanFixPrompts(&errRowsScanner{})
	return err
}

func ExportNewSharedTaskListWithFailClaimQuery(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.claimQueryFn = func(_ context.Context, _ *sql.Tx, _ string) (string, error) {
		return "", errors.New("injected query error")
	}
	return impl
}

func ExportNewSharedTaskListWithFailClaimExec(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.claimExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _ string) error {
		return errors.New("injected claim exec error")
	}
	return impl
}

func ExportNewCheckpointQueueWithFailDrainQuery(s *store.Store) *CheckpointQueueImpl {
	impl := NewCheckpointQueue(s)
	impl.drainQueryFn = func(_ context.Context, _ *sql.Tx, _ string) (rowsScanner, error) {
		return nil, errors.New("injected drain query error")
	}
	return impl
}

func ExportNewCheckpointQueueWithFailDrainExec(s *store.Store) *CheckpointQueueImpl {
	impl := NewCheckpointQueue(s)
	impl.drainExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		return errors.New("injected drain exec error")
	}
	return impl
}

func ExportNewFixPromptQueueWithFailDrainQuery(s *store.Store) *FixPromptQueueImpl {
	impl := NewFixPromptQueue(s)
	impl.drainQueryFn = func(_ context.Context, _ *sql.Tx, _ string) (rowsScanner, error) {
		return nil, errors.New("injected drain query error")
	}
	return impl
}

func ExportNewFixPromptQueueWithFailDrainExec(s *store.Store) *FixPromptQueueImpl {
	impl := NewFixPromptQueue(s)
	impl.drainExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		return errors.New("injected drain exec error")
	}
	return impl
}

func ExportNewSharedTaskListWithFailClaimCommit(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.claimCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected commit error")
	}
	return impl
}

func ExportNewCheckpointQueueWithFailDrainCommit(s *store.Store) *CheckpointQueueImpl {
	impl := NewCheckpointQueue(s)
	impl.drainCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected drain commit error")
	}
	return impl
}

func ExportNewFixPromptQueueWithFailDrainCommit(s *store.Store) *FixPromptQueueImpl {
	impl := NewFixPromptQueue(s)
	impl.drainCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected drain commit error")
	}
	return impl
}

func ExportNewSharedTaskListWithFailScan(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.TaskRow, error) {
		return nil, errors.New("injected scan error")
	}
	return impl
}

func ExportNewCheckpointQueueWithFailScan(s *store.Store) *CheckpointQueueImpl {
	impl := NewCheckpointQueue(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.Checkpoint, []int64, error) {
		return nil, nil, errors.New("injected scan error")
	}
	return impl
}

func ExportNewFixPromptQueueWithFailScan(s *store.Store) *FixPromptQueueImpl {
	impl := NewFixPromptQueue(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.FixPrompt, []int64, error) {
		return nil, nil, errors.New("injected scan error")
	}
	return impl
}

func ExportNewScopedSharedTaskListWithFailClaimQuery(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scopedClaimQueryFn = func(_ context.Context, _ *sql.Tx, _, _ string) (string, error) {
		return "", errors.New("injected scoped query error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithFailClaimExec(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scopedClaimExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _, _ string) error {
		return errors.New("injected scoped claim exec error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithFailClaimCommit(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.claimCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected scoped commit error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithFailScan(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.TaskRow, error) {
		return nil, errors.New("injected scan error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedCheckpointQueueWithFailDrainQuery(s *store.Store, projectID string) *ScopedCheckpointQueue {
	impl := NewCheckpointQueue(s)
	impl.scopedDrainQueryFn = func(_ context.Context, _ *sql.Tx, _, _ string) (rowsScanner, error) {
		return nil, errors.New("injected scoped drain query error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedCheckpointQueueWithFailDrainExec(s *store.Store, projectID string) *ScopedCheckpointQueue {
	impl := NewCheckpointQueue(s)
	impl.drainExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		return errors.New("injected scoped drain exec error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedCheckpointQueueWithFailDrainCommit(s *store.Store, projectID string) *ScopedCheckpointQueue {
	impl := NewCheckpointQueue(s)
	impl.drainCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected scoped drain commit error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedCheckpointQueueWithFailScan(s *store.Store, projectID string) *ScopedCheckpointQueue {
	impl := NewCheckpointQueue(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.Checkpoint, []int64, error) {
		return nil, nil, errors.New("injected scoped scan error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedFixPromptQueueWithFailDrainQuery(s *store.Store, projectID string) *ScopedFixPromptQueue {
	impl := NewFixPromptQueue(s)
	impl.scopedDrainQueryFn = func(_ context.Context, _ *sql.Tx, _, _ string) (rowsScanner, error) {
		return nil, errors.New("injected scoped drain query error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedFixPromptQueueWithFailDrainExec(s *store.Store, projectID string) *ScopedFixPromptQueue {
	impl := NewFixPromptQueue(s)
	impl.drainExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		return errors.New("injected scoped drain exec error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedFixPromptQueueWithFailDrainCommit(s *store.Store, projectID string) *ScopedFixPromptQueue {
	impl := NewFixPromptQueue(s)
	impl.drainCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected scoped drain commit error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedFixPromptQueueWithFailScan(s *store.Store, projectID string) *ScopedFixPromptQueue {
	impl := NewFixPromptQueue(s)
	impl.scannerFn = func(_ rowsScanner) ([]queue.FixPrompt, []int64, error) {
		return nil, nil, errors.New("injected scoped scan error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewStreamAdapterWithFailOpenWindow(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.openWindowExecFn = func(_ context.Context, _ stream.Layer, _ int64) (int64, error) {
		return 0, errors.New("injected OpenWindow error")
	}
	return a
}

func ExportNewStreamAdapterWithFailAppendEvent(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.appendEventExecFn = func(_ context.Context, _ int64, _ stream.Event) error {
		return errors.New("injected AppendEvent error")
	}
	return a
}

func ExportNewStreamAdapterWithFailCloseWindow(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.closeWindowExecFn = func(_ context.Context, _ int64, _ int64, _ int) (int64, error) {
		return 0, errors.New("injected CloseWindow error")
	}
	return a
}

func ExportNewStreamAdapterWithFailLoadWindows(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.loadWindowsQueryFn = func(_ context.Context) ([]stream.WindowRecord, error) {
		return nil, errors.New("injected LoadOpenWindows error")
	}
	return a
}

func ExportNewStreamAdapterWithZeroCloseRows(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.closeWindowExecFn = func(_ context.Context, _ int64, _ int64, _ int) (int64, error) {
		return 0, nil
	}
	return a
}

func ExportNewStreamAdapterWithScanError(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.loadWindowsQueryFn = func(_ context.Context) ([]stream.WindowRecord, error) {
		return nil, errors.New("injected scan error")
	}
	return a
}

func ExportNewStreamAdapterWithCloseWindowSuccess(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.closeWindowExecFn = func(_ context.Context, _ int64, _ int64, _ int) (int64, error) {
		return 1, nil
	}
	return a
}

func ExportNewStreamAdapterWithDisambiguateSelectError(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.closeDisambiguateQueryFn = func(_ context.Context, _ int64) (string, error) {
		return "", errors.New("injected disambiguate error")
	}
	return a
}

func ExportNewStreamAdapterWithDisambiguateUnexpectedStatus(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.closeDisambiguateQueryFn = func(_ context.Context, _ int64) (string, error) {
		return "phantom_status", nil
	}
	return a
}

func ExportNewGateAdapterWithFailLoadState(s *store.Store) *GateAdapter {
	a := NewGateAdapter(s)
	a.loadStateQueryFn = func(_ context.Context) (gate.State, error) {
		return gate.StateRunning, errors.New("injected LoadState error")
	}
	return a
}

func ExportNewGateAdapterWithFailSaveState(s *store.Store) *GateAdapter {
	a := NewGateAdapter(s)
	a.saveStateExecFn = func(_ context.Context, _ gate.State, _ string) error {
		return errors.New("injected SaveState error")
	}
	return a
}

func ExportNewGateAdapterWithUnrecognizedStateSeam(s *store.Store) *GateAdapter {
	a := NewGateAdapter(s)
	a.scanStateResultFn = func() (string, error) {
		return "unknown_state_xyz", nil
	}
	return a
}

func ExportNewStreamAdapterWithRealScanError(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.loadWindowsQueryFn = func(_ context.Context) ([]stream.WindowRecord, error) {
		return nil, fmt.Errorf("stream_adapter.LoadOpenWindows scan: %w",
			errors.New("simulated scan error in real path"))
	}
	return a
}

func ExportNewStreamAdapterWithScanRowError(s *store.Store) *StreamAdapter {
	a := NewStreamAdapter(s)
	a.scanWindowRowFn = func(_ ...interface{}) error {
		return errors.New("injected row scan error")
	}
	return a
}

func ExportNewSharedTaskListWithFailAdvanceQuery(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.advanceQueryFn = func(_ context.Context, _ *sql.Tx, _ string) (string, error) {
		return "", errors.New("injected advance query error")
	}
	return impl
}

type fakeResult struct{ rows int64 }

func (f fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeResult) RowsAffected() (int64, error) { return f.rows, nil }

func ExportNewSharedTaskListWithFailAdvanceExec(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.advanceExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _ string) (sql.Result, error) {
		return nil, errors.New("injected advance exec error")
	}
	return impl
}

func ExportNewSharedTaskListWithAdvanceZeroAffected(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.advanceExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _ string) (sql.Result, error) {
		return fakeResult{rows: 0}, nil
	}
	return impl
}

func ExportNewSharedTaskListWithFailAdvanceCommit(s *store.Store) *SharedTaskListImpl {
	impl := NewSharedTaskList(s)
	impl.advanceCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected advance commit error")
	}
	return impl
}

func ExportDrainBatchSize() int { return drainBatchSize }

func ExportCallUpdateConsumedInBatches(
	ctx context.Context,
	execFn func(ctx context.Context, tx *sql.Tx, placeholders string, args []interface{}) error,
	ids []int64,
) error {
	return updateConsumedInBatches(ctx, nil, execFn, ids)
}

func ExportNewScopedSharedTaskListWithFailAdvanceQuery(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scopedAdvanceQueryFn = func(_ context.Context, _ *sql.Tx, _, _ string) (string, error) {
		return "", errors.New("injected scoped advance query error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithFailAdvanceExec(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scopedAdvanceExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _, _ string) (sql.Result, error) {
		return nil, errors.New("injected scoped advance exec error")
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithAdvanceZeroAffected(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.scopedAdvanceExecFn = func(_ context.Context, _ *sql.Tx, _ string, _ int64, _, _ string) (sql.Result, error) {
		return fakeResult{rows: 0}, nil
	}
	return impl.ScopedTo(projectID)
}

func ExportNewScopedSharedTaskListWithFailAdvanceCommit(s *store.Store, projectID string) *ScopedSharedTaskList {
	impl := NewSharedTaskList(s)
	impl.advanceCommitFn = func(_ *sql.Tx) error {
		return errors.New("injected scoped advance commit error")
	}
	return impl.ScopedTo(projectID)
}
