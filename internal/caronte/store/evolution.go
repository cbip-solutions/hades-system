// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func canonicalPair(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}

func (s *Store) UpsertCoChange(ctx context.Context, c CoChange) error {
	fa, fb := canonicalPair(c.FileA, c.FileB)
	ra, rb := c.RevsA, c.RevsB
	if fa != c.FileA {
		ra, rb = c.RevsB, c.RevsA
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO co_change_matrix
			(file_a, file_b, shared_revs, revs_a, revs_b, window_days, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_a, file_b, window_days) DO UPDATE SET
			shared_revs = excluded.shared_revs,
			revs_a = excluded.revs_a,
			revs_b = excluded.revs_b,
			updated_at = excluded.updated_at`,
		fa, fb, c.SharedRevs, ra, rb, c.WindowDays, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertCoChange: %w", err)
	}
	return nil
}

func (s *Store) GetCoChange(ctx context.Context, fileA, fileB string, windowDays int) (CoChange, error) {
	fa, fb := canonicalPair(fileA, fileB)
	var c CoChange
	err := s.db.QueryRowContext(ctx, `
		SELECT file_a, file_b, shared_revs, revs_a, revs_b, window_days, updated_at
		FROM co_change_matrix WHERE file_a = ? AND file_b = ? AND window_days = ?`,
		fa, fb, windowDays,
	).Scan(&c.FileA, &c.FileB, &c.SharedRevs, &c.RevsA, &c.RevsB, &c.WindowDays, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CoChange{}, fmt.Errorf("caronte/store: GetCoChange (%s,%s): %w", fa, fb, ErrNotFound)
	}
	if err != nil {
		return CoChange{}, fmt.Errorf("caronte/store: GetCoChange: %w", err)
	}
	return c, nil
}

func (s *Store) UpsertChurn(ctx context.Context, c Churn) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO churn_metrics
			(path, window_days, touch_count, author_count, last_touched, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, window_days) DO UPDATE SET
			touch_count = excluded.touch_count,
			author_count = excluded.author_count,
			last_touched = excluded.last_touched,
			updated_at = excluded.updated_at`,
		c.Path, c.WindowDays, c.TouchCount, c.AuthorCount, c.LastTouched, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertChurn: %w", err)
	}
	return nil
}

func (s *Store) ListCoChangeForFile(ctx context.Context, file string, windowDays int) ([]CoChange, error) {
	const q = `SELECT file_a, file_b, shared_revs, revs_a, revs_b, window_days, updated_at
	           FROM co_change_matrix
	           WHERE (file_a = ? OR file_b = ?) AND window_days = ?
	           ORDER BY file_a, file_b`
	rows, err := s.db.QueryContext(ctx, q, file, file, windowDays)
	if err != nil {
		return nil, fmt.Errorf("caronte store: ListCoChangeForFile query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CoChange
	for rows.Next() {
		var c CoChange
		if err := rows.Scan(&c.FileA, &c.FileB, &c.SharedRevs, &c.RevsA, &c.RevsB, &c.WindowDays, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("caronte store: ListCoChangeForFile scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte store: ListCoChangeForFile rows: %w", err)
	}
	return out, nil
}

func (s *Store) GetChurn(ctx context.Context, path string, windowDays int) (Churn, error) {
	var c Churn
	err := s.db.QueryRowContext(ctx, `
		SELECT path, window_days, touch_count, author_count, last_touched, updated_at
		FROM churn_metrics WHERE path = ? AND window_days = ?`, path, windowDays,
	).Scan(&c.Path, &c.WindowDays, &c.TouchCount, &c.AuthorCount, &c.LastTouched, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Churn{}, fmt.Errorf("caronte/store: GetChurn %q: %w", path, ErrNotFound)
	}
	if err != nil {
		return Churn{}, fmt.Errorf("caronte/store: GetChurn: %w", err)
	}
	return c, nil
}
