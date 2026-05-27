//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) UpsertADRLink(ctx context.Context, l ADRLink) error {
	stale := 0
	if l.Stale {
		stale = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO adr_links
			(adr_id, node_id, package_id, link_kind, confidence, stale)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(adr_id, node_id, link_kind) DO UPDATE SET
			package_id = excluded.package_id,
			confidence = excluded.confidence,
			stale = excluded.stale`,
		l.ADRID, l.NodeID, l.PackageID, l.LinkKind, l.Confidence, stale,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertADRLink: %w", err)
	}
	return nil
}

func (s *Store) SetADRLinkStale(ctx context.Context, adrID, nodeID string, kind LinkKind, stale bool) error {
	v := 0
	if stale {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE adr_links SET stale = ? WHERE adr_id = ? AND node_id = ? AND link_kind = ?`,
		v, adrID, nodeID, string(kind),
	)
	if err != nil {
		return fmt.Errorf("caronte/store: SetADRLinkStale: %w", err)
	}
	return nil
}

func (s *Store) ListADRLinksForNode(ctx context.Context, nodeID string) ([]ADRLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT adr_id, node_id, package_id, link_kind, confidence, stale
		FROM adr_links WHERE node_id = ? ORDER BY adr_id ASC, link_kind ASC`, nodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListADRLinksForNode %q: %w", nodeID, err)
	}
	defer rows.Close()
	out := []ADRLink{}
	for rows.Next() {
		var l ADRLink
		var conf sql.NullFloat64
		var stale int
		if err := rows.Scan(&l.ADRID, &l.NodeID, &l.PackageID, &l.LinkKind, &conf, &stale); err != nil {
			return nil, fmt.Errorf("caronte/store: scan adr_link: %w", err)
		}
		if conf.Valid {
			l.Confidence = conf.Float64
		}
		l.Stale = stale != 0
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: adr_link rows: %w", err)
	}
	return out, nil
}

func (s *Store) UpsertLoreTrailer(ctx context.Context, l LoreTrailer) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO lore_trailers
			(commit_sha, file_path, node_id, trailer_kind, body, authored_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(commit_sha, trailer_kind, body) DO UPDATE SET
			file_path = excluded.file_path,
			node_id = excluded.node_id,
			authored_at = excluded.authored_at`,
		l.CommitSHA, l.FilePath, l.NodeID, l.TrailerKind, l.Body, l.AuthoredAt,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: UpsertLoreTrailer: %w", err)
	}
	return nil
}

func (s *Store) ListLoreTrailersForNode(ctx context.Context, nodeID string) ([]LoreTrailer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT commit_sha, file_path, node_id, trailer_kind, body, authored_at
		FROM lore_trailers WHERE node_id = ? ORDER BY authored_at DESC, commit_sha ASC`, nodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListLoreTrailersForNode %q: %w", nodeID, err)
	}
	defer rows.Close()
	out := []LoreTrailer{}
	for rows.Next() {
		var l LoreTrailer
		if err := rows.Scan(&l.CommitSHA, &l.FilePath, &l.NodeID, &l.TrailerKind, &l.Body, &l.AuthoredAt); err != nil {
			return nil, fmt.Errorf("caronte/store: scan lore_trailer: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: lore_trailer rows: %w", err)
	}
	return out, nil
}
