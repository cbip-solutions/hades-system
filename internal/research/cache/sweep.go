// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — sweep.go
//
// Sweeper is the background revalidation sweep goroutine.
// It periodically scans research_findings for expired entries and revalidates
// them via the Revalidator, writing the result to research_validation_log and
// updating the finding's last_validated_at + content_hash on stale content.
//
// # Flow (per sweep iteration)
//
// 1. SELECT findings ORDER BY (last_validated_at IS NULL) DESC, last_validated_at ASC
// LIMIT BatchSize — prioritises unvalidated findings first.
// 2. For each finding: call IsExpired; skip if not expired (spare fresh findings
// from unnecessary HTTP traffic).
// 3. Call Revalidator.Validate on each expired finding.
// 4. INSERT research_validation_log row (regardless of fresh/stale/error) for
// forensic auditability.
// 5. On fresh: UPDATE research_findings.last_validated_at → emit
// EventResearchCacheRevalidatedFresh.
// 6. On stale: UPDATE research_findings.last_validated_at + content_hash →
// emit EventResearchCacheRevalidatedStaleRefetched.
// 7. Errors from Validate do not halt the batch; the validation_log row records
// them. (Failure mode #11 per spec §7.1 — error per source, not per sweep.)
//
// # Cadence and batch size
//
// - Default cadence: 24h.
// - Default batch size: 100 (prevents hot-spot scanning on large caches).
// - First iteration fires immediately (time.NewTimer(0)) so the sweep does
// one pass at daemon startup without waiting a full cadence.
//
// # Context cancellation
//
// Run respects ctx cancellation at the timer-select level. In-progress
// sweepOnce calls are allowed to complete their current finding before the
// goroutine exits. A pre-cancelled ctx causes Run to return context.Canceled
// before the first sweep iteration.
//
// # invariant
//
// This package MUST NOT import internal/store. Sweeper touches only the
// research_cache.db via *DB.SQL; the Revalidator interface is the sole
// HTTP boundary; the Sink interface decouples from the eventlog package.
//
// Doctrine-tunable cadence: default daily (max-scope eager default;
// wires capa-firewall / crypto-attribution overrides via doctrine schema).
package cache

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"
)

// itoa converts a non-negative integer to its base-10 string representation.
// It is a thin wrapper around strconv.Itoa provided as a package-level helper
// so callers do not need to import strconv at every use-site.
//
// Pre n may be any int (including negative; strconv.Itoa handles all int values).
// Post returns the base-10 string, e.g. itoa(42) == "42".
func itoa(n int) string {
	return strconv.Itoa(n)
}

const cacheSchemaVersionV5 = 5

type Sweeper struct {
	DB          *DB
	Revalidator *Revalidator
	Sink        Sink
	Cadence     time.Duration
	BatchSize   int
}

func (s *Sweeper) normalize() {
	if s.Cadence <= 0 {
		s.Cadence = 24 * time.Hour
	}
	if s.BatchSize <= 0 {
		s.BatchSize = 100
	}
}

func (s *Sweeper) Run(ctx context.Context) error {
	if s.DB == nil {
		return fmt.Errorf("research_cache: Sweeper.DB is required")
	}
	if s.Revalidator == nil {
		return fmt.Errorf("research_cache: Sweeper.Revalidator is required")
	}
	if s.Sink == nil {
		return fmt.Errorf("research_cache: Sweeper.Sink is required")
	}

	if err := applyMigrationV5(ctx, s.DB.SQL); err != nil {
		return fmt.Errorf("research_cache: Sweeper.Run: applyMigrationV5: %w", err)
	}

	s.normalize()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			s.sweepOnce(ctx)
			timer.Reset(s.Cadence)
		}
	}
}

func (s *Sweeper) sweepOnce(ctx context.Context) {
	now := time.Now().UTC()

	rows, err := s.DB.SQL.QueryContext(ctx,
		`SELECT id, dispatch_id, url, title, snippet,
		        freshness_status, retrieved_at,
		        content_hash, body_inline_blob, body_path,
		        last_validated_at, source_url_canonical
		   FROM research_findings
		  ORDER BY (last_validated_at IS NULL) DESC, last_validated_at ASC
		  LIMIT ?`,
		s.BatchSize,
	)
	if err != nil {
		log.Printf("research_cache: sweepOnce: query findings: %v", err)
		return
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var f Finding
		var freshness string
		var contentHash sql.NullString
		var bodyPath sql.NullString
		var sourceURLCanonical sql.NullString
		var lastValidatedAt sql.NullInt64

		if err := rows.Scan(
			&f.ID, &f.DispatchID, &f.URL, &f.Title, &f.Snippet,
			&freshness, &f.RetrievedAt,
			&contentHash, &f.BodyInlineBlob, &bodyPath,
			&lastValidatedAt, &sourceURLCanonical,
		); err != nil {
			log.Printf("research_cache: sweepOnce: scan finding: %v", err)
			continue
		}

		f.Freshness = FreshnessStatus(freshness)
		if contentHash.Valid {
			f.ContentHash = contentHash.String
		}
		if bodyPath.Valid {
			f.BodyPath = bodyPath.String
		}
		if sourceURLCanonical.Valid {
			f.SourceURLCanonical = sourceURLCanonical.String
		}
		if lastValidatedAt.Valid {
			t := time.Unix(lastValidatedAt.Int64, 0).UTC()
			f.LastValidatedAt = &t
		}

		f.RetrievalTimestamp = time.Unix(f.RetrievedAt, 0).UTC()

		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		log.Printf("research_cache: sweepOnce: rows.Err: %v", err)
		return
	}

	for _, f := range findings {

		if !IsExpired(f, now) {
			continue
		}

		if err := ctx.Err(); err != nil {
			return
		}

		s.revalidateOne(ctx, f)
	}
}

func (s *Sweeper) revalidateOne(ctx context.Context, finding Finding) {
	now := time.Now().UTC()

	result, validateErr := s.Revalidator.Validate(ctx, finding)

	var httpStatus int
	var etag, lastModified string
	var contentHashMatch sql.NullInt64
	passed := 0
	note := ""

	if validateErr != nil {

		passed = 0
		note = fmt.Sprintf("validate error: %v", validateErr)

	} else {
		httpStatus = 200
		if result.Status == FreshnessFresh {
			passed = 1
			note = "revalidated fresh"
			if result.ETag != "" {
				etag = result.ETag
			}
			if result.LastModified != "" {
				lastModified = result.LastModified
			}
			contentHashMatch = sql.NullInt64{Int64: 1, Valid: true}
		} else {

			passed = 0
			note = "revalidated stale"
			if result.ETag != "" {
				etag = result.ETag
			}
			if result.LastModified != "" {
				lastModified = result.LastModified
			}
			contentHashMatch = sql.NullInt64{Int64: 0, Valid: true}
		}
	}

	logID := "vlog-" + finding.ID + "-" + itoa(int(now.Unix()))

	_, insertErr := s.DB.SQL.ExecContext(ctx,
		`INSERT INTO research_validation_log
		 (id, finding_id, passed, note, validated_at,
		  http_status, etag, last_modified, content_hash_match)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		logID,
		finding.ID,
		passed,
		note,
		now.Unix(),
		nullableInt(httpStatus),
		nullableString(etag),
		nullableString(lastModified),
		nullableNullInt64(contentHashMatch),
	)
	if insertErr != nil {
		log.Printf("research_cache: revalidateOne: insert validation_log finding=%q: %v",
			finding.ID, insertErr)

	}

	if validateErr != nil {
		log.Printf("research_cache: revalidateOne: validate finding=%q URL=%q: %v (skipping update)",
			finding.ID, finding.URL, validateErr)
		return
	}

	nowUnix := now.Unix()

	if result.Status == FreshnessFresh {

		_, updateErr := s.DB.SQL.ExecContext(ctx,
			`UPDATE research_findings SET last_validated_at = ? WHERE id = ?`,
			nowUnix, finding.ID,
		)
		if updateErr != nil {
			log.Printf("research_cache: revalidateOne: update last_validated_at finding=%q: %v",
				finding.ID, updateErr)
		}

		_ = EmitRevalidatedFresh(
			ctx, s.Sink,
			0,
			finding.URL,
			304,
			result.ETag,
			result.LastModified,
			finding.ContentHash,
			finding.ContentHash,
			now,
		)
	} else {

		newHash := result.NewContentHash
		if newHash == "" {
			newHash = finding.ContentHash
		}

		_, updateErr := s.DB.SQL.ExecContext(ctx,
			`UPDATE research_findings
			    SET last_validated_at = ?, content_hash = ?
			  WHERE id = ?`,
			nowUnix, newHash, finding.ID,
		)
		if updateErr != nil {
			log.Printf("research_cache: revalidateOne: update stale finding=%q: %v",
				finding.ID, updateErr)
		}

		_ = EmitRevalidatedStaleRefetched(
			ctx, s.Sink,
			0,
			finding.URL,
			200,
			result.ETag,
			result.LastModified,
			finding.ContentHash,
			newHash,
			now,
		)
	}
}

func applyMigrationV5(ctx context.Context, db *sql.DB) error {

	hasLastValidated, err := tableHasColumn(ctx, db, "research_findings", "last_validated_at")
	if err != nil {
		return fmt.Errorf("applyMigrationV5: table_info research_findings: %w", err)
	}

	if hasLastValidated {

		return nil
	}

	findingsMigrations := []struct {
		stmt string
		desc string
	}{
		{`ALTER TABLE research_findings ADD COLUMN last_validated_at INTEGER`, "add last_validated_at"},
		{`ALTER TABLE research_findings ADD COLUMN source_url_canonical TEXT`, "add source_url_canonical"},
	}
	for _, m := range findingsMigrations {
		if _, err := db.ExecContext(ctx, m.stmt); err != nil {
			return fmt.Errorf("applyMigrationV5: %s: %w", m.desc, err)
		}
	}

	vlogMigrations := []struct {
		stmt string
		desc string
	}{
		{`ALTER TABLE research_validation_log ADD COLUMN http_status INTEGER`, "add http_status"},
		{`ALTER TABLE research_validation_log ADD COLUMN etag TEXT`, "add etag"},
		{`ALTER TABLE research_validation_log ADD COLUMN last_modified TEXT`, "add last_modified"},
		{`ALTER TABLE research_validation_log ADD COLUMN content_hash_match INTEGER`, "add content_hash_match"},
	}
	for _, m := range vlogMigrations {
		if _, err := db.ExecContext(ctx, m.stmt); err != nil {
			return fmt.Errorf("applyMigrationV5: %s: %w", m.desc, err)
		}
	}

	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_findings_last_validated ON research_findings(last_validated_at ASC)`); err != nil {
		return fmt.Errorf("applyMigrationV5: create idx_findings_last_validated: %w", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM _cache_schema_version`); err != nil {
		return fmt.Errorf("applyMigrationV5: clear schema version: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO _cache_schema_version(version) VALUES (?)`, cacheSchemaVersionV5); err != nil {
		return fmt.Errorf("applyMigrationV5: insert schema version V5: %w", err)
	}

	return nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func nullableInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

func nullableNullInt64(n sql.NullInt64) interface{} {
	if !n.Valid {
		return nil
	}
	return n.Int64
}
