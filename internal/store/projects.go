// SPDX-License-Identifier: MIT
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	zerrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/ncruces/go-sqlite3"
)

type ProjectRow struct {
	ID               string
	Path             string
	Execution        string
	AuthoritativeGit string
	VPSEndpoint      string
	Doctrine         string
	BudgetMonthlyUSD float64
	PriorityWeight   int
	RegisteredAt     int64
	ConfigJSON       string
}

func (s *Store) UpsertProject(row ProjectRow) error {
	return zerrors.ErrNotImplementedPlan7
}

func (s *Store) GetProject(id string) (*ProjectRow, error) {
	return nil, zerrors.ErrNotImplementedPlan7
}

func (s *Store) ListProjects() ([]ProjectRow, error) {
	return nil, zerrors.ErrNotImplementedPlan7
}

func (s *Store) DeleteProject(id string) error {
	return zerrors.ErrNotImplementedPlan7
}

var ErrDuplicateProjectID = errors.New("projects_alias: id_sha256 already recorded")

var ErrDuplicateAlias = errors.New("projects_alias: alias collides with existing project")

type ProjectAliasRow struct {
	IDSha256      string
	Alias         string
	CanonicalPath string
	FirstSeenAt   int64
	LastSeenAt    int64
	ArchivedAt    int64
}

type PathHistoryRow struct {
	IDSha256    string
	Path        string
	FirstSeenAt int64
	LastSeenAt  int64
}

func InsertProjectAlias(db *sql.DB, row ProjectAliasRow) error {
	if row.IDSha256 == "" {
		return errors.New("InsertProjectAlias: id_sha256 is empty")
	}
	if len(row.IDSha256) != 64 {
		return fmt.Errorf("InsertProjectAlias: id_sha256 must be 64 hex chars, got %d", len(row.IDSha256))
	}
	if row.Alias == "" {
		return errors.New("InsertProjectAlias: alias is empty")
	}
	if row.CanonicalPath == "" {
		return errors.New("InsertProjectAlias: canonical_path is empty")
	}
	var archivedNullable interface{}
	if row.ArchivedAt == 0 {
		archivedNullable = nil
	} else {
		archivedNullable = row.ArchivedAt
	}
	_, err := db.Exec(
		`INSERT INTO projects_alias (
			id_sha256, alias, canonical_path,
			first_seen_at, last_seen_at, archived_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		row.IDSha256, row.Alias, row.CanonicalPath,
		row.FirstSeenAt, row.LastSeenAt, archivedNullable,
	)
	if err != nil {

		if isProjectAliasPKViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicateProjectID, err)
		}
		if isProjectAliasUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicateAlias, err)
		}
		return fmt.Errorf("insert projects_alias: %w", err)
	}
	return nil
}

func isProjectAliasPKViolation(err error) bool {
	if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "projects_alias.id_sha256") {
		return true
	}
	if strings.Contains(msg, "PRIMARY KEY") {
		return true
	}
	return false
}

func isProjectAliasUniqueViolation(err error) bool {
	if errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "projects_alias.alias") {
		return true
	}
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	return false
}

func GetProjectAliasByAlias(db *sql.DB, alias string) (*ProjectAliasRow, error) {
	row := &ProjectAliasRow{}
	var archived sql.NullInt64
	err := db.QueryRow(
		`SELECT id_sha256, alias, canonical_path,
		        first_seen_at, last_seen_at, COALESCE(archived_at, 0)
		 FROM projects_alias
		 WHERE alias = ?`,
		alias,
	).Scan(
		&row.IDSha256, &row.Alias, &row.CanonicalPath,
		&row.FirstSeenAt, &row.LastSeenAt, &archived,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query projects_alias by alias: %w", err)
	}
	if archived.Valid {
		row.ArchivedAt = archived.Int64
	}
	return row, nil
}

func GetProjectAliasByID(db *sql.DB, idSha256 string) (*ProjectAliasRow, error) {
	row := &ProjectAliasRow{}
	var archived sql.NullInt64
	err := db.QueryRow(
		`SELECT id_sha256, alias, canonical_path,
		        first_seen_at, last_seen_at, COALESCE(archived_at, 0)
		 FROM projects_alias
		 WHERE id_sha256 = ?`,
		idSha256,
	).Scan(
		&row.IDSha256, &row.Alias, &row.CanonicalPath,
		&row.FirstSeenAt, &row.LastSeenAt, &archived,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query projects_alias by id: %w", err)
	}
	if archived.Valid {
		row.ArchivedAt = archived.Int64
	}
	return row, nil
}

func ListProjectAliases(db *sql.DB, includeArchived bool) ([]ProjectAliasRow, error) {
	q := `SELECT id_sha256, alias, canonical_path,
	             first_seen_at, last_seen_at, COALESCE(archived_at, 0)
	      FROM projects_alias`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY last_seen_at DESC`
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list projects_alias: %w", err)
	}
	defer rows.Close()
	var out []ProjectAliasRow
	for rows.Next() {
		r := ProjectAliasRow{}
		var archived sql.NullInt64
		if err := rows.Scan(
			&r.IDSha256, &r.Alias, &r.CanonicalPath,
			&r.FirstSeenAt, &r.LastSeenAt, &archived,
		); err != nil {
			return nil, fmt.Errorf("scan projects_alias: %w", err)
		}
		if archived.Valid {
			r.ArchivedAt = archived.Int64
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects_alias: %w", err)
	}
	return out, nil
}

func UpdateProjectAliasLastSeen(db *sql.DB, alias string, lastSeenAt int64) error {
	res, err := db.Exec(
		`UPDATE projects_alias SET last_seen_at = ? WHERE alias = ?`,
		lastSeenAt, alias,
	)
	if err != nil {
		return fmt.Errorf("update projects_alias last_seen: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("update projects_alias: alias %q not found: %w", alias, sql.ErrNoRows)
	}
	return nil
}

func ArchiveProjectAlias(db *sql.DB, alias string, archivedAt int64) error {
	if archivedAt <= 0 {
		return errors.New("ArchiveProjectAlias: archivedAt must be positive ms timestamp")
	}
	res, err := db.Exec(
		`UPDATE projects_alias SET archived_at = ? WHERE alias = ? AND archived_at IS NULL`,
		archivedAt, alias,
	)
	if err != nil {
		return fmt.Errorf("archive projects_alias: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {

		return fmt.Errorf("archive projects_alias: alias %q not found or already archived", alias)
	}
	return nil
}

func DeleteProjectAlias(db *sql.DB, alias string) error {
	res, err := db.Exec(
		`DELETE FROM projects_alias WHERE alias = ?`,
		alias,
	)
	if err != nil {
		return fmt.Errorf("delete projects_alias: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("delete projects_alias: alias %q not found", alias)
	}
	return nil
}

func InsertPathHistory(db *sql.DB, row PathHistoryRow) error {
	if row.IDSha256 == "" {
		return errors.New("InsertPathHistory: id_sha256 is empty")
	}
	if row.Path == "" {
		return errors.New("InsertPathHistory: path is empty")
	}
	_, err := db.Exec(
		`INSERT INTO path_history (id_sha256, path, first_seen_at, last_seen_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id_sha256, path) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		row.IDSha256, row.Path, row.FirstSeenAt, row.LastSeenAt,
	)
	if err != nil {
		return fmt.Errorf("insert path_history: %w", err)
	}
	return nil
}

func QueryPathHistoryByID(db *sql.DB, idSha256 string) ([]PathHistoryRow, error) {
	rows, err := db.Query(
		`SELECT id_sha256, path, first_seen_at, last_seen_at
		 FROM path_history
		 WHERE id_sha256 = ?
		 ORDER BY first_seen_at ASC`,
		idSha256,
	)
	if err != nil {
		return nil, fmt.Errorf("query path_history by id: %w", err)
	}
	defer rows.Close()
	var out []PathHistoryRow
	for rows.Next() {
		r := PathHistoryRow{}
		if err := rows.Scan(&r.IDSha256, &r.Path, &r.FirstSeenAt, &r.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan path_history: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate path_history: %w", err)
	}
	return out, nil
}

func QueryPathHistoryByAlias(db *sql.DB, alias string) ([]PathHistoryRow, error) {
	pa, err := GetProjectAliasByAlias(db, alias)
	if err != nil {
		return nil, err
	}
	if pa == nil {
		return nil, nil
	}
	return QueryPathHistoryByID(db, pa.IDSha256)
}
