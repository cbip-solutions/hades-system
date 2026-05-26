package workforceadapter_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
)

func TestUpdateConsumedInBatches_NoChunkingNeeded(t *testing.T) {
	ctx := context.Background()
	calls := 0
	execFn := func(_ context.Context, _ *sql.Tx, _ string, args []interface{}) error {
		calls++
		if len(args) == 0 {
			t.Errorf("empty batch")
		}
		return nil
	}
	ids := []int64{1, 2, 3}
	if err := workforceadapter.ExportCallUpdateConsumedInBatches(ctx, execFn, ids); err != nil {
		t.Fatalf("err: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (single batch)", calls)
	}
}

func TestUpdateConsumedInBatches_ChunksLargeSlices(t *testing.T) {
	ctx := context.Background()
	batchSize := workforceadapter.ExportDrainBatchSize()
	if batchSize <= 1 {
		t.Skip("nothing to test with batchSize <= 1")
	}

	n := batchSize + 10
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	var sizes []int
	execFn := func(_ context.Context, _ *sql.Tx, _ string, args []interface{}) error {
		sizes = append(sizes, len(args))
		return nil
	}
	if err := workforceadapter.ExportCallUpdateConsumedInBatches(ctx, execFn, ids); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sizes) != 2 {
		t.Fatalf("calls = %d, want 2 (batches of %d + 10)", len(sizes), batchSize)
	}
	if sizes[0] != batchSize {
		t.Errorf("batch[0] size = %d, want %d", sizes[0], batchSize)
	}
	if sizes[1] != 10 {
		t.Errorf("batch[1] size = %d, want 10", sizes[1])
	}
}

func TestUpdateConsumedInBatches_PropagatesError(t *testing.T) {
	ctx := context.Background()
	want := errors.New("injected")
	execFn := func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		return want
	}
	ids := []int64{1}
	got := workforceadapter.ExportCallUpdateConsumedInBatches(ctx, execFn, ids)
	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUpdateConsumedInBatches_EmptyIDs(t *testing.T) {
	ctx := context.Background()
	calls := 0
	execFn := func(_ context.Context, _ *sql.Tx, _ string, _ []interface{}) error {
		calls++
		return nil
	}
	if err := workforceadapter.ExportCallUpdateConsumedInBatches(ctx, execFn, nil); err != nil {
		t.Errorf("empty ids err = %v, want nil", err)
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 for empty ids", calls)
	}
}
