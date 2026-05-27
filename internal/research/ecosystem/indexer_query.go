//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// BinaryTop200 issues a sqlite-vec Hamming top-200 query over the
// ecosystem_chunks_vec_bin BIT[256] virtual table (the binary
// embeddings indexed by C-3's WriteChunks). Returns up to 200
// Candidates ordered by ascending Hamming distance (closest first),
// scoped to the requested ecosystem via the ecosystem_packages JOIN.
//
// dispatcher consumes this as one of two parallel retrieval
// legs per ecosystem (the other is FTS5Top200); the two result sets
// are RRF-fused and re-ranked downstream.
//
// CRITICAL wire-format invariant (inv-hades-203, C-3 inheritance):
// queryEmbBin MUST be wrapped as `vec_bit(?)` in the SQL MATCH
// clause — sqlite-vec BIT[N] virtual tables REJECT raw `?` bindings
// with "Inserted vector for the embedding column is expected to be
// of type bit, but a float32 vector was provided" (sqlite-vec defaults
// raw BLOBs to float32). Mirrors the write-side wrap in
// insertChunkVecBin (indexer.go:512). Verified empirically in C-3 tests.
//
// Score derivation: SimilarityScore = 1 - (hamming_distance / 256).
// For 256-bit binary embeddings the Hamming distance range is [0, 256]
// so SimilarityScore lies in [0, 1] inclusive (1.0 = exact match;
// 0.0 = all-bits-different).
//
// Pre-conditions:
// - len(queryEmbBin) == 32 (256 bits packed little-endian; sqlite-vec
// BIT[256] wire format).
// - idx.opts.DB != nil (NewIndexer enforces but a future refactor
// might surface a nil DB; the runtime guard is defense-in-depth).
// - ctx not yet cancelled (early-exit returns ctx.Err with no query).
//
// Post-conditions on nil error:
// - Returns []Candidate of length 0..200.
// - All Candidates have Ecosystem == eco (per-ecosystem JOIN filter).
// - SimilarityScore monotonically non-increasing across the slice
// (mirrors the ORDER BY v.distance ASC).
//
// versionFilter semantics: empty → no filter (all versions); non-empty →
// matches chunks where stable_in_json contains the literal version
// token (LIKE '%"<version>"%'). Note this is a substring match on the
// JSON-encoded array; it accepts "1.22.0" but NOT "1.22" (a substring
// like the start of "1.22.0"). version-cascade resolves the
// canonical version BEFORE calling this method, so the filter operates
// on a literal version string. The substring approach mirrors the
// dispatcher's hybrid-fusion expectation that version-mismatched chunks
// drop out at the SQL layer rather than during downstream rerank.
//
// Failure modes (all wrapped):
// - len(queryEmbBin) != 32 → returns error mentioning "want 32".
// - idx.opts.DB == nil → returns error mentioning "no DB configured".
// - ctx.Err() set on entry → returns ctx.Err() unwrapped (database/sql
// convention — propagate cancellation transparently).
// - QueryContext error (closed DB, bad SQL syntax) → wrapped with %w.
// - rows.Scan error (schema-column mismatch) → wrapped with %w.
// - rows.Err() after iteration (driver mid-stream failure) → returned
// as the second return value alongside the partial slice.
func (idx *Indexer) BinaryTop200(
	ctx context.Context,
	queryEmbBin []byte,
	versionFilter string,
	eco Ecosystem,
) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(queryEmbBin) != 32 {
		return nil, fmt.Errorf(
			"research/ecosystem: BinaryTop200 queryEmbBin len=%d, want 32",
			len(queryEmbBin),
		)
	}
	if idx.opts.DB == nil {
		return nil, fmt.Errorf("research/ecosystem: BinaryTop200: no DB configured")
	}

	var sb strings.Builder
	args := []interface{}{queryEmbBin, 200, string(eco)}
	sb.WriteString(`
		SELECT c.id, c.content_text, c.symbol_path, c.source_url, v.distance
		FROM ecosystem_chunks_vec_bin v
		JOIN ecosystem_chunks c ON c.id = v.chunk_id
		JOIN ecosystem_packages p ON p.id = c.package_id
		WHERE v.embedding MATCH vec_bit(?) AND v.k = ? AND p.ecosystem = ?
	`)
	if versionFilter != "" {
		sb.WriteString(` AND c.stable_in_json LIKE ?`)
		args = append(args, "%\""+versionFilter+"\"%")
	}
	sb.WriteString(` ORDER BY v.distance ASC`)

	return idx.scanCandidates(ctx, sb.String(), args, eco, hammingDistanceToSim)
}

func (idx *Indexer) FTS5Top200(
	ctx context.Context,
	queryText, versionFilter string,
	eco Ecosystem,
) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if queryText == "" {

		return nil, nil
	}
	if idx.opts.DB == nil {
		return nil, fmt.Errorf("research/ecosystem: FTS5Top200: no DB configured")
	}
	var sb strings.Builder
	args := []interface{}{queryText, string(eco)}
	sb.WriteString(`
		SELECT c.id, c.content_text, c.symbol_path, c.source_url, bm25(ecosystem_chunks_fts) AS score
		FROM ecosystem_chunks_fts
		JOIN ecosystem_chunks c ON c.id = ecosystem_chunks_fts.chunk_id
		JOIN ecosystem_packages p ON p.id = c.package_id
		WHERE ecosystem_chunks_fts MATCH ? AND p.ecosystem = ?
	`)
	if versionFilter != "" {
		sb.WriteString(` AND c.stable_in_json LIKE ?`)
		args = append(args, "%\""+versionFilter+"\"%")
	}

	sb.WriteString(` ORDER BY score ASC LIMIT 200`)

	return idx.scanCandidates(ctx, sb.String(), args, eco, bm25ToSim)
}

func (idx *Indexer) HydrateChunks(
	ctx context.Context,
	chunkIDs []int64,
	eco Ecosystem,
) ([]QueryChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	if idx.opts.DB == nil {
		return nil, fmt.Errorf("research/ecosystem: HydrateChunks: no DB configured")
	}

	var ph strings.Builder
	args := make([]interface{}, 0, len(chunkIDs))
	for i, id := range chunkIDs {
		if i > 0 {
			ph.WriteByte(',')
		}
		ph.WriteByte('?')
		args = append(args, id)
	}
	q := `
		SELECT c.id, c.package_id, p.name, c.symbol_path, c.kind, c.version_introduced,
		       c.content_text, c.contextual_prefix, c.source_url
		FROM ecosystem_chunks c
		JOIN ecosystem_packages p ON p.id = c.package_id
		WHERE c.id IN (` + ph.String() + `)
	`
	rows, err := idx.opts.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: HydrateChunks query: %w", err)
	}
	defer rows.Close()
	out := make([]QueryChunk, 0, len(chunkIDs))
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var qc QueryChunk
		var kind sql.NullString
		var ctxPrefix sql.NullString

		if err := rows.Scan(
			&qc.ChunkID,
			&qc.PackageID,
			&qc.PackageName,
			&qc.SymbolPath,
			&kind,
			&qc.Version,
			&qc.ContentText,
			&ctxPrefix,
			&qc.SourceURL,
		); err != nil {
			return nil, fmt.Errorf("research/ecosystem: HydrateChunks scan: %w", err)
		}
		if kind.Valid {
			qc.Kind = ChunkKind(kind.String)
		}
		if ctxPrefix.Valid {
			qc.ContextualPrefix = ctxPrefix.String
		}
		_ = eco
		out = append(out, qc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("research/ecosystem: HydrateChunks rows.Err: %w", err)
	}
	return out, nil
}

type scoreFunc func(raw float64) float64

func hammingDistanceToSim(dist float64) float64 {
	return 1.0 - (dist / 256.0)
}

func bm25ToSim(score float64) float64 {
	return 1.0 / (1.0 + score)
}

// scanCandidates is the shared rows.Scan loop for BinaryTop200 +
// FTS5Top200. The SQL is responsible for the per-method JOIN +
// WHERE + ORDER BY shape; this helper just iterates + builds the
// []Candidate slice using the supplied score-mapping function.
//
// The columns MUST be (in this order):
//
// c.id int64
// c.content_text string
// c.symbol_path string (nullable per migration 003)
// c.source_url string (NOT NULL per migration 003)
// score float64 (raw distance / bm25)
//
// scoreFn maps raw→[0,1].
func (idx *Indexer) scanCandidates(
	ctx context.Context,
	query string,
	args []interface{},
	eco Ecosystem,
	scoreFn scoreFunc,
) ([]Candidate, error) {
	rows, err := idx.opts.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: scanCandidates query: %w", err)
	}
	defer rows.Close()
	out := make([]Candidate, 0, 200)
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var (
			id    int64
			body  string
			sym   sql.NullString
			url   string
			score float64
		)

		if err := rows.Scan(&id, &body, &sym, &url, &score); err != nil {
			return nil, fmt.Errorf("research/ecosystem: scanCandidates scan: %w", err)
		}
		symStr := ""
		if sym.Valid {
			symStr = sym.String
		}
		out = append(out, Candidate{
			ChunkID:         id,
			Ecosystem:       eco,
			ContentText:     body,
			SymbolPath:      symStr,
			SourceURL:       url,
			SimilarityScore: scoreFn(score),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("research/ecosystem: scanCandidates rows.Err: %w", err)
	}
	return out, nil
}
