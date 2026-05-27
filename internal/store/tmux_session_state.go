// SPDX-License-Identifier: MIT
package store

//
// Schema lives in migrations/062_tmux_session_state.sql ( ships
// the migration alongside the CRUD). Master plan §"Migration numbering
// coordination" originally reserved slot 060 for a JOINT priority_overrides
// + tmux_session_state migration; split the joint payload (its
// own forensic explanation lives in migrations/060_priority_overrides.sql)
// so picks the next free number — 062. (Slot 061 is reserved
// for the knowledge-index database, which lives on a separate
// SQLite file and does not contribute to daemon.db schemaVersion.)
//
// invariant boundary: internal/tmuxlife MUST NOT import internal/store.
// The interface tmuxlife.SessionStore declares the daemon-side contract;
// internal/daemon/handlers/sessions.go is the only package
// permitted to bridge tmuxlife.SessionStore to *store.Store via these
// CRUD primitives.
//
// Time handling:
// - INTEGER columns store UTC unix seconds (per invariant).
// - The Go-side TmuxSessionStateRow uses time.Time (matches Session
// value-type in internal/tmuxlife/session.go).
// - last_attach_at = 0 sentinel means "never attached"; the Go layer
// translates 0 → time.Time{} so callers can use IsZero() consistently.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
)

var ErrDuplicateTmuxSessionName = errors.New("tmux_session_state: name already recorded")

var ErrTmuxSessionStateNotFound = errors.New("tmux_session_state: row not found")

type TmuxSessionStateRow struct {
	Name          string
	Alias         string
	Sha8          string
	Status        int
	CreatedAt     time.Time
	LastAttachAt  time.Time
	ExpectedPanes string
}

func validateSha8(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func validateTmuxStatus(s int) error {
	if s < 0 || s > 3 {
		return fmt.Errorf("tmux_session_state: status %d out of range [0,3]", s)
	}
	return nil
}

func validateExpectedPanes(s string) error {
	if s == "" {
		return errors.New("tmux_session_state: expected_panes is empty (use \"{}\" for empty map)")
	}
	if !json.Valid([]byte(s)) {
		return fmt.Errorf("tmux_session_state: expected_panes is not valid JSON: %q", s)
	}
	return nil
}

func timeToUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().Unix()
}

func unixToTime(u int64) time.Time {
	if u == 0 {
		return time.Time{}
	}
	return time.Unix(u, 0).UTC()
}

func InsertTmuxSessionState(db *sql.DB, row TmuxSessionStateRow) error {
	if row.Name == "" {
		return errors.New("InsertTmuxSessionState: name is empty")
	}
	if row.Alias == "" {
		return errors.New("InsertTmuxSessionState: alias is empty")
	}
	if !validateSha8(row.Sha8) {
		return fmt.Errorf("InsertTmuxSessionState: sha8 %q must be 8 lowercase-hex chars", row.Sha8)
	}
	if err := validateTmuxStatus(row.Status); err != nil {
		return fmt.Errorf("InsertTmuxSessionState: %w", err)
	}
	if err := validateExpectedPanes(row.ExpectedPanes); err != nil {
		return fmt.Errorf("InsertTmuxSessionState: %w", err)
	}
	_, err := db.Exec(
		`INSERT INTO tmux_session_state
		 (name, alias, sha8, status, created_at, last_attach_at, expected_panes)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.Name, row.Alias, row.Sha8, row.Status,
		timeToUnix(row.CreatedAt), timeToUnix(row.LastAttachAt), row.ExpectedPanes,
	)
	if err != nil {
		if isTmuxSessionNamePKViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicateTmuxSessionName, err)
		}
		return fmt.Errorf("insert tmux_session_state: %w", err)
	}
	return nil
}

func isTmuxSessionNamePKViolation(err error) bool {
	if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "tmux_session_state.name") {
		return true
	}
	if strings.Contains(msg, "PRIMARY KEY") {
		return true
	}
	return false
}

func GetTmuxSessionState(db *sql.DB, name string) (*TmuxSessionStateRow, error) {
	if name == "" {
		return nil, errors.New("GetTmuxSessionState: name is empty")
	}
	var (
		row          TmuxSessionStateRow
		createdAt    int64
		lastAttachAt int64
	)
	err := db.QueryRow(
		`SELECT name, alias, sha8, status, created_at, last_attach_at, expected_panes
		 FROM tmux_session_state
		 WHERE name = ?`, name,
	).Scan(
		&row.Name, &row.Alias, &row.Sha8, &row.Status,
		&createdAt, &lastAttachAt, &row.ExpectedPanes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tmux_session_state: %w", err)
	}
	row.CreatedAt = unixToTime(createdAt)
	row.LastAttachAt = unixToTime(lastAttachAt)
	return &row, nil
}

func ListTmuxSessionStates(db *sql.DB) ([]TmuxSessionStateRow, error) {
	rows, err := db.Query(
		`SELECT name, alias, sha8, status, created_at, last_attach_at, expected_panes
		 FROM tmux_session_state
		 ORDER BY created_at DESC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list tmux_session_state: %w", err)
	}
	defer rows.Close()
	out := make([]TmuxSessionStateRow, 0)
	for rows.Next() {
		var (
			r            TmuxSessionStateRow
			createdAt    int64
			lastAttachAt int64
		)
		if err := rows.Scan(
			&r.Name, &r.Alias, &r.Sha8, &r.Status,
			&createdAt, &lastAttachAt, &r.ExpectedPanes,
		); err != nil {
			return nil, fmt.Errorf("scan tmux_session_state: %w", err)
		}
		r.CreatedAt = unixToTime(createdAt)
		r.LastAttachAt = unixToTime(lastAttachAt)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tmux_session_state: %w", err)
	}
	return out, nil
}

func DeleteTmuxSessionState(db *sql.DB, name string) error {
	if name == "" {
		return errors.New("DeleteTmuxSessionState: name is empty")
	}
	res, err := db.Exec(`DELETE FROM tmux_session_state WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete tmux_session_state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete tmux_session_state: rows affected: %w", err)
	}
	if n == 0 {
		return ErrTmuxSessionStateNotFound
	}
	return nil
}

func UpdateTmuxSessionLastAttach(db *sql.DB, name string, t time.Time) error {
	if name == "" {
		return errors.New("UpdateTmuxSessionLastAttach: name is empty")
	}
	res, err := db.Exec(
		`UPDATE tmux_session_state SET last_attach_at = ? WHERE name = ?`,
		timeToUnix(t), name,
	)
	if err != nil {
		return fmt.Errorf("update tmux_session_state.last_attach_at: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tmux_session_state.last_attach_at: rows affected: %w", err)
	}
	if n == 0 {
		return ErrTmuxSessionStateNotFound
	}
	return nil
}

func UpdateTmuxSessionStatus(db *sql.DB, name string, status int) error {
	if name == "" {
		return errors.New("UpdateTmuxSessionStatus: name is empty")
	}
	if err := validateTmuxStatus(status); err != nil {
		return fmt.Errorf("UpdateTmuxSessionStatus: %w", err)
	}
	res, err := db.Exec(
		`UPDATE tmux_session_state SET status = ? WHERE name = ?`,
		status, name,
	)
	if err != nil {
		return fmt.Errorf("update tmux_session_state.status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tmux_session_state.status: rows affected: %w", err)
	}
	if n == 0 {
		return ErrTmuxSessionStateNotFound
	}
	return nil
}

func UpdateTmuxSessionExpectedPanes(db *sql.DB, name, expectedPanes string) error {
	if name == "" {
		return errors.New("UpdateTmuxSessionExpectedPanes: name is empty")
	}
	if err := validateExpectedPanes(expectedPanes); err != nil {
		return fmt.Errorf("UpdateTmuxSessionExpectedPanes: %w", err)
	}
	res, err := db.Exec(
		`UPDATE tmux_session_state SET expected_panes = ? WHERE name = ?`,
		expectedPanes, name,
	)
	if err != nil {
		return fmt.Errorf("update tmux_session_state.expected_panes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tmux_session_state.expected_panes: rows affected: %w", err)
	}
	if n == 0 {
		return ErrTmuxSessionStateNotFound
	}
	return nil
}
