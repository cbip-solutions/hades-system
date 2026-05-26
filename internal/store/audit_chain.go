// SPDX-License-Identifier: MIT
// Package store — audit_chain.go
//
// CRUD wrappers for Plan 9 Phase B chain integrity columns on
// audit_events_raw + the audit_partition_seals table.
//
// Boundary (inv-zen-031): the chain layer (internal/audit/chain) NEVER
// imports this file directly — it goes through internal/daemon/auditadapter
// which translates between chain.* value types and store row types via
// field-by-field copy.
//
// All chain-related UPDATEs are permitted by the migration 059 REFUSE
// triggers ONLY when:
//   - The append-only columns (id/project_id/type/payload_json/emitted_at)
//     are NOT touched
//   - The chain hash columns transition from ” to non-empty (one-time write)
//   - The partition_id transitions from ” to non-empty (one-time write)
//   - The tessera_leaf_id transitions from NULL to non-NULL (one-time write)
//
// Phase B audit_chain.go scope:
//   - GetChainTip          — read prev_hash for chain compute
//   - GetEventByID         — read full row for chain walker / verify
//   - UpdateChainColumns   — set prev_hash + record_hash + partition_id (one-time)
//   - UpdateTesseraLeafID  — set tessera_leaf_id (one-time, post-batch)
//   - InsertPartitionSeal  — write monthly seal record
//   - GetPartitionSeal     — read seal by partition_id
//   - ListPartitions       — query audit_events_partitions view
//   - BackfillScan         — id-ordered cursor for one-time backfill
//   - ListEventsForPartition — id-ordered events in a partition (verify-chain)
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

type AuditChainEventRow struct {
	ID            string
	ProjectID     string
	Type          string
	PayloadJSON   string
	EmittedAt     int64
	PrevHash      string
	RecordHash    string
	PartitionID   string
	TesseraLeafID sql.NullString
}

type AuditPartitionSealRow struct {
	PartitionID            string
	SealedAt               int64
	FinalRecordHash        string
	TesseraSealLeafID      string
	DaemonWitnessSignature string
	ColdArchiveURL         sql.NullString
	ColdArchiveContentHash sql.NullString
}

type AuditPartitionStat struct {
	PartitionID     string
	FirstID         string
	LastID          string
	EventCount      int64
	FinalRecordHash string
}

type AuditChainBackfillRow struct {
	RowID int64
	AuditChainEventRow
}

var ErrEventNotFound = errors.New("store: audit event not found")

var ErrNoChainTip = errors.New("store: no chain tip (audit_events_raw empty)")

var ErrPartitionSealNotFound = errors.New("store: partition seal not found")

func (s *Store) GetChainTip() (string, error) {
	var hash string
	err := s.db.QueryRow(
		`SELECT record_hash FROM audit_events_raw
		 WHERE record_hash != ''
		 ORDER BY rowid DESC LIMIT 1`,
	).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoChainTip
	}
	if err != nil {
		return "", fmt.Errorf("store: get chain tip: %w", err)
	}
	return hash, nil
}

func (s *Store) GetEventByID(id string) (*AuditChainEventRow, error) {
	row := s.db.QueryRow(
		`SELECT id, project_id, type, payload_json, emitted_at,
		        prev_hash, record_hash, partition_id, tessera_leaf_id
		 FROM audit_events_raw WHERE id = ?`, id,
	)
	r := &AuditChainEventRow{}
	err := row.Scan(&r.ID, &r.ProjectID, &r.Type, &r.PayloadJSON, &r.EmittedAt,
		&r.PrevHash, &r.RecordHash, &r.PartitionID, &r.TesseraLeafID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get event by id: %w", err)
	}
	return r, nil
}

func (s *Store) UpdateChainColumns(id, prevHash, recordHash, partitionID string) error {
	res, err := s.db.Exec(
		`UPDATE audit_events_raw
		 SET prev_hash = ?, record_hash = ?, partition_id = ?
		 WHERE id = ? AND prev_hash = '' AND record_hash = '' AND partition_id = ''`,
		prevHash, recordHash, partitionID, id,
	)
	if err != nil {
		return fmt.Errorf("store: update chain columns: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update chain columns rows-affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: update chain columns: no row matched id=%q with empty chain (already chained or not found)", id)
	}
	return nil
}

func (s *Store) UpdateTesseraLeafID(id, leafID string) error {
	res, err := s.db.Exec(
		`UPDATE audit_events_raw
		 SET tessera_leaf_id = ?
		 WHERE id = ? AND tessera_leaf_id IS NULL`,
		leafID, id,
	)
	if err != nil {
		return fmt.Errorf("store: update tessera leaf id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update tessera leaf id rows-affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: update tessera leaf id: no row matched id=%q with NULL leaf", id)
	}
	return nil
}

func (s *Store) InsertPartitionSeal(row AuditPartitionSealRow) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_partition_seals
		   (partition_id, sealed_at, final_record_hash, tessera_seal_leaf_id,
		    daemon_witness_signature, cold_archive_url, cold_archive_content_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.PartitionID, row.SealedAt, row.FinalRecordHash, row.TesseraSealLeafID,
		row.DaemonWitnessSignature, row.ColdArchiveURL, row.ColdArchiveContentHash,
	)
	if err != nil {
		return fmt.Errorf("store: insert partition seal: %w", err)
	}
	return nil
}

func (s *Store) GetPartitionSeal(partitionID string) (*AuditPartitionSealRow, error) {
	r := &AuditPartitionSealRow{}
	err := s.db.QueryRow(
		`SELECT partition_id, sealed_at, final_record_hash, tessera_seal_leaf_id,
		        daemon_witness_signature, cold_archive_url, cold_archive_content_hash
		 FROM audit_partition_seals WHERE partition_id = ?`, partitionID,
	).Scan(&r.PartitionID, &r.SealedAt, &r.FinalRecordHash, &r.TesseraSealLeafID,
		&r.DaemonWitnessSignature, &r.ColdArchiveURL, &r.ColdArchiveContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPartitionSealNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get partition seal: %w", err)
	}
	return r, nil
}

func (s *Store) ListPartitions() ([]AuditPartitionStat, error) {
	rows, err := s.db.Query(
		`SELECT partition_id, first_id, last_id, event_count, final_record_hash
		 FROM audit_events_partitions
		 ORDER BY partition_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list partitions: %w", err)
	}
	defer rows.Close()
	var out []AuditPartitionStat
	for rows.Next() {
		var p AuditPartitionStat
		if err := rows.Scan(&p.PartitionID, &p.FirstID, &p.LastID, &p.EventCount, &p.FinalRecordHash); err != nil {
			return nil, fmt.Errorf("store: scan partition: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list partitions iter: %w", err)
	}
	return out, nil
}

func (s *Store) ListEventsForPartition(partitionID string) ([]AuditChainEventRow, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, type, payload_json, emitted_at,
		        prev_hash, record_hash, partition_id, tessera_leaf_id
		 FROM audit_events_raw
		 WHERE partition_id = ?
		 ORDER BY rowid ASC`, partitionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list events for partition: %w", err)
	}
	defer rows.Close()
	var out []AuditChainEventRow
	for rows.Next() {
		var r AuditChainEventRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Type, &r.PayloadJSON, &r.EmittedAt,
			&r.PrevHash, &r.RecordHash, &r.PartitionID, &r.TesseraLeafID); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list events for partition iter: %w", err)
	}
	return out, nil
}

func (s *Store) BackfillScan(afterRowID int64, limit int) ([]AuditChainBackfillRow, error) {
	rows, err := s.db.Query(
		`SELECT rowid, id, project_id, type, payload_json, emitted_at,
		        prev_hash, record_hash, partition_id, tessera_leaf_id
		 FROM audit_events_raw
		 WHERE rowid > ? AND prev_hash = '' AND record_hash = ''
		 ORDER BY rowid ASC
		 LIMIT ?`, afterRowID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: backfill scan: %w", err)
	}
	defer rows.Close()
	var out []AuditChainBackfillRow
	for rows.Next() {
		var item AuditChainBackfillRow
		if err := rows.Scan(&item.RowID, &item.ID, &item.ProjectID, &item.Type, &item.PayloadJSON, &item.EmittedAt,
			&item.PrevHash, &item.RecordHash, &item.PartitionID, &item.TesseraLeafID); err != nil {
			return nil, fmt.Errorf("store: scan backfill row: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: backfill scan iter: %w", err)
	}
	return out, nil
}
