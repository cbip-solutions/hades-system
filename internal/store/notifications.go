// SPDX-License-Identifier: MIT
// Package store — notifications.go (Plan 2 Phase L Task L-4).
//
// Typed CRUD on the notifications table (schema v9). Distinct from
// notifications_queue (Plan 11 multi-channel routing). This table is
// bypass-scoped: severity ∈ {INFO, WARN, CRITICAL}, single dispatcher
// (osascript on darwin), simple ack workflow with 1h CRITICAL repeat.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Notification struct {
	ID           int64
	Severity     string
	Title        string
	Body         string
	Source       string
	TS           time.Time
	Acknowledged bool
	AckTS        *time.Time
	LastRepeated *time.Time
}

func validSeverity(sev string) bool {
	switch sev {
	case "INFO", "WARN", "CRITICAL":
		return true
	}
	return false
}

func (s *Store) InsertBypassNotification(ctx context.Context, n Notification) (int64, error) {
	if !validSeverity(n.Severity) {
		return 0, errors.New("severity must be INFO, WARN, or CRITICAL")
	}
	if n.Source == "" {
		return 0, errors.New("source required")
	}
	if n.Title == "" {
		return 0, errors.New("title required")
	}
	if n.TS.IsZero() {
		n.TS = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (severity, title, body, source, ts, acknowledged) VALUES (?, ?, ?, ?, ?, 0)`,
		n.Severity, n.Title, n.Body, n.Source, n.TS.UTC().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListBypassNotifications(ctx context.Context, limit int, onlyUnacked bool) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, severity, title, body, source, ts, acknowledged, ack_ts, last_repeated
	      FROM notifications`
	if onlyUnacked {
		q += ` WHERE acknowledged = 0`
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AckBypassNotification(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET acknowledged = 1, ack_ts = ? WHERE id = ? AND acknowledged = 0`,
		time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("notification not found or already acked")
	}
	return nil
}

func (s *Store) UnackedCriticalsDueForRepeat(ctx context.Context, repeatAfter time.Duration) ([]Notification, error) {
	cutoff := time.Now().UTC().Add(-repeatAfter).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, severity, title, body, source, ts, acknowledged, ack_ts, last_repeated
		FROM notifications
		WHERE acknowledged = 0 AND severity = 'CRITICAL'
		  AND COALESCE(last_repeated, ts) <= ?
		ORDER BY ts ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkRepeated(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET last_repeated = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	return err
}

func scanNotification(rows *sql.Rows) (Notification, error) {
	var n Notification
	var ts, acked int64
	var ackTS, lastRep sql.NullInt64
	if err := rows.Scan(&n.ID, &n.Severity, &n.Title, &n.Body, &n.Source,
		&ts, &acked, &ackTS, &lastRep); err != nil {
		return Notification{}, err
	}
	n.TS = time.Unix(ts, 0).UTC()
	n.Acknowledged = acked == 1
	if ackTS.Valid {
		t := time.Unix(ackTS.Int64, 0).UTC()
		n.AckTS = &t
	}
	if lastRep.Valid {
		t := time.Unix(lastRep.Int64, 0).UTC()
		n.LastRepeated = &t
	}
	return n, nil
}
