// SPDX-License-Identifier: MIT
package auditadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	s           *store.Store
	tessera     TesseraAdapter
	s3          S3Client
	litestream  LitestreamMgr
	coldArchive ColdArchiver
}

type Option func(*Adapter)

func New(s *store.Store, opts ...Option) *Adapter {
	if s == nil {
		panic("auditadapter.New: store is nil")
	}
	a := &Adapter{s: s}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (a *Adapter) GetChainTip(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash, err := a.s.GetChainTip()
	if errors.Is(err, store.ErrNoChainTip) {
		return "", chain.ErrNoChainTip
	}
	if err != nil {
		return "", fmt.Errorf("auditadapter: get chain tip: %w", err)
	}
	return hash, nil
}

func (a *Adapter) GetEventByID(ctx context.Context, id string) (*chain.EventRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := a.s.GetEventByID(id)
	if errors.Is(err, store.ErrEventNotFound) {
		return nil, chain.ErrEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auditadapter: get event by id: %w", err)
	}
	return storeToChainEventRow(r), nil
}

func (a *Adapter) GetByEventID(ctx context.Context, eventID string) (*chain.EventRow, error) {
	return a.GetEventByID(ctx, eventID)
}

func (a *Adapter) UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.s.UpdateChainColumns(id, prevHash, recordHash, partitionID); err != nil {
		return fmt.Errorf("auditadapter: update chain columns: %w", err)
	}
	return nil
}

func (a *Adapter) UpdateTesseraLeafID(ctx context.Context, id, leafID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.s.UpdateTesseraLeafID(id, leafID); err != nil {
		return fmt.Errorf("auditadapter: update tessera leaf id: %w", err)
	}
	return nil
}

func (a *Adapter) InsertPartitionSeal(ctx context.Context, seal chain.SealRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	row := store.AuditPartitionSealRow{
		PartitionID:            seal.PartitionID,
		SealedAt:               seal.SealedAt,
		FinalRecordHash:        seal.FinalRecordHash,
		TesseraSealLeafID:      seal.TesseraSealLeafID,
		DaemonWitnessSignature: seal.DaemonWitnessSignature,
	}
	if seal.ColdArchiveURL != "" {
		row.ColdArchiveURL.String = seal.ColdArchiveURL
		row.ColdArchiveURL.Valid = true
	}
	if seal.ColdArchiveContentHash != "" {
		row.ColdArchiveContentHash.String = seal.ColdArchiveContentHash
		row.ColdArchiveContentHash.Valid = true
	}
	if err := a.s.InsertPartitionSeal(row); err != nil {
		return fmt.Errorf("auditadapter: insert partition seal: %w", err)
	}
	return nil
}

func (a *Adapter) GetPartitionSeal(ctx context.Context, partitionID string) (*chain.SealRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := a.s.GetPartitionSeal(partitionID)
	if errors.Is(err, store.ErrPartitionSealNotFound) {
		return nil, chain.ErrPartitionSealNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auditadapter: get partition seal: %w", err)
	}
	return storeToChainSealRecord(r), nil
}

func (a *Adapter) ListPartitions(ctx context.Context) ([]chain.PartitionStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stats, err := a.s.ListPartitions()
	if err != nil {
		return nil, fmt.Errorf("auditadapter: list partitions: %w", err)
	}
	out := make([]chain.PartitionStat, len(stats))
	for i, s := range stats {
		out[i] = chain.PartitionStat{
			PartitionID:     s.PartitionID,
			FirstID:         s.FirstID,
			LastID:          s.LastID,
			EventCount:      s.EventCount,
			FinalRecordHash: s.FinalRecordHash,
		}
	}
	return out, nil
}

func (a *Adapter) ListEventsForPartition(ctx context.Context, partitionID string) ([]chain.EventRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := a.s.ListEventsForPartition(partitionID)
	if err != nil {
		return nil, fmt.Errorf("auditadapter: list events for partition: %w", err)
	}
	out := make([]chain.EventRow, len(rows))
	for i := range rows {
		out[i] = *storeToChainEventRow(&rows[i])
	}
	return out, nil
}

func (a *Adapter) BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]chain.BackfillCursorRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := a.s.BackfillScan(afterRowID, limit)
	if err != nil {
		return nil, fmt.Errorf("auditadapter: backfill scan: %w", err)
	}
	out := make([]chain.BackfillCursorRow, len(rows))
	for i := range rows {
		out[i] = chain.BackfillCursorRow{
			RowID:    rows[i].RowID,
			EventRow: *storeToChainEventRow(&rows[i].AuditChainEventRow),
		}
	}
	return out, nil
}

func (a *Adapter) OnEmitRaw(ctx context.Context, eventID, projectID, eventType string, payload []byte, emittedAt int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tip, err := a.GetChainTip(ctx)
	if err != nil && !errors.Is(err, chain.ErrNoChainTip) {
		return "", fmt.Errorf("auditadapter.OnEmitRaw: get chain tip: %w", err)
	}
	if errors.Is(err, chain.ErrNoChainTip) {
		tip = ""
	}
	h, err := chain.Compute(tip, eventType, payload, emittedAt)
	if err != nil {
		return "", fmt.Errorf("auditadapter.OnEmitRaw: compute hash: %w", err)
	}
	partitionID := chain.PartitionID(emittedAt)
	if err := a.UpdateChainColumns(ctx, eventID, tip, h, partitionID); err != nil {
		return "", fmt.Errorf("auditadapter.OnEmitRaw: update chain columns: %w", err)
	}

	payloadHash := sha256Bytes(payload)
	recordHashBytes, herr := decodeChainHash(h)
	if herr != nil {
		return "", fmt.Errorf("auditadapter.OnEmitRaw: %w", herr)
	}
	leafID, derr := a.dispatchTessera(ctx, projectID, eventID, payloadHash, recordHashBytes)
	if derr == nil && leafID != "" {

		_ = a.OnTesseraBatchFlushed(ctx, eventID, string(leafID))
	}

	return h, nil
}

func (a *Adapter) OnTesseraBatchFlushed(ctx context.Context, eventID, leafID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.UpdateTesseraLeafID(ctx, eventID, leafID)
}

func storeToChainEventRow(r *store.AuditChainEventRow) *chain.EventRow {
	out := &chain.EventRow{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Type:        r.Type,
		PayloadJSON: r.PayloadJSON,
		EmittedAt:   r.EmittedAt,
		PrevHash:    r.PrevHash,
		RecordHash:  r.RecordHash,
		PartitionID: r.PartitionID,
	}
	if r.TesseraLeafID.Valid {
		s := r.TesseraLeafID.String
		out.TesseraLeafID = &s
	}
	return out
}

func storeToChainSealRecord(r *store.AuditPartitionSealRow) *chain.SealRecord {
	out := &chain.SealRecord{
		PartitionID:            r.PartitionID,
		SealedAt:               r.SealedAt,
		FinalRecordHash:        r.FinalRecordHash,
		TesseraSealLeafID:      r.TesseraSealLeafID,
		DaemonWitnessSignature: r.DaemonWitnessSignature,
	}
	if r.ColdArchiveURL.Valid {
		out.ColdArchiveURL = r.ColdArchiveURL.String
	}
	if r.ColdArchiveContentHash.Valid {
		out.ColdArchiveContentHash = r.ColdArchiveContentHash.String
	}
	return out
}

func sha256Bytes(input []byte) []byte {
	h := sha256.Sum256(input)
	return h[:]
}

func decodeChainHash(h string) ([]byte, error) {
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("chain hash %q is not valid hex: %w", h, err)
	}
	return b, nil
}
