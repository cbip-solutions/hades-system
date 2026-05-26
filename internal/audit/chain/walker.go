// SPDX-License-Identifier: MIT
package chain

import (
	"context"
	"fmt"
)

type WalkReport struct {
	ProjectID        string
	PartitionsWalked int
	EventsWalked     int64
	Tampered         []TamperRecord
	GapsDetected     []GapRecord
}

type TamperRecord struct {
	EventID      string
	PartitionID  string
	ExpectedHash string
	StoredHash   string
}

type GapRecord struct {
	EventID          string
	PartitionID      string
	ExpectedPrevHash string
	StoredPrevHash   string
}

type Walker struct {
	store EventStore
}

func NewWalker(store EventStore) *Walker {
	return &Walker{store: store}
}

func (w *Walker) Walk(ctx context.Context, projectID string) (WalkReport, error) {
	return Walk(ctx, w.store, projectID)
}

func Walk(ctx context.Context, store EventStore, projectID string) (WalkReport, error) {
	if err := ctx.Err(); err != nil {
		return WalkReport{}, err
	}

	report := WalkReport{ProjectID: projectID}

	parts, err := store.ListPartitions(ctx)
	if err != nil {
		return report, fmt.Errorf("chain.Walk: list partitions: %w", err)
	}

	for _, ps := range parts {
		if err := ctx.Err(); err != nil {
			return report, err
		}

		events, err := store.ListEventsForPartition(ctx, ps.PartitionID)
		if err != nil {
			return report, fmt.Errorf("chain.Walk: list events for %q: %w", ps.PartitionID, err)
		}

		filtered := make([]EventRow, 0, len(events))
		for _, e := range events {
			if e.ProjectID == projectID {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		report.PartitionsWalked++

		var prevHash string

		for i, e := range filtered {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			report.EventsWalked++

			// Within-partition linkage: from second event onward, the
			// row's stored prev_hash MUST equal the prior row's
			// record_hash. Gap detection runs FIRST and independently
			// of Compute so a tampered prev_hash that breaks Compute's
			// input contract still surfaces as a gap (forensic
			// completeness — operators see BOTH symptoms when both apply).
			if i > 0 && e.PrevHash != prevHash {
				report.GapsDetected = append(report.GapsDetected, GapRecord{
					EventID:          e.ID,
					PartitionID:      ps.PartitionID,
					ExpectedPrevHash: prevHash,
					StoredPrevHash:   e.PrevHash,
				})
			}

			expected, err := Compute(e.PrevHash, e.Type, []byte(e.PayloadJSON), e.EmittedAt)
			if err != nil {

				report.Tampered = append(report.Tampered, TamperRecord{
					EventID:      e.ID,
					PartitionID:  ps.PartitionID,
					ExpectedHash: "<compute_failed>",
					StoredHash:   e.RecordHash,
				})
				prevHash = e.RecordHash
				continue
			}
			if expected != e.RecordHash {
				report.Tampered = append(report.Tampered, TamperRecord{
					EventID:      e.ID,
					PartitionID:  ps.PartitionID,
					ExpectedHash: expected,
					StoredHash:   e.RecordHash,
				})
			}
			prevHash = e.RecordHash
		}
	}

	return report, nil
}
