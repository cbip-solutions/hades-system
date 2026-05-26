// SPDX-License-Identifier: MIT
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

type AnomalyRow struct {
	ID                     int64
	FieldPath              string
	ParentPath             string
	Count                  int64
	FirstSeen              int64
	LastSeen               int64
	TotalResponsesInWindow int64
	Percentage             float64
	Acknowledged           bool
}

func (s *Store) RecordAnomaly(fieldPath, parentPath string, ts int64) error {
	if fieldPath == "" {
		return errors.New("store: RecordAnomaly: fieldPath empty")
	}
	if ts == 0 {
		return errors.New("store: RecordAnomaly: ts=0 (pass explicit timestamp)")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: RecordAnomaly begin: %w", err)
	}
	defer tx.Rollback()

	// SQLite UPSERT atomically inserts on first observation and
	// increments on subsequent ones. parent_path is set only on first
	// insert; subsequent calls do not overwrite it (a field's parent
	// never changes — re-passing "" or a different value must not
	// clobber the originally-recorded parent_path).
	if _, err := tx.Exec(`
		INSERT INTO bypass_anomalies
			(field_path, parent_path, count, first_seen, last_seen)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(field_path) DO UPDATE SET
			count = count + 1,
			last_seen = excluded.last_seen
	`, fieldPath, parentPath, ts, ts); err != nil {
		return fmt.Errorf("store: RecordAnomaly upsert: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO bypass_anomaly_observations (field_path, ts)
		VALUES (?, ?)
	`, fieldPath, ts); err != nil {
		return fmt.Errorf("store: RecordAnomaly observation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: RecordAnomaly commit: %w", err)
	}
	return nil
}

func (s *Store) QueryAnomalyCount(fieldPath string, sinceTS, untilTS int64) (int64, error) {
	var count sql.NullInt64
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM bypass_anomaly_observations
		WHERE field_path = ?
		  AND ts >= ?
		  AND ts <= ?
	`, fieldPath, sinceTS, untilTS).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: QueryAnomalyCount: %w", err)
	}
	return count.Int64, nil
}

func (s *Store) PurgeAnomalyObservationsOlderThan(cutoffTS int64) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM bypass_anomaly_observations WHERE ts < ?`,
		cutoffTS,
	)
	if err != nil {
		return 0, fmt.Errorf("store: PurgeAnomalyObservationsOlderThan: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) ListAnomalies(includeAcknowledged bool) ([]AnomalyRow, error) {
	q := `
		SELECT id, field_path, COALESCE(parent_path,''), count,
		       first_seen, last_seen,
		       COALESCE(total_responses_in_window, 0),
		       COALESCE(percentage, 0.0),
		       acknowledged
		FROM bypass_anomalies
	`
	if !includeAcknowledged {
		q += ` WHERE acknowledged = 0`
	}
	q += ` ORDER BY last_seen DESC`

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("store: ListAnomalies query: %w", err)
	}
	defer rows.Close()

	var out []AnomalyRow
	for rows.Next() {
		var r AnomalyRow
		var ack int
		if err := rows.Scan(
			&r.ID, &r.FieldPath, &r.ParentPath, &r.Count,
			&r.FirstSeen, &r.LastSeen,
			&r.TotalResponsesInWindow, &r.Percentage, &ack,
		); err != nil {
			return nil, fmt.Errorf("store: ListAnomalies scan: %w", err)
		}
		r.Acknowledged = ack != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: ListAnomalies iter: %w", err)
	}
	return out, nil
}

func (s *Store) AcknowledgeAnomaly(fieldPath string) error {
	res, err := s.db.Exec(
		`UPDATE bypass_anomalies SET acknowledged = 1 WHERE field_path = ?`,
		fieldPath,
	)
	if err != nil {
		return fmt.Errorf("store: AcknowledgeAnomaly: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: AcknowledgeAnomaly: no row for %q", fieldPath)
	}
	return nil
}

func (s *Store) IsAnomalyAcknowledged(fieldPath string) (bool, error) {
	var ack int
	err := s.db.QueryRow(
		`SELECT acknowledged FROM bypass_anomalies WHERE field_path = ?`,
		fieldPath,
	).Scan(&ack)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: IsAnomalyAcknowledged: %w", err)
	}
	return ack != 0, nil
}

func (s *Store) UpdateAnomalyMetrics(fieldPath string, totalInWindow int64, percentage float64) error {
	_, err := s.db.Exec(`
		UPDATE bypass_anomalies
		SET total_responses_in_window = ?, percentage = ?
		WHERE field_path = ?
	`, totalInWindow, percentage, fieldPath)
	if err != nil {
		return fmt.Errorf("store: UpdateAnomalyMetrics: %w", err)
	}
	return nil
}
