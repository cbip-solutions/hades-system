// SPDX-License-Identifier: MIT
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

type ConversationTurnRow struct {
	ID             int64
	ConversationID string
	RequestHash    []byte
	RequestTS      int64
	ResponseHash   []byte
	ResponseTS     int64
	Status         string
	ErrorMessage   string
}

var ErrTurnNotFound = errors.New("conversation_wal: turn not found or already terminal")

func (s *Store) BeginConversationTurn(conversationID string, requestHash []byte, requestTS int64) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO conversation_wal (conversation_id, request_hash, request_ts, status)
		 VALUES (?, ?, ?, 'pending')`,
		conversationID, requestHash, requestTS,
	)
	if err != nil {
		return 0, fmt.Errorf("conversation_wal insert: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) CompleteConversationTurn(turnID int64, responseHash []byte, responseTS int64) error {
	res, err := s.db.Exec(
		`UPDATE conversation_wal
		    SET status='completed', response_hash=?, response_ts=?
		  WHERE turn_id=? AND status='pending'`,
		responseHash, responseTS, turnID,
	)
	if err != nil {
		return fmt.Errorf("conversation_wal complete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTurnNotFound
	}
	return nil
}

func (s *Store) FailConversationTurn(turnID int64, errorMessage string) error {
	res, err := s.db.Exec(
		`UPDATE conversation_wal
		    SET status='failed', error_message=?
		  WHERE turn_id=? AND status='pending'`,
		errorMessage, turnID,
	)
	if err != nil {
		return fmt.Errorf("conversation_wal fail: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTurnNotFound
	}
	return nil
}

func (s *Store) LoadConversation(conversationID string) ([]ConversationTurnRow, error) {
	rows, err := s.db.Query(
		`SELECT turn_id, conversation_id, request_hash, request_ts,
		        response_hash, response_ts, status, error_message
		   FROM conversation_wal
		  WHERE conversation_id=?
		  ORDER BY request_ts ASC, turn_id ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("conversation_wal query: %w", err)
	}
	defer rows.Close()
	return scanConversationRows(rows)
}

func (s *Store) LoadPendingTurns() ([]ConversationTurnRow, error) {
	rows, err := s.db.Query(
		`SELECT turn_id, conversation_id, request_hash, request_ts,
		        response_hash, response_ts, status, error_message
		   FROM conversation_wal
		  WHERE status='pending'
		  ORDER BY request_ts ASC, turn_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("conversation_wal pending query: %w", err)
	}
	defer rows.Close()
	return scanConversationRows(rows)
}

func scanConversationRows(rows *sql.Rows) ([]ConversationTurnRow, error) {
	var out []ConversationTurnRow
	for rows.Next() {
		var r ConversationTurnRow
		var rh sql.RawBytes
		var rts sql.NullInt64
		var em sql.NullString
		if err := rows.Scan(
			&r.ID, &r.ConversationID, &r.RequestHash, &r.RequestTS,
			&rh, &rts, &r.Status, &em,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(rh) > 0 {
			r.ResponseHash = append([]byte(nil), rh...)
		}
		if rts.Valid {
			r.ResponseTS = rts.Int64
		}
		if em.Valid {
			r.ErrorMessage = em.String
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
