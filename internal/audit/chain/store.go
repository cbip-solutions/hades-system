// SPDX-License-Identifier: MIT
package chain

import "context"

type EventStore interface {
	GetChainTip(ctx context.Context) (string, error)
	GetEventByID(ctx context.Context, id string) (*EventRow, error)

	GetByEventID(ctx context.Context, eventID string) (*EventRow, error)
	UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error
	UpdateTesseraLeafID(ctx context.Context, id, leafID string) error
	InsertPartitionSeal(ctx context.Context, seal SealRecord) error
	GetPartitionSeal(ctx context.Context, partitionID string) (*SealRecord, error)
	ListPartitions(ctx context.Context) ([]PartitionStat, error)
	ListEventsForPartition(ctx context.Context, partitionID string) ([]EventRow, error)
	BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]BackfillCursorRow, error)
}

var ErrNoChainTip = chainErr{"no chain tip (audit_events_raw empty)"}

var ErrEventNotFound = chainErr{"audit event not found"}

var ErrPartitionSealNotFound = chainErr{"partition seal not found"}

type chainErr struct{ msg string }

func (e chainErr) Error() string { return "chain: " + e.msg }
