// SPDX-License-Identifier: MIT
package chain

type EventRow struct {
	ID            string
	ProjectID     string
	Type          string
	PayloadJSON   string
	EmittedAt     int64
	PrevHash      string
	RecordHash    string
	PartitionID   string
	TesseraLeafID *string
}

type SealRecord struct {
	PartitionID            string
	SealedAt               int64
	FinalRecordHash        string
	TesseraSealLeafID      string
	DaemonWitnessSignature string
	ColdArchiveURL         string
	ColdArchiveContentHash string
}

type PartitionStat struct {
	PartitionID     string
	FirstID         string
	LastID          string
	EventCount      int64
	FinalRecordHash string
}

type BackfillCursorRow struct {
	RowID int64
	EventRow
}
