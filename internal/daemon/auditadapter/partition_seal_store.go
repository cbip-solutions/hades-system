// SPDX-License-Identifier: MIT
// Package auditadapter — extension.
//
// routes audit-chain row queries through the umbrella Adapter
// type (the EventStore implementation). extends auditadapter
// with PartitionSealStore for the cold-archive metadata write-back
// path AND for the recovery package's read paths (verify-chain seal
// walk + restore cold-archive meta lookup). Both types live in the
// same package because they share the same invariant bridge boundary
// (audit substrate ↔ internal/store).
//
// invariant boundary check: this file imports BOTH database/sql AND
// the audit/recovery package. recovery does NOT import auditadapter
// (recovery defines interface surfaces; auditadapter is one concrete
// impl). The import direction is therefore one-way and cycle-free.
package auditadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
)

type PartitionSealStore struct {
	db *sql.DB
}

func NewPartitionSealStore(db *sql.DB) *PartitionSealStore {
	return &PartitionSealStore{db: db}
}

func (s *PartitionSealStore) UpdateColdArchive(
	ctx context.Context,
	projectID, partitionID string,
	coldArchiveURL, contentHash string,
) error {
	if projectID == "" || partitionID == "" {
		return errors.New("auditadapter: empty project_id or partition_id")
	}
	const q = `UPDATE audit_partition_seals
	           SET cold_archive_url = ?, cold_archive_content_hash = ?
	           WHERE partition_id = ?`
	res, err := s.db.ExecContext(ctx, q, coldArchiveURL, contentHash, partitionID)
	if err != nil {
		return fmt.Errorf("auditadapter: update seal: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSealRowMissing
	}
	return nil
}

func (s *PartitionSealStore) GetSealRow(
	ctx context.Context,
	projectID, partitionID string,
) (*PartitionSealRow, error) {
	const q = `SELECT partition_id, sealed_at, final_record_hash,
	                  tessera_seal_leaf_id, daemon_witness_signature,
	                  cold_archive_url, cold_archive_content_hash
	           FROM audit_partition_seals
	           WHERE partition_id = ?`
	row := s.db.QueryRowContext(ctx, q, partitionID)
	var r PartitionSealRow
	var coldURL, coldHash sql.NullString
	if err := row.Scan(
		&r.PartitionID,
		&r.SealedAt,
		&r.FinalRecordHash,
		&r.TesseraSealLeafID,
		&r.DaemonWitnessSignature,
		&coldURL,
		&coldHash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSealRowMissing
		}
		return nil, fmt.Errorf("auditadapter: get seal: %w", err)
	}
	r.ColdArchiveURL = coldURL.String
	r.ColdArchiveContentHash = coldHash.String
	return &r, nil
}

type PartitionSealRow struct {
	PartitionID            string
	SealedAt               int64
	FinalRecordHash        string
	TesseraSealLeafID      string
	DaemonWitnessSignature string
	ColdArchiveURL         string
	ColdArchiveContentHash string
}

var ErrSealRowMissing = errors.New("auditadapter: partition seal row missing")

// ListSeals returns every seal row in audit_partition_seals ordered by
// sealed_at ASC, mapped into recovery.SealMeta values.
//
// Implements recovery.SealRowReader (verify-chain) and the list-half of
// recovery.SealStoreReader (restore).
//
// Schema note: audit_partition_seals
// does NOT carry a project_id column. The projectID parameter is
// therefore a NO-OP filter at the storage layer in the current schema:
// this method returns ALL seal rows across all projects. A future
// migration that adds project_id MUST also revisit the WHERE clause
// here AND the test TestListSealsProjectIDIsCurrentlyNoOp which pins
// the current behaviour.
//
// EventCount + LastID are reconstructed from audit_events_partitions,
// because migration 059 persists the monthly partition statistics in a
// view rather than duplicating them on audit_partition_seals. This keeps
// recovery.VerifyChain able to rebuild the canonical seal payload without
// weakening the seal table schema.
func (s *PartitionSealStore) ListSeals(
	ctx context.Context,
	projectID string,
) ([]recovery.SealMeta, error) {
	const q = `SELECT s.partition_id, s.final_record_hash,
	                  s.tessera_seal_leaf_id, s.daemon_witness_signature,
	                  COALESCE(p.event_count, 0), COALESCE(p.last_id, '')
	           FROM audit_partition_seals s
	           LEFT JOIN audit_events_partitions p ON p.partition_id = s.partition_id
	           ORDER BY s.sealed_at ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("auditadapter: list seals: %w", err)
	}
	defer rows.Close()
	out := make([]recovery.SealMeta, 0)
	for rows.Next() {
		var m recovery.SealMeta
		if err := rows.Scan(
			&m.PartitionID,
			&m.FinalRecordHash,
			&m.TesseraSealLeafID,
			&m.DaemonWitnessSignature,
			&m.EventCount,
			&m.LastID,
		); err != nil {
			return nil, fmt.Errorf("auditadapter: scan seal: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auditadapter: iterate seals: %w", err)
	}
	return out, nil
}

func (s *PartitionSealStore) ColdArchiveMetaFor(
	ctx context.Context,
	projectID, partitionID string,
) (recovery.ColdArchiveMeta, error) {
	if projectID == "" || partitionID == "" {
		return recovery.ColdArchiveMeta{}, errors.New("auditadapter: empty project_id or partition_id")
	}
	const q = `SELECT cold_archive_url, cold_archive_content_hash
	           FROM audit_partition_seals
	           WHERE partition_id = ?`
	row := s.db.QueryRowContext(ctx, q, partitionID)
	var coldURL, coldHash sql.NullString
	if err := row.Scan(&coldURL, &coldHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return recovery.ColdArchiveMeta{}, ErrSealRowMissing
		}
		return recovery.ColdArchiveMeta{}, fmt.Errorf("auditadapter: get cold archive meta: %w", err)
	}
	return recovery.ColdArchiveMeta{
		URL:         coldURL.String,
		ContentHash: coldHash.String,
	}, nil
}
