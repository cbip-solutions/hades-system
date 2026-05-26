// SPDX-License-Identifier: MIT
package chain

import (
	"context"
	"errors"
	"fmt"
)

type BackfillReport struct {
	RowsBackfilled int64
	BatchesRun     int
}

func Backfill(ctx context.Context, store EventStore, batchSize int) (BackfillReport, error) {
	if batchSize <= 0 {
		return BackfillReport{}, fmt.Errorf("chain.Backfill: batchSize must be > 0, got %d", batchSize)
	}

	report := BackfillReport{}

	prevHash, err := store.GetChainTip(ctx)
	if err != nil && !errors.Is(err, ErrNoChainTip) {
		return report, fmt.Errorf("chain.Backfill: get chain tip: %w", err)
	}
	if errors.Is(err, ErrNoChainTip) {
		prevHash = ""
	}

	var afterRowID int64
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		batch, err := store.BackfillScan(ctx, afterRowID, batchSize)
		if err != nil {
			return report, fmt.Errorf("chain.Backfill: scan after rowid %d: %w", afterRowID, err)
		}
		if len(batch) == 0 {
			break
		}
		report.BatchesRun++

		for _, r := range batch {
			if err := ctx.Err(); err != nil {
				return report, err
			}

			partitionID := PartitionID(r.EmittedAt)
			h, err := Compute(prevHash, r.Type, []byte(r.PayloadJSON), r.EmittedAt)
			if err != nil {
				return report, fmt.Errorf("chain.Backfill: compute hash for row %s: %w", r.ID, err)
			}

			if err := store.UpdateChainColumns(ctx, r.ID, prevHash, h, partitionID); err != nil {
				return report, fmt.Errorf("chain.Backfill: update chain for row %s: %w", r.ID, err)
			}
			prevHash = h
			afterRowID = r.RowID
			report.RowsBackfilled++
		}
	}

	return report, nil
}
