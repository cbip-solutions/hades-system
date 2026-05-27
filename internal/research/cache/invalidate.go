// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package cache

import (
	"context"
	"database/sql"
	"fmt"
)

const cacheSchemaVersionV6 = 6

func InvalidateByQuery(ctx context.Context, db *DB, query, reason string, invalidatedAt int64) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("research_cache: InvalidateByQuery: db is nil")
	}
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE research_dispatches
		    SET invalidated_at = ?,
		        invalidated_reason = ?
		  WHERE query_text_hash = ?
		    AND status = ?
		    AND invalidated_at IS NULL`,
		invalidatedAt, reason, ComputeQueryHash(query), string(DispatchStatusDone),
	)
	if err != nil {
		return 0, fmt.Errorf("research_cache: invalidate query: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("research_cache: invalidate rows affected: %w", err)
	}
	return int(n), nil
}

func applyMigrationV6(ctx context.Context, db *sql.DB) error {
	hasInvalidatedAt, err := tableHasColumn(ctx, db, "research_dispatches", "invalidated_at")
	if err != nil {
		return fmt.Errorf("applyMigrationV6: table_info research_dispatches: %w", err)
	}
	if !hasInvalidatedAt {
		migrations := []struct {
			stmt string
			desc string
		}{
			{`ALTER TABLE research_dispatches ADD COLUMN invalidated_at INTEGER`, "add invalidated_at"},
			{`ALTER TABLE research_dispatches ADD COLUMN invalidated_reason TEXT`, "add invalidated_reason"},
		}
		for _, m := range migrations {
			if _, err := db.ExecContext(ctx, m.stmt); err != nil {
				return fmt.Errorf("applyMigrationV6: %s: %w", m.desc, err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM _cache_schema_version`); err != nil {
		return fmt.Errorf("applyMigrationV6: clear schema version: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO _cache_schema_version(version) VALUES (?)`, cacheSchemaVersionV6); err != nil {
		return fmt.Errorf("applyMigrationV6: insert schema version: %w", err)
	}
	return nil
}
