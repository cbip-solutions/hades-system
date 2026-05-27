// SPDX-License-Identifier: MIT
// Package knowledge — hybrid query API Task G-6.
//
// Spec reference: internal design record
// §"Task G-6" lines 2056–2616. Per spec §3.5 the query flow is:
//
// parse query → apply structured filters first (uses indexes)
// → FTS5 MATCH on filtered subset
// → rank
// → limit
// → format
//
// "Filter first" matters: filtering by project_alias + file_type +
// last_modified on indexed columns reduces the candidate set BEFORE FTS5
// runs, keeping query latency O(log N) on the indexed prefix even when
// the corpus grows to 5k+ docs (per spec §6.6 expected ceiling).
//
// invariant (spec §7.2): aggregator NEVER queries web sources directly.
// Query.Remote=true short-circuits to ErrRemoteNotShipped; the CLI layer
// (G-12) translates the sentinel to the deferred-message UX.
// owns the eventual --remote ecosystem RAG implementation.
//
// invariant (spec §7.2): the three extension-hook columns
// (audit_chain_anchor, ecosystem_join_keys, caronte_symbol_refs) ship
// NULL by default in The --code-symbol path uses a JSON-LIKE
// filter on caronte_symbol_refs; in the baseline the column is
// NULL for all rows so the filter matches zero rows. The path is
// preserved so Caronte can populate rows post- without
// retrofit migration.
//
// Boundary: this file imports only
// stdlib + database/sql + the package's own SQLite driver registered in
// index.go. No net/http. No
// internal/store (separate-DB boundary documented in
// docs/operations/knowledge-aggregator-boundary.md, ).
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Format string

const (
	FormatText Format = "text"

	FormatJSON Format = "json"

	FormatMD Format = "md"
)

const DefaultLimit = 10

const MaxLimit = 1000

var ErrRemoteNotShipped = errors.New(
	"knowledge: --remote ecosystem RAG not yet shipped (Plan 14 deliverable)",
)

var ErrAuditChainNotShipped = errors.New(
	"knowledge: --audit-chain hash-chain not yet shipped (Plan 9 deliverable)",
)

type Query struct {
	FreeText      string
	SinceFilter   *time.Duration
	ProjectFilter []string
	TypeFilter    []FileType
	Limit         int
	Format        Format

	Remote     bool
	AuditChain bool
	CodeSymbol string
}

type Result struct {
	Doc     Doc
	Score   float64
	Snippet string
}

func Execute(ctx context.Context, db *sql.DB, q Query) ([]Result, error) {
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	if q.Remote {
		return nil, ErrRemoteNotShipped
	}
	if q.AuditChain {
		return nil, ErrAuditChainNotShipped
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}

	if q.FreeText != "" {
		return executeFreeText(ctx, db, q)
	}
	return executeStructuredOnly(ctx, db, q)
}

func validateQuery(q Query) error {
	if q.Limit < 0 {
		return fmt.Errorf("knowledge: Limit must be ≥ 0; got %d", q.Limit)
	}
	if q.Limit > MaxLimit {
		return fmt.Errorf("knowledge: Limit must be ≤ %d; got %d", MaxLimit, q.Limit)
	}
	if q.SinceFilter != nil && *q.SinceFilter < 0 {
		return fmt.Errorf("knowledge: SinceFilter must be non-negative; got %v", *q.SinceFilter)
	}
	for _, t := range q.TypeFilter {
		switch t {
		case FileTypeMemory, FileTypeResearch, FileTypeADR,
			FileTypeSpec, FileTypePlan, FileTypeHandoff:

		default:
			return fmt.Errorf("knowledge: unknown FileType in TypeFilter: %q", t)
		}
	}
	return nil
}

func buildStructuredWhere(q Query) (string, []any) {
	var clauses []string
	var args []any

	if len(q.ProjectFilter) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(q.ProjectFilter)), ",")
		clauses = append(clauses, fmt.Sprintf("m.project_alias IN (%s)", placeholders))
		for _, p := range q.ProjectFilter {
			args = append(args, p)
		}
	}
	if len(q.TypeFilter) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(q.TypeFilter)), ",")
		clauses = append(clauses, fmt.Sprintf("m.file_type IN (%s)", placeholders))
		for _, t := range q.TypeFilter {
			args = append(args, string(t))
		}
	}
	if q.SinceFilter != nil {
		cutoff := time.Now().Add(-*q.SinceFilter).UnixNano()
		clauses = append(clauses, "m.last_modified >= ?")
		args = append(args, cutoff)
	}
	if q.CodeSymbol != "" {

		clauses = append(clauses, "m.caronte_symbol_refs LIKE ?")
		args = append(args, "%\""+q.CodeSymbol+"\"%")
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func executeFreeText(ctx context.Context, db *sql.DB, q Query) ([]Result, error) {
	where, args := buildStructuredWhere(q)

	var sb strings.Builder
	sb.WriteString(`
		SELECT
			m.rowid,
			m.file_path, m.project_id, m.project_alias, m.file_type, m.title,
			m.frontmatter_json, m.last_modified, m.last_indexed,
			m.audit_chain_anchor, m.ecosystem_join_keys, m.caronte_symbol_refs,
			f.content_text,
			snippet(knowledge_fts, 0, '[', ']', '...', 16) AS snippet,
			bm25(knowledge_fts) AS bm25
		FROM knowledge_meta m
		JOIN knowledge_fts f ON f.rowid = m.rowid
		WHERE knowledge_fts MATCH ? `)
	if where != "" {

		sb.WriteString(" AND ")
		sb.WriteString(strings.TrimPrefix(where, "WHERE "))
	}
	sb.WriteString(" ORDER BY bm25 ASC LIMIT ?")

	finalArgs := make([]any, 0, 2+len(args))
	finalArgs = append(finalArgs, q.FreeText)
	finalArgs = append(finalArgs, args...)
	finalArgs = append(finalArgs, q.Limit)

	rows, err := db.QueryContext(ctx, sb.String(), finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("knowledge: query free-text: %w", err)
	}
	defer rows.Close()

	return scanResults(rows, q, true)
}

func executeStructuredOnly(ctx context.Context, db *sql.DB, q Query) ([]Result, error) {
	where, args := buildStructuredWhere(q)
	var sb strings.Builder
	sb.WriteString(`
		SELECT
			m.rowid,
			m.file_path, m.project_id, m.project_alias, m.file_type, m.title,
			m.frontmatter_json, m.last_modified, m.last_indexed,
			m.audit_chain_anchor, m.ecosystem_join_keys, m.caronte_symbol_refs,
			f.content_text,
			'' AS snippet,
			0.0 AS bm25
		FROM knowledge_meta m
		JOIN knowledge_fts f ON f.rowid = m.rowid `)
	if where != "" {
		sb.WriteString(where)
	}
	sb.WriteString(" ORDER BY m.last_modified DESC LIMIT ?")

	finalArgs := make([]any, 0, 1+len(args))
	finalArgs = append(finalArgs, args...)
	finalArgs = append(finalArgs, q.Limit)

	rows, err := db.QueryContext(ctx, sb.String(), finalArgs...)
	if err != nil {
		return nil, fmt.Errorf("knowledge: query structured: %w", err)
	}
	defer rows.Close()

	return scanResults(rows, q, false)
}

func scanResults(rows *sql.Rows, q Query, hasFTS bool) ([]Result, error) {
	var out []Result
	for rows.Next() {
		var (
			rowid                          int64
			filePath                       string
			projectID, projectAlias, title sql.NullString
			fileType                       string
			fmJSON                         sql.NullString
			lastModNanos, lastIdxNanos     int64
			audit, eco, caronte            sql.NullString
			contentText                    string
			snippet                        string
			bm25                           float64
		)
		if err := rows.Scan(
			&rowid,
			&filePath, &projectID, &projectAlias, &fileType, &title,
			&fmJSON, &lastModNanos, &lastIdxNanos,
			&audit, &eco, &caronte,
			&contentText,
			&snippet, &bm25,
		); err != nil {
			return nil, fmt.Errorf("knowledge: scan row: %w", err)
		}

		d := Doc{
			FilePath:          filePath,
			ProjectID:         projectID.String,
			ProjectAlias:      projectAlias.String,
			FileType:          FileType(fileType),
			Title:             title.String,
			ContentText:       contentText,
			LastModified:      time.Unix(0, lastModNanos),
			LastIndexed:       time.Unix(0, lastIdxNanos),
			AuditChainAnchor:  audit,
			EcosystemJoinKeys: eco,
			CaronteSymbolRefs: caronte,
		}
		if fmJSON.Valid {
			d.FrontmatterJSON = []byte(fmJSON.String)
		}

		if !hasFTS || snippet == "" {
			snippet = firstNChars(d.ContentText, 100)
		}

		score := computeScore(RankParams{
			BaseBM25:          -bm25,
			LastModified:      d.LastModified,
			Now:               time.Now(),
			ProjectMatchBonus: hasProjectMatch(d, q),
		})

		out = append(out, Result{Doc: d, Score: score, Snippet: snippet})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: rows: %w", err)
	}
	if out == nil {

		out = []Result{}
	}
	return out, nil
}

func hasProjectMatch(d Doc, q Query) float64 {
	if len(q.ProjectFilter) == 0 {
		return 0
	}
	for _, p := range q.ProjectFilter {
		if d.ProjectAlias == p {
			return 1.0
		}
	}
	return 0
}

func firstNChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
