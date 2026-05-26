// SPDX-License-Identifier: MIT
// Package auditadapter — Phase C extension.
//
// Phase B routes audit-chain row queries through the umbrella Adapter
// type (the EventStore implementation). Phase C extends auditadapter
// with PartitionSealStore for the cold-archive metadata write-back
// path AND for the recovery package's read paths (verify-chain seal
// walk + restore cold-archive meta lookup). Both types live in the
// same package because they share the same inv-zen-031 bridge boundary
// (audit substrate ↔ internal/store).
//
// inv-zen-031 boundary check: this file imports BOTH database/sql AND
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
// Schema note (Plan 9 Phase B migration 059): audit_partition_seals
// does NOT carry a project_id column. The projectID parameter is
// therefore a NO-OP filter at the storage layer in the current schema:
// this method returns ALL seal rows across all projects. A future
// migration that adds project_id MUST also revisit the WHERE clause
// here AND the test TestListSealsProjectIDIsCurrentlyNoOp which pins
// the current behaviour.
//
// EventCount + LastID gap (Plan 9 cross-phase review C-fix-3 follow-up):
// recovery.SealMeta now carries EventCount + LastID so verify_chain can
// reconstruct the canonical seal payload bytes for witness signature
// verification. Phase B migration 059 does NOT persist these on
// audit_partition_seals; they are filled with zero / "" here. The
// recovery layer treats unverifiable seals as a tamper signal
// (intentional fail-closed under spec §1.Q10), so the immediate runtime
// effect is that VerifyChain reports PathWitnessSignatureInvalid on
// seals built via this path until a follow-up migration adds the two
// columns + a SealPartition update populates them. Tracked separately
// from C-fix-3 because it is a Phase B schema change, not a recovery
// realignment.
func (s *PartitionSealStore) ListSeals(
	ctx context.Context,
	projectID string,
) ([]recovery.SealMeta, error) {
	const q = `SELECT partition_id, final_record_hash,
	                  tessera_seal_leaf_id, daemon_witness_signature
	           FROM audit_partition_seals
	           ORDER BY sealed_at ASC`
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
