//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type Indexer struct {
	opts IndexerOptions

	now func() time.Time
}

type IndexerOptions struct {
	// DB is the open *sql.DB handle for this ecosystem (e.g., go.db).
	// MUST be opened with the sqlite-vec extension auto-registered
	// (via ApplyMigrations which calls sqlite_vec.Auto). The DSN should
	// also enable foreign_keys=ON; without it, FK violations are
	// silently ignored — see migrations_test.go::openTestDB for the
	// canonical DSN pattern.
	DB *sql.DB

	Chain RAGAuditChainEmitter

	Doctrine string
}

func NewIndexer(opts IndexerOptions) (*Indexer, error) {
	if opts.DB == nil {
		return nil, errors.New("research/ecosystem: NewIndexer: DB required")
	}
	if opts.Chain == nil {
		return nil, errors.New("research/ecosystem: NewIndexer: Chain required")
	}
	if opts.Doctrine == "" {
		opts.Doctrine = "default"
	}
	return &Indexer{opts: opts, now: time.Now}, nil
}

func (idx *Indexer) WriteChunks(
	ctx context.Context,
	pkg PackageRef,
	version string,
	chunks []Chunk,
	symbols []SymbolRef,
	changes []ChangeNode,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range chunks {
		c := &chunks[i]
		if got := len(c.EmbeddingBin256d); got != 32 {
			return fmt.Errorf("research/ecosystem: WriteChunks chunk[%d]: bin len=%d, want 32", i, got)
		}
		if got := len(c.EmbeddingFP32_1536d); got != 1536 {
			return fmt.Errorf("research/ecosystem: WriteChunks chunk[%d]: fp32 len=%d, want 1536", i, got)
		}
	}

	startedAt := idx.now()

	tx, err := idx.opts.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {

		return fmt.Errorf("research/ecosystem: WriteChunks BeginTx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {

			_ = tx.Rollback()
		}
	}()

	packageID, err := idx.upsertPackage(ctx, tx, pkg)
	if err != nil {
		return fmt.Errorf("research/ecosystem: upsertPackage: %w", err)
	}

	if err := idx.insertVersion(ctx, tx, packageID, version); err != nil {
		return fmt.Errorf("research/ecosystem: insertVersion: %w", err)
	}

	for i := range chunks {
		c := &chunks[i]
		chunkID, err := idx.insertChunk(ctx, tx, packageID, c)
		if err != nil {
			return fmt.Errorf("research/ecosystem: insertChunk[%d]: %w", i, err)
		}
		if err := idx.insertChunkFP32(ctx, tx, chunkID, c.EmbeddingFP32_1536d); err != nil {
			return fmt.Errorf("research/ecosystem: insertChunkFP32[%d]: %w", i, err)
		}
		if err := idx.insertChunkVecBin(ctx, tx, chunkID, c.EmbeddingBin256d); err != nil {
			return fmt.Errorf("research/ecosystem: insertChunkVecBin[%d]: %w", i, err)
		}
		if err := idx.insertChunkFTS(ctx, tx, chunkID, c); err != nil {
			return fmt.Errorf("research/ecosystem: insertChunkFTS[%d]: %w", i, err)
		}
	}

	for i, s := range symbols {
		if err := idx.upsertSymbol(ctx, tx, packageID, s, version); err != nil {
			return fmt.Errorf("research/ecosystem: upsertSymbol[%d]: %w", i, err)
		}
	}

	for i, ch := range changes {
		if err := idx.upsertChange(ctx, tx, packageID, ch); err != nil {
			return fmt.Errorf("research/ecosystem: upsertChange[%d]: %w", i, err)
		}
	}

	parentHash, _ := idx.opts.Chain.LastHash(ctx)

	completedAt := idx.now()
	payload, err := json.Marshal(map[string]interface{}{
		"package_name":       pkg.Name,
		"ecosystem":          string(pkg.Ecosystem),
		"version":            version,
		"chunks_count":       len(chunks),
		"symbols_count":      len(symbols),
		"change_nodes_count": len(changes),
		"started_at":         startedAt.Unix(),
		"completed_at":       completedAt.Unix(),
	})
	if err != nil {

		return fmt.Errorf("research/ecosystem: marshal audit payload: %w", err)
	}

	seq, err := idx.opts.Chain.Append(ctx, eventlog.EvtRAGIngestPackage, payload, idx.opts.Doctrine)
	if err != nil {
		return fmt.Errorf("research/ecosystem: audit chain Append: %w", err)
	}

	if err := idx.insertAuditRow(ctx, tx, seq, int(eventlog.EvtRAGIngestPackage),
		payload, parentHash, idx.opts.Doctrine, completedAt); err != nil {
		return fmt.Errorf("research/ecosystem: insertAuditRow: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("research/ecosystem: WriteChunks Commit: %w", err)
	}
	committed = true
	return nil
}

func (idx *Indexer) upsertPackage(ctx context.Context, tx *sql.Tx, pkg PackageRef) (int64, error) {
	now := idx.now()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_packages
		    (name, ecosystem, upstream_url, canonical_namespace,
		     last_indexed_at, last_upstream_check, latest_stable_version)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (ecosystem, name) DO UPDATE SET
		    upstream_url          = excluded.upstream_url,
		    canonical_namespace   = excluded.canonical_namespace,
		    last_indexed_at       = excluded.last_indexed_at,
		    last_upstream_check   = excluded.last_upstream_check,
		    latest_stable_version = excluded.latest_stable_version
	`, pkg.Name, string(pkg.Ecosystem), pkg.UpstreamURL, pkg.CanonicalNamespace,
		now, now, pkg.LatestStableVersion)
	if err != nil {

		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM ecosystem_packages WHERE ecosystem = ? AND name = ?`,
		string(pkg.Ecosystem), pkg.Name,
	).Scan(&id); err != nil {

		return 0, err
	}
	return id, nil
}

func (idx *Indexer) insertVersion(ctx context.Context, tx *sql.Tx, packageID int64, version string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_versions (package_id, version, released_at)
		VALUES (?, ?, ?)
		ON CONFLICT (package_id, version) DO NOTHING
	`, packageID, version, idx.now())
	return err
}

func (idx *Indexer) insertChunk(ctx context.Context, tx *sql.Tx, packageID int64, c *Chunk) (int64, error) {

	stableInJSON, err := marshalStableIn(c.StableIn)
	if err != nil {

		return 0, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_chunks
		    (package_id, version_introduced, version_deprecated, stable_in_json,
		     content_text, contextual_prefix, chunk_fingerprint, parent_chunk_id,
		     source_type, symbol_path, kind, source_url, embedding_binary_256d)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		packageID,
		c.VersionIntroduced,
		nullStringToArg(c.VersionDeprecated),
		stableInJSON,
		c.ContentText,
		c.ContextualPrefix,
		c.Fingerprint,
		nullInt64ToArg(c.ParentChunkID),
		string(c.SourceType),
		c.SymbolPath,
		string(c.Kind),
		c.SourceURL,
		c.EmbeddingBin256d,
	)
	if err != nil {

		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {

		return 0, err
	}
	c.ID = id
	return id, nil
}

func (idx *Indexer) insertChunkFP32(ctx context.Context, tx *sql.Tx, chunkID int64, fp32 []float32) error {
	if len(fp32) != 1536 {
		return fmt.Errorf("fp32 chunk %d: len=%d, want 1536", chunkID, len(fp32))
	}
	blob := fp32ToBytes(fp32)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_chunks_fp32 (chunk_id, embedding_blob) VALUES (?, ?)
	`, chunkID, blob)
	return err
}

func (idx *Indexer) insertChunkVecBin(ctx context.Context, tx *sql.Tx, chunkID int64, bin []byte) error {
	if len(bin) != 32 {
		return fmt.Errorf("vec_bin chunk %d: len=%d, want 32", chunkID, len(bin))
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_chunks_vec_bin (chunk_id, embedding) VALUES (?, vec_bit(?))
	`, chunkID, bin)
	return err
}

func (idx *Indexer) insertChunkFTS(ctx context.Context, tx *sql.Tx, chunkID int64, c *Chunk) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_chunks_fts (chunk_id, content_text, contextual_prefix, symbol_path)
		VALUES (?, ?, ?, ?)
	`, chunkID, c.ContentText, c.ContextualPrefix, c.SymbolPath)
	return err
}

func (idx *Indexer) upsertSymbol(ctx context.Context, tx *sql.Tx, packageID int64, s SymbolRef, version string) error {
	introduced := s.Version
	if introduced == "" {
		introduced = version
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_symbols (package_id, symbol_path, kind, introduced_in)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (package_id, symbol_path, introduced_in) DO NOTHING
	`, packageID, s.SymbolPath, "function", introduced)
	return err
}

func (idx *Indexer) upsertChange(ctx context.Context, tx *sql.Tx, packageID int64, ch ChangeNode) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_changes
		    (package_id, version_from, version_to, change_type, symbol_path, description, source_extracted)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (package_id, version_from, version_to, symbol_path) DO UPDATE SET
		    description      = excluded.description,
		    source_extracted = excluded.source_extracted,
		    change_type      = excluded.change_type
	`, packageID, ch.VersionFrom, ch.VersionTo, string(ch.ChangeType),
		ch.SymbolPath, ch.Description, ch.SourceExtracted)
	return err
}

// insertAuditRow writes the per-DB mirror of the canonical chain row.
// seq is whatever the Chain.Append returned (so the per-DB and global
// chain agree on row IDs). self_hash is recomputed locally with the
// SAME spec §4.6 formula as the chain emitter — the two MUST be
// byte-equal; if they ever diverge, tamper-detection breaks.
func (idx *Indexer) insertAuditRow(ctx context.Context, tx *sql.Tx,
	seq int64, evtType int, payload []byte, parentHash, doctrine string,
	emittedAt time.Time,
) error {
	partitionID := emittedAt.UTC().Format("2006-01")
	selfHash := computeSelfHashHex(seq, evtType, payload, parentHash)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ecosystem_audit_chain
		    (seq, event_type, payload_json, parent_hash, self_hash, emitted_at, doctrine, partition_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, seq, evtType, string(payload), parentHash, selfHash, emittedAt, doctrine, partitionID)
	return err
}

func fp32ToBytes(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:4*(i+1)], math.Float32bits(f))
	}
	return buf
}

func marshalStableIn(stableIn []string) (string, error) {
	if len(stableIn) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(stableIn)
	if err != nil {

		return "", err
	}
	return string(b), nil
}

func nullStringToArg(ns sql.NullString) interface{} {
	if !ns.Valid {
		return nil
	}
	return ns.String
}

func nullInt64ToArg(ni sql.NullInt64) interface{} {
	if !ni.Valid {
		return nil
	}
	return ni.Int64
}

type ChunkCandidate struct {
	ChunkID int64

	HammingDistance int

	CosineScore float64

	ContentText       string
	SymbolPath        string
	SourceURL         string
	VersionIntroduced string
	Kind              string
	PackageID         int64
}

// Stage1Binary executes the binary 256-d Hamming KNN against
// ecosystem_chunks_vec_bin, optionally filtered by version. Returns up
// to `topK` candidates ordered by Hamming distance ascending (closest
// first). When `versionFilter` is non-empty, candidates whose
// (version_introduced, version_deprecated) range does not include the
// requested version are dropped in Go after the SQL fetch (SQL string
// comparison is wrong for "1.10" vs "1.2"; the Go post-filter uses a
// SemVer-aware compare via versionInRange + compareSemverLike). To
// keep the post-filter from underflowing the requested topK when many
// candidates are dropped, the SQL fetches 4× topK when a version
// filter is in play (heuristic — tune if hit-rate observability shows
// drops).
//
// `queryBin` MUST be exactly 32 bytes (256-bit). Defense-in-depth:
// length validated at entry rather than relying on sqlite-vec's
// runtime type error.
//
// Wire format note (LOAD-BEARING): the sqlite-vec virtual table
// ecosystem_chunks_vec_bin uses BIT[256] which REQUIRES the
// `vec_bit(?)` SQL wrapper to tag the query BLOB as a BIT vector
// (sqlite-vec defaults raw BLOBs to float32). Mirrors the write-side
// pattern in insertChunkVecBin. Verified empirically in C-3 tests.
func (idx *Indexer) Stage1Binary(
	ctx context.Context,
	eco Ecosystem,
	queryBin []byte,
	topK int,
	versionFilter string,
) ([]ChunkCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(queryBin) != 32 {
		return nil, fmt.Errorf("research/ecosystem: Stage1Binary queryBin len=%d, want 32", len(queryBin))
	}
	if topK <= 0 {
		return nil, fmt.Errorf("research/ecosystem: Stage1Binary topK=%d must be > 0", topK)
	}

	fetchK := topK
	if versionFilter != "" {
		fetchK = topK * 4
	}
	rows, err := idx.opts.DB.QueryContext(ctx, `
		SELECT v.chunk_id, v.distance, c.version_introduced, c.version_deprecated
		FROM ecosystem_chunks_vec_bin v
		JOIN ecosystem_chunks c   ON c.id = v.chunk_id
		JOIN ecosystem_packages p ON p.id = c.package_id
		WHERE v.embedding MATCH vec_bit(?)
		  AND v.k = ?
		  AND p.ecosystem = ?
		ORDER BY v.distance ASC
	`, queryBin, fetchK, string(eco))
	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: Stage1Binary query: %w", err)
	}
	defer rows.Close()
	out := make([]ChunkCandidate, 0, topK)
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var c ChunkCandidate
		var introduced string
		var deprecated sql.NullString
		var distance float64

		if err := rows.Scan(&c.ChunkID, &distance, &introduced, &deprecated); err != nil {
			return nil, fmt.Errorf("research/ecosystem: Stage1Binary scan: %w", err)
		}
		c.HammingDistance = int(distance)
		c.VersionIntroduced = introduced
		if versionFilter != "" {
			depString := ""
			if deprecated.Valid {
				depString = deprecated.String
			}
			if !versionInRange(versionFilter, introduced, depString) {
				continue
			}
		}
		out = append(out, c)
		if len(out) >= topK {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("research/ecosystem: Stage1Binary rows.Err: %w", err)
	}
	return out, nil
}

// Stage2FP32Rerank fetches FP32 1536-d embeddings for `candidates`,
// computes cosine similarity against `queryFP`, and returns the top
// `topK` candidates sorted descending by cosine score. ChunkCandidate
// metadata (ContentText, SymbolPath, SourceURL, VersionIntroduced,
// Kind, PackageID) is populated as a side benefit so the Phase D
// dispatcher and downstream BGE reranker can consume the row without a
// second SELECT against ecosystem_chunks.
//
// `queryFP` MUST be exactly 1536 floats. Defense-in-depth: length
// validated at entry. Each candidate blob MUST decode to exactly 1536
// floats; otherwise a wrapped error surfaces (an indexed row whose
// blob disagrees with the schema-implied 6144 bytes is a corruption
// signal worth surfacing loudly).
//
// Degenerate case: when `queryFP` is the zero vector (vectorNorm == 0),
// the cosine formula's denominator is zero. Rather than producing NaN
// (which would silently propagate through the sort + downstream RRF),
// we short-circuit and return the first `topK` candidates with
// CosineScore == 0 — caller-visible degeneracy, no math-domain error.
//
// Sort insertion sort. Stage 2 typically operates on topK ≈ 50
// candidates from Stage 1's top-200; insertion sort's O(n²) is
// faster than O(n log n) for n ≤ ~100 and avoids the standard library
// sort allocation overhead. Tune if Phase D dispatcher passes much
// larger candidate sets.
func (idx *Indexer) Stage2FP32Rerank(
	ctx context.Context,
	queryFP []float32,
	candidates []ChunkCandidate,
	topK int,
) ([]ChunkCandidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(queryFP) != 1536 {
		return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank queryFP len=%d, want 1536", len(queryFP))
	}
	if topK <= 0 {
		return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank topK=%d must be > 0", topK)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	qNorm := vectorNorm(queryFP)
	if qNorm == 0 {

		out := make([]ChunkCandidate, 0, topK)
		for _, c := range candidates {
			c.CosineScore = 0
			out = append(out, c)
			if len(out) >= topK {
				break
			}
		}
		return out, nil
	}

	ids := make([]interface{}, len(candidates))
	var placeholders strings.Builder
	for i, c := range candidates {
		ids[i] = c.ChunkID
		if i > 0 {
			placeholders.WriteByte(',')
		}
		placeholders.WriteByte('?')
	}
	q := fmt.Sprintf(`
		SELECT c.id, c.content_text, c.symbol_path, c.source_url,
		       c.version_introduced, c.kind, c.package_id, fp.embedding_blob
		FROM ecosystem_chunks c
		JOIN ecosystem_chunks_fp32 fp ON fp.chunk_id = c.id
		WHERE c.id IN (%s)
	`, placeholders.String())
	rows, err := idx.opts.DB.QueryContext(ctx, q, ids...)
	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank query: %w", err)
	}
	defer rows.Close()
	type scored struct {
		cand  ChunkCandidate
		score float64
	}

	hammingByID := make(map[int64]int, len(candidates))
	for _, c := range candidates {
		hammingByID[c.ChunkID] = c.HammingDistance
	}
	scoredList := make([]scored, 0, len(candidates))
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var c ChunkCandidate
		var kindNS sql.NullString
		var symbolNS sql.NullString
		var blob []byte

		if err := rows.Scan(&c.ChunkID, &c.ContentText, &symbolNS, &c.SourceURL,
			&c.VersionIntroduced, &kindNS, &c.PackageID, &blob); err != nil {
			return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank scan: %w", err)
		}
		if symbolNS.Valid {
			c.SymbolPath = symbolNS.String
		}
		if kindNS.Valid {
			c.Kind = kindNS.String
		}
		c.HammingDistance = hammingByID[c.ChunkID]
		fp32, err := bytesToFP32(blob)
		if err != nil {
			return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank decode chunk %d: %w", c.ChunkID, err)
		}
		if len(fp32) != 1536 {
			return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank chunk %d fp32 len=%d, want 1536", c.ChunkID, len(fp32))
		}
		score := cosineSimilarity(queryFP, fp32, qNorm)
		c.CosineScore = score
		scoredList = append(scoredList, scored{cand: c, score: score})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("research/ecosystem: Stage2FP32Rerank rows.Err: %w", err)
	}

	for i := 1; i < len(scoredList); i++ {
		for j := i; j > 0 && scoredList[j].score > scoredList[j-1].score; j-- {
			scoredList[j], scoredList[j-1] = scoredList[j-1], scoredList[j]
		}
	}
	if len(scoredList) > topK {
		scoredList = scoredList[:topK]
	}
	out := make([]ChunkCandidate, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.cand
	}
	return out, nil
}

type CachedEmbedding struct {
	Bin  []byte
	FP32 []float32
}

// LookupExistingEmbedding returns a cached embedding for fingerprint
// (sha256(content_text)) if any chunk row already carries it, else nil.
// Returns the first match (LIMIT 1); ordering is implementation-defined
// because all rows with the same fingerprint MUST carry identical
// embeddings per the cross-shape invariant (C-1).
//
// Phase B ingester usage:
//
//	if cached, err := idx.LookupExistingEmbedding(ctx, chunk.Fingerprint); cached != nil {
//	    chunk.EmbeddingBin256d, chunk.EmbeddingFP32_1536d = cached.Bin, cached.FP32
//	} else {
//	    // call embedder
//	}
func (idx *Indexer) LookupExistingEmbedding(ctx context.Context, fingerprint string) (*CachedEmbedding, error) {
	if fingerprint == "" {
		return nil, errors.New("ecosystem: LookupExistingEmbedding fingerprint required")
	}
	row := idx.opts.DB.QueryRowContext(ctx, `
		SELECT c.embedding_binary_256d, fp.embedding_blob
		FROM ecosystem_chunks c
		JOIN ecosystem_chunks_fp32 fp ON fp.chunk_id = c.id
		WHERE c.chunk_fingerprint = ?
		LIMIT 1
	`, fingerprint)
	var bin, blob []byte
	if err := row.Scan(&bin, &blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("ecosystem: LookupExistingEmbedding scan: %w", err)
	}
	if len(bin) != 32 {
		return nil, fmt.Errorf("ecosystem: LookupExistingEmbedding bin len=%d, want 32", len(bin))
	}
	fp32, err := bytesToFP32(blob)
	if err != nil {
		return nil, fmt.Errorf("ecosystem: LookupExistingEmbedding decode fp32: %w", err)
	}
	if len(fp32) != 1536 {
		return nil, fmt.Errorf("ecosystem: LookupExistingEmbedding fp32 len=%d, want 1536", len(fp32))
	}
	return &CachedEmbedding{Bin: bin, FP32: fp32}, nil
}

func bytesToFP32(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("byte length %d not divisible by 4", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[i*4 : i*4+4])
		out[i] = math.Float32frombits(bits)
	}
	return out, nil
}

func cosineSimilarity(q, c []float32, qNorm float64) float64 {
	if len(q) != len(c) {
		return 0
	}
	var dot, cNormSq float64
	for i := range q {
		qi := float64(q[i])
		ci := float64(c[i])
		dot += qi * ci
		cNormSq += ci * ci
	}
	cNorm := math.Sqrt(cNormSq)
	if cNorm == 0 {
		return 0
	}
	return dot / (qNorm * cNorm)
}

func vectorNorm(v []float32) float64 {
	var sumSq float64
	for _, f := range v {
		sumSq += float64(f) * float64(f)
	}
	return math.Sqrt(sumSq)
}

func versionInRange(query, introduced, deprecated string) bool {
	if introduced == "" {
		return true
	}
	if compareSemverLike(query, introduced) < 0 {
		return false
	}
	if deprecated != "" && compareSemverLike(query, deprecated) >= 0 {
		return false
	}
	return true
}

func compareSemverLike(a, b string) int {
	as := splitDot(a)
	bs := splitDot(b)
	n := len(as)
	if len(bs) < n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, aerr := parseUint(as[i])
		bi, berr := parseUint(bs[i])
		if aerr == nil && berr == nil {
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
			continue
		}
		if as[i] < bs[i] {
			return -1
		}
		if as[i] > bs[i] {
			return 1
		}
	}
	if len(as) < len(bs) {
		return -1
	}
	if len(as) > len(bs) {
		return 1
	}
	return 0
}

func splitDot(s string) []string {
	out := []string{}
	cur := strings.Builder{}
	for _, ch := range s {
		if ch == '.' {
			out = append(out, cur.String())
			cur.Reset()
		} else {
			cur.WriteRune(ch)
		}
	}
	out = append(out, cur.String())
	return out
}

func parseUint(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	var n uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-numeric")
		}
		n = n*10 + uint64(ch-'0')
	}
	return n, nil
}

// computeSelfHashHex computes the canonical spec §4.6 chain-link
// hash: sha256(seq | evt | payload | parent_hash), hex-encoded.
//
// MUST match InMemoryRAGAuditChain.chainHashFormula (mock_chain.go) +
// the Phase D production wrapper formula. If this drifts, the per-DB
// mirror row's self_hash will not equal the canonical chain row's
// self_hash, breaking tamper-detection cross-checks.
//
// Encoding "%d|%d|" prefix + payload bytes + "|<parent>" suffix.
// Stable across implementations (any change requires synchronised
// updates in mock_chain.go::chainHashFormula AND the Phase D wrapper).
func computeSelfHashHex(seq int64, evt int, payload []byte, parentHash string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%d|", seq, evt)
	b.Write(payload)
	b.WriteByte('|')
	b.WriteString(parentHash)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// =============================================================================
// Cross-version query path — Plan 14 Phase E Task E-6
// =============================================================================
//
// IsCrossVersionQuery (package-level helper) + Indexer.QueryCrossVersion
// (SQL pivot over ecosystem_changes) implement the VersionRAG
// cross-version intent path consumed by Phase D dispatcher.Query at the
// intent-routing step (step 1). When IsCrossVersionQuery returns true,
// the dispatcher fans out to QueryCrossVersion (instead of the normal
// vector + BM25 retrieval) and returns the resulting []ChangeNode as
// the answer surface.
//
// inv-zen-193 (Change-node graph consistency): every ecosystem_changes
// row's (version_from, version_to) MUST correspond to ecosystem_versions
// rows via FK chain. QueryCrossVersion is a READ-only operation; the
// invariant is enforced at WRITE time (Phase B-9 + Phase C indexer
// upsertChange) AND verified at audit time (Phase E-7 SweepChangeNodes
// consistency verifier). This method makes no claim about consistency
// — it returns whatever ecosystem_changes contains for the given pivot.

var crossVersionQueryRe = regexp.MustCompile(
	`(?i)(?:changed?\s+between|changes?\s+from|what.{0,20}new\s+in)` +
		`\s+(?:\w+\s+)?(\d+(?:\.\d+)*)` +
		`\s+(?:to|and|→|->)` +
		`\s+(?:\w+\s+)?(\d+(?:\.\d+)*)` +
		`|(\d+(?:\.\d+)*)` +
		`\s+(?:to|→|->)` +
		`\s+(?:\w+\s+)?(\d+(?:\.\d+)*)` +
		`\s+(?:break|chang|new|releas|migrat)`)

func IsCrossVersionQuery(query string) (matched bool, versionFrom, versionTo string) {
	m := crossVersionQueryRe.FindStringSubmatch(query)
	if m == nil {
		return false, "", ""
	}

	if m[1] != "" && m[2] != "" {
		return true, m[1], m[2]
	}

	return true, m[3], m[4]
}

// QueryCrossVersion executes a SQL pivot over ecosystem_changes for the
// given (packageID, versionFrom, versionTo) tuple. Returns []ChangeNode
// sorted by (change_type, symbol_path) — alphabetic on the change_type
// enum value, then on the symbol_path string.
//
// SQL
//
//	SELECT id, package_id, version_from, version_to, change_type,
//	       COALESCE(symbol_path, ''), COALESCE(description, ''),
//	       source_extracted
//	FROM ecosystem_changes
//	WHERE package_id = ? AND version_from = ? AND version_to = ?
//	ORDER BY change_type, symbol_path
//
// The COALESCE wrappers on symbol_path + description handle the schema's
// nullable columns: ecosystem_changes.symbol_path TEXT (no NOT NULL) and
// ecosystem_changes.description TEXT (no NOT NULL). A NULL on either
// scans to an empty string — the dispatcher's UX surface treats empty
// as "no symbol attribution" / "no human description" rather than
// crashing on a database/sql NULL→string conversion error.
//
// Architecture pattern: uses idx.opts.DB (Phase C convention, mirrors
// BinaryTop200 / FTS5Top200 / HydrateChunks in indexer_query.go), NOT
// db-as-parameter from the plan-file. Aligns with master §3.13
// IndexerQueryAdapter contract pattern. Phase D dispatcher will add a
// QueryCrossVersion method to that adapter interface separately (it is
// not part of the C-9 frozen surface; the cross-version path is an
// E-phase extension consumed via a dispatcher-side cross-version
// router).
//
// inv-zen-193 (Change-node graph consistency): every row's
// (version_from, version_to) MUST correspond to ecosystem_versions rows
// via FK chain. QueryCrossVersion is a READ-only operation; the
// invariant is enforced at WRITE time (Phase B-9 + Phase C indexer
// upsertChange) and verified by the Phase E-7 SweepChangeNodes
// consistency verifier. This method makes no consistency claim — it
// returns whatever ecosystem_changes contains for the pivot tuple.
//
// Pre-conditions:
//   - ctx not yet cancelled (early-exit returns ctx.Err with no query).
//   - idx.opts.DB != nil. NewIndexer enforces but a future refactor
//     might surface a nil DB; the runtime guard is defense-in-depth
//     (mirrors BinaryTop200 / FTS5Top200 / HydrateChunks pattern).
//
// Post-conditions on nil error:
//   - Returns []ChangeNode of length 0..N (N = number of matching rows).
//   - All ChangeNodes have PackageID == packageID, VersionFrom ==
//     versionFrom, VersionTo == versionTo (per the WHERE clause).
//   - Sort order: (change_type, symbol_path) ASC, ASC.
//
// Failure modes (all wrapped):
//   - ctx.Err() set on entry → returns ctx.Err() unwrapped.
//   - idx.opts.DB == nil → returns error mentioning "no DB configured".
//   - QueryContext error → wrapped with %w.
//   - rows.Scan error → wrapped with %w.
//   - rows.Err() after iteration → wrapped with %w.
//
// IsCrossVersionQuery returns true.
func (idx *Indexer) QueryCrossVersion(ctx context.Context, packageID int64, versionFrom, versionTo string) ([]ChangeNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if idx.opts.DB == nil {
		return nil, fmt.Errorf("research/ecosystem: QueryCrossVersion: no DB configured")
	}
	const query = `
		SELECT id, package_id, version_from, version_to, change_type,
		       COALESCE(symbol_path, ''), COALESCE(description, ''), source_extracted
		FROM ecosystem_changes
		WHERE package_id = ? AND version_from = ? AND version_to = ?
		ORDER BY change_type, symbol_path
	`
	rows, err := idx.opts.DB.QueryContext(ctx, query, packageID, versionFrom, versionTo)
	if err != nil {

		return nil, fmt.Errorf("research/ecosystem: QueryCrossVersion query: %w", err)
	}
	defer rows.Close()
	var nodes []ChangeNode
	for rows.Next() {

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var n ChangeNode
		var changeTypeStr string

		if err := rows.Scan(
			&n.ID, &n.PackageID, &n.VersionFrom, &n.VersionTo,
			&changeTypeStr, &n.SymbolPath, &n.Description, &n.SourceExtracted,
		); err != nil {
			return nil, fmt.Errorf("research/ecosystem: QueryCrossVersion scan: %w", err)
		}
		n.ChangeType = ChangeType(changeTypeStr)
		nodes = append(nodes, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("research/ecosystem: QueryCrossVersion rows.Err: %w", err)
	}
	return nodes, nil
}
