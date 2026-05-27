//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — lookup_semantic.go
//
// LookupSemantic implements Step 2 of the research-cache lookup pipeline
// (spec §3.5): encode the 384-float32 query embedding to sqlite-vec wire
// format, query research_query_vec via KNN MATCH, iterate ORDER BY distance ASC,
// and return the first completed dispatch whose findings are non-empty.
//
// Design notes:
//
// 1. embeddingToBytes encodes the query vector as little-endian IEEE-754
// float32 bytes — the canonical sqlite-vec binary wire format. Mirrors
// float32SliceBytes in internal/knowledge/aggregator/vec.go,
// verifying the format empirically in D-4 probe (2026-05-09).
//
// 2. Threshold filtering (cosine_similarity ≥ SemanticThresholdCosine) is
// done in Go after the KNN scan, NOT in SQL. sqlite-vec vec0 MATCH returns
// the angular distance on the unit sphere (sqrt(2*(1-cos_sim)) for unit
// vectors), NOT (1 - cosine_similarity) directly. The correct conversion:
// cosine_similarity = 1 - distance² / 2
// This was verified empirically in probe (2026-05-09)
// against sqlite-vec v0.1.6.
//
// 3. SemanticDistanceThreshold = 1 - SemanticThresholdCosine = 0.08 is the
// threshold on (1 - cosine_similarity), NOT on the angular distance itself.
// The Go filter checks: (1 - distance²/2) >= SemanticThresholdCosine,
// which is equivalent to distance² <= 2*0.08 = 0.16.
//
// 4. KNN query retrieves SemanticTopK=5 candidates; we iterate and return
// the first candidate that has status=DONE and ≥1 finding.
//
// 5. selectDispatchByID uses SELECT by SQLite integer rowid — the rowid
// returned by the KNN MATCH query. This avoids a TEXT primary-key lookup
// when we already have the rowid from the vec0 result set.
//
// invariant enforced via shared ErrProjectIDRequired (defined in lookup_exact.go).
// ErrEmbeddingDimension surfaces dimension mismatch before any DB I/O.
//
// invariant: this package MUST NOT import internal/store. Enforced
// by the post-implementation boundary check in the workflow and the
// compliance test at tests/compliance/.
package cache

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const EmbeddingDim = 384

const SemanticThresholdCosine = 0.92

const SemanticDistanceThreshold = 1.0 - SemanticThresholdCosine

const SemanticTopK = 5

var ErrEmbeddingDimension = errors.New("research_cache: embedding dimension must be 384 (EmbeddingDim)")

func LookupSemantic(ctx context.Context, db *DB, embedding []float32, projectID, sessionID string) (*LookupResult, error) {
	if projectID == "" {
		return nil, ErrProjectIDRequired
	}
	if len(embedding) != EmbeddingDim {
		return nil, ErrEmbeddingDimension
	}
	if db == nil {
		return nil, errors.New("research_cache: LookupSemantic: db is nil")
	}

	embBytes := embeddingToBytes(embedding)

	type knnRow struct {
		rowid    int64
		distance float64
	}

	rows, err := db.SQL.QueryContext(ctx,
		`SELECT rowid, distance
		   FROM research_query_vec
		  WHERE embedding MATCH ?
		    AND k = ?
		  ORDER BY distance ASC`,
		embBytes, SemanticTopK,
	)
	if err != nil {
		return nil, fmt.Errorf("research_cache: LookupSemantic KNN query: %w", err)
	}

	var candidates []knnRow
	for rows.Next() {
		var r knnRow
		if err := rows.Scan(&r.rowid, &r.distance); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("research_cache: LookupSemantic scan: %w", err)
		}

		cosSim := 1.0 - (r.distance*r.distance)/2.0

		if cosSim < SemanticThresholdCosine {
			break
		}
		candidates = append(candidates, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("research_cache: LookupSemantic iter: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("research_cache: LookupSemantic close KNN rows: %w", err)
	}

	for _, c := range candidates {
		dispatch, err := selectDispatchByID(ctx, db.SQL, c.rowid)
		if errors.Is(err, sql.ErrNoRows) {

			continue
		}
		if err != nil {
			return nil, fmt.Errorf("research_cache: LookupSemantic selectDispatchByID rowid=%d: %w", c.rowid, err)
		}

		if dispatch.Status != DispatchStatusDone {
			continue
		}

		findings, err := selectFindingsByDispatchID(ctx, db.SQL, dispatch.ID)
		if err != nil {
			return nil, fmt.Errorf("research_cache: LookupSemantic selectFindings dispatch=%q: %w", dispatch.ID, err)
		}

		if len(findings) == 0 {
			continue
		}

		return &LookupResult{
			Hit:             true,
			HitReason:       CacheHitSemantic,
			FreshnessStatus: FreshnessUnknown,
			Dispatch:        &dispatch,
			Findings:        findings,
		}, nil
	}

	return nil, ErrCacheMiss
}

func selectDispatchByID(ctx context.Context, raw *sql.DB, rowid int64) (Dispatch, error) {
	row := raw.QueryRowContext(ctx,
		`SELECT id, query, status, created_at, updated_at
		   FROM research_dispatches
		  WHERE rowid = ?
		    AND invalidated_at IS NULL`,
		rowid,
	)

	var d Dispatch
	var status string
	if err := row.Scan(
		&d.ID, &d.Query, &status, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {

		return Dispatch{}, err
	}
	d.Status = DispatchStatus(status)
	return d, nil
}

func embeddingToBytes(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:4*(i+1)], math.Float32bits(f))
	}
	return buf
}

func embeddingFromBytes(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[4*i : 4*(i+1)])
		out[i] = math.Float32frombits(bits)
	}
	return out
}
