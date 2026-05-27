// SPDX-License-Identifier: MIT
// Package aggregator — FTS5 BM25 wrapper for knowledge_pin_fts.
//
// QueryFTS is the primary text-search path in the aggregator. It queries the
// external-content FTS5 virtual table (knowledge_pin_fts) with BM25 ranking
// and returns ordered QueryResult slices for consumption by the D-5 RRF
// fusion layer.
//
// Design choices:
//
// 1. Split into exported Aggregator method (QueryFTS) + package-level helper
// (queryFTS): the package-level helper is directly callable from CGO-tagged
// tests without constructing a full Aggregator, enabling seedFTS to verify
// the SQL without the overhead of Options validation.
//
// 2. BM25 normalisation: FTS5 bm25() returns a negative float64 (lower /
// more negative = better match). We negate it so QueryResult.Score is
// positive and "higher = better", consistent with vec0 cosine distances
// and the RRF fusion contract in D-5.
//
// 3. sanitizeFTSQuery is conservative: it strips FTS5 operator characters
// (`+ - " ( ) * :`) rather than escaping them. release does not expose
// advanced FTS5 syntax (prefix queries, NEAR, column filters) to the
// operator; stripping keeps the implementation simple and correct.
// is a separate package decision.
//
// 4. No CGO build tag here: QueryFTS uses database/sql which works with any
// registered driver. The CGO dependency lives in db.go (mattn/go-sqlite3
// driver registration). fts.go itself is build-tag–agnostic, consistent
// with aggregator.go's posture. The fts_test.go is tagged //go:build cgo
// because it calls Open+Init which require the mattn driver.
//
// invariant: this file makes no web calls. All data comes from aggregator.db.
package aggregator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (a *Aggregator) QueryFTS(ctx context.Context, queryText string, limit int) ([]QueryResult, error) {
	return queryFTS(ctx, a.db, queryText, limit)
}

func queryFTS(ctx context.Context, db *sql.DB, queryText string, limit int) ([]QueryResult, error) {
	if strings.TrimSpace(queryText) == "" {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}
	sanitized := sanitizeFTSQuery(queryText)
	if sanitized == "" {
		return nil, nil
	}

	const q = `
		SELECT
			ki.note_id,
			ki.title,
			snippet(knowledge_pin_fts, 0, '<mark>', '</mark>', '...', 32) AS snippet,
			ki.project_id,
			ki.audit_chain_anchor,
			bm25(knowledge_pin_fts) AS bm25_score
		FROM knowledge_pin_fts
		JOIN knowledge_pin_index ki ON ki.rowid = knowledge_pin_fts.rowid
		WHERE knowledge_pin_fts MATCH ?
		ORDER BY bm25_score
		LIMIT ?
	`

	rows, err := db.QueryContext(ctx, q, sanitized, limit)
	if err != nil {
		return nil, fmt.Errorf("aggregator: QueryFTS MATCH %q: %w", sanitized, err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var bm25 float64
		var anchor sql.NullString
		if err := rows.Scan(
			&r.NoteID, &r.Title, &r.Snippet, &r.ProjectID, &anchor, &bm25,
		); err != nil {
			return nil, fmt.Errorf("aggregator: QueryFTS scan: %w", err)
		}

		r.Score = -bm25
		r.Source = "fts"
		if anchor.Valid {
			r.AuditChainAnchor = anchor.String
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("aggregator: QueryFTS rows: %w", err)
	}
	return results, nil
}

func sanitizeFTSQuery(s string) string {
	stripped := strings.NewReplacer(
		`"`, " ",
		`(`, " ",
		`)`, " ",
		`*`, " ",
		`:`, " ",
		`+`, " ",
		`-`, " ",
	).Replace(s)
	return strings.TrimSpace(stripped)
}
