// SPDX-License-Identifier: MIT
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

type IdempotencyRow struct {
	Key                string
	RequestHash        []byte
	Status             string
	ResponseStatusCode int
	ResponseHeaders    string
	ResponseBody       []byte
	ErrorMessage       string
	TS                 int64
	ExpiresAt          int64
}

var ErrIdempotencyNotFound = errors.New("idempotency_keys: key not found")

func (s *Store) MarkIdempotencyPending(key string, requestHash []byte, ts, expiresAt int64) error {
	_, err := s.db.Exec(
		`INSERT INTO idempotency_keys
		    (key, request_hash, status, ts, expires_at)
		 VALUES (?, ?, 'pending', ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		    request_hash=excluded.request_hash,
		    status='pending',
		    ts=excluded.ts,
		    expires_at=excluded.expires_at,
		    response_status_code=NULL,
		    response_headers=NULL,
		    response_body=NULL,
		    error_message=NULL`,
		key, requestHash, ts, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("idempotency mark pending: %w", err)
	}
	return nil
}

func (s *Store) MarkIdempotencyCompleted(key string, statusCode int, headersJSON string, body []byte) error {
	res, err := s.db.Exec(
		`UPDATE idempotency_keys
		    SET status='completed',
		        response_status_code=?,
		        response_headers=?,
		        response_body=?
		  WHERE key=? AND status='pending'`,
		statusCode, headersJSON, body, key,
	)
	if err != nil {
		return fmt.Errorf("idempotency mark completed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrIdempotencyNotFound
	}
	return nil
}

func (s *Store) MarkIdempotencyFailed(key string, errorMessage string) error {
	res, err := s.db.Exec(
		`UPDATE idempotency_keys
		    SET status='failed', error_message=?
		  WHERE key=? AND status='pending'`,
		errorMessage, key,
	)
	if err != nil {
		return fmt.Errorf("idempotency mark failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrIdempotencyNotFound
	}
	return nil
}

func (s *Store) GetIdempotency(key string) (*IdempotencyRow, error) {
	row := s.db.QueryRow(
		`SELECT key, request_hash, status,
		        response_status_code, response_headers, response_body,
		        error_message, ts, expires_at
		   FROM idempotency_keys WHERE key=?`,
		key,
	)
	var r IdempotencyRow
	var sc sql.NullInt64
	var hdr sql.NullString
	var em sql.NullString
	var body []byte
	err := row.Scan(&r.Key, &r.RequestHash, &r.Status,
		&sc, &hdr, &body, &em, &r.TS, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency get: %w", err)
	}
	if sc.Valid {
		r.ResponseStatusCode = int(sc.Int64)
	}
	if hdr.Valid {
		r.ResponseHeaders = hdr.String
	}
	if em.Valid {
		r.ErrorMessage = em.String
	}
	if len(body) > 0 {
		r.ResponseBody = body
	}
	return &r, nil
}

func (s *Store) PurgeExpiredIdempotency(now int64) (int, error) {
	res, err := s.db.Exec(`DELETE FROM idempotency_keys WHERE expires_at < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("idempotency purge: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
