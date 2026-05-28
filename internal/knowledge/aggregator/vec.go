// SPDX-License-Identifier: MIT
// Package aggregator — sqlite-vec KNN wrapper for knowledge_pin_vec.
//
// QueryVec is the primary vector-similarity search path in the aggregator. It
// queries the vec0 virtual table (knowledge_pin_vec) via sqlite-vec's KNN
// MATCH syntax and returns cosine-filtered QueryResult slices for consumption
// by the D-5 RRF fusion layer.
//
// Design choices:
//
// 1. float32SliceBytes encodes the query vector as little-endian IEEE-754 bytes
// so it can be bound to the MATCH parameter of the sqlite-vec vec0 virtual
// table. sqlite-vec accepts raw bytes in this format from Go via the mattn
// driver (confirmed via D-2 probe and HADES design plan-file §D-4
// schema check).
//
// 2. Threshold filtering (cosine_similarity ≥ threshold) is done in Go after
// the KNN scan rather than in SQL. sqlite-vec vec0 MATCH returns the
// angular distance on the unit sphere, NOT (1 - cosine_similarity) directly.
// The correct conversion is: cosine_similarity = 1 - distance²/2
// (derived from the identity: ||a-b||² = 2 - 2·cos(θ) for unit vectors).
// This was verified empirically against sqlite-vec v0.1.6 in the D-4
// probe (2026-05-09): the plan-file assumed `sim = 1 - distance`
// but the actual formula is `sim = 1 - distance²/2`.
//
// 3. Score = 1 - distance²/2 (cosine_similarity) makes the QueryResult.Score
// contract consistent with QueryFTS (higher = better) and the D-5 RRF
// fusion input expectations.
//
// 4. isExtensionMissing + contains are kept in this file (no "strings" import)
// to satisfy the invariant grep test which checks for net/http absence.
// The contains helper avoids importing strings just for one callsite.
//
// invariant: this file makes NO web calls. All data comes from aggregator.db
// (local sqlite-vec extension). No import of net/http, net/url, or any
// network package.
package aggregator

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var ErrVecUnavailable = errors.New("aggregator: sqlite-vec unavailable (degraded mode)")

func float32SliceBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:4*(i+1)], math.Float32bits(f))
	}
	return buf
}

func (a *Aggregator) QueryVec(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]QueryResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, nil
	}
	if len(queryEmbedding) != vecDimensions {
		return nil, fmt.Errorf(
			"aggregator: queryEmbedding dim %d != vecDimensions %d",
			len(queryEmbedding), vecDimensions,
		)
	}
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if a.Degraded() {
		return nil, ErrVecUnavailable
	}
	return queryVec(ctx, a.db, queryEmbedding, limit, threshold)
}

func queryVec(ctx context.Context, db *sql.DB, queryEmbedding []float32, limit int, threshold float64) ([]QueryResult, error) {
	embBytes := float32SliceBytes(queryEmbedding)

	const q = `
		SELECT
			ki.note_id,
			ki.title,
			substr(ki.content, 1, 200) AS snippet,
			ki.project_id,
			ki.audit_chain_anchor,
			distance
		FROM knowledge_pin_vec AS kv
		JOIN knowledge_pin_index ki ON ki.rowid = kv.rowid
		WHERE kv.embedding MATCH ?
		  AND k = ?
		ORDER BY distance ASC
	`

	rows, err := db.QueryContext(ctx, q, embBytes, limit)
	if err != nil {
		if isExtensionMissing(err) {
			return nil, ErrVecUnavailable
		}
		return nil, fmt.Errorf("aggregator: QueryVec KNN: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var distance float64
		var anchor sql.NullString
		if err := rows.Scan(
			&r.NoteID, &r.Title, &r.Snippet, &r.ProjectID, &anchor, &distance,
		); err != nil {
			return nil, fmt.Errorf("aggregator: QueryVec scan: %w", err)
		}

		similarity := 1.0 - (distance*distance)/2.0
		if similarity < threshold {
			continue
		}
		r.Score = similarity
		r.Source = "vec"
		if anchor.Valid {
			r.AuditChainAnchor = anchor.String
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("aggregator: QueryVec iter: %w", err)
	}
	return results, nil
}

func isExtensionMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	markers := []string{
		"no such module: vec0",
		"no such function: vec_distance_cosine",
		"no such table: knowledge_pin_vec",
	}
	for _, m := range markers {
		if contains(msg, m) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	sl := len(substr)
	if sl == 0 {
		return true
	}
	for i := 0; i+sl <= len(s); i++ {
		if s[i:i+sl] == substr {
			return true
		}
	}
	return false
}
