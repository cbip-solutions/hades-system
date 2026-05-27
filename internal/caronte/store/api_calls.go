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

func (s *Store) InsertAPICall(ctx context.Context, c APICall) error {
	if s.db == nil {
		return ErrEmptyDB
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_calls
			(call_id, repo, caller_node_id, target_method, target_path_template,
			 target_proto, target_topic, target_graphql_type, target_graphql_field,
			 base_url_ref, confidence, extracted_at, extractor_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(call_id) DO UPDATE SET
			repo                 = excluded.repo,
			caller_node_id       = excluded.caller_node_id,
			target_method        = excluded.target_method,
			target_path_template = excluded.target_path_template,
			target_proto         = excluded.target_proto,
			target_topic         = excluded.target_topic,
			target_graphql_type  = excluded.target_graphql_type,
			target_graphql_field = excluded.target_graphql_field,
			base_url_ref         = excluded.base_url_ref,
			confidence           = excluded.confidence,
			extracted_at         = excluded.extracted_at,
			extractor_id         = excluded.extractor_id`,
		c.CallID, c.Repo, c.CallerNodeID, c.TargetMethod, c.TargetPathTemplate,
		c.TargetProto, c.TargetTopic, c.TargetGraphQLType, c.TargetGraphQLField,
		c.BaseURLRef, c.Confidence, c.ExtractedAt, c.ExtractorID,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: InsertAPICall %q: %w", c.CallID, err)
	}
	return nil
}

func (s *Store) GetAPICall(ctx context.Context, callID string) (APICall, error) {
	if s.db == nil {
		return APICall{}, ErrEmptyDB
	}
	var c APICall
	err := s.db.QueryRowContext(ctx, `
		SELECT call_id, repo, caller_node_id,
		       COALESCE(target_method,''), COALESCE(target_path_template,''),
		       COALESCE(target_proto,''), COALESCE(target_topic,''),
		       COALESCE(target_graphql_type,''), COALESCE(target_graphql_field,''),
		       COALESCE(base_url_ref,''),
		       confidence, extracted_at, extractor_id
		FROM api_calls WHERE call_id = ?`, callID,
	).Scan(
		&c.CallID, &c.Repo, &c.CallerNodeID,
		&c.TargetMethod, &c.TargetPathTemplate,
		&c.TargetProto, &c.TargetTopic,
		&c.TargetGraphQLType, &c.TargetGraphQLField,
		&c.BaseURLRef,
		&c.Confidence, &c.ExtractedAt, &c.ExtractorID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return APICall{}, fmt.Errorf("caronte/store: GetAPICall %q: %w", callID, ErrNotFound)
	}
	if err != nil {
		return APICall{}, fmt.Errorf("caronte/store: GetAPICall %q: %w", callID, err)
	}
	return c, nil
}

func (s *Store) ListAPICallsByCaller(ctx context.Context, callerNodeID string) ([]APICall, error) {
	if s.db == nil {
		return nil, ErrEmptyDB
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT call_id, repo, caller_node_id,
		       COALESCE(target_method,''), COALESCE(target_path_template,''),
		       COALESCE(target_proto,''), COALESCE(target_topic,''),
		       COALESCE(target_graphql_type,''), COALESCE(target_graphql_field,''),
		       COALESCE(base_url_ref,''),
		       confidence, extracted_at, extractor_id
		FROM api_calls WHERE caller_node_id = ?
		ORDER BY call_id ASC`, callerNodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPICallsByCaller %q: %w", callerNodeID, err)
	}
	defer rows.Close()
	out := []APICall{}
	for rows.Next() {
		var c APICall
		if err := rows.Scan(
			&c.CallID, &c.Repo, &c.CallerNodeID,
			&c.TargetMethod, &c.TargetPathTemplate,
			&c.TargetProto, &c.TargetTopic,
			&c.TargetGraphQLType, &c.TargetGraphQLField,
			&c.BaseURLRef,
			&c.Confidence, &c.ExtractedAt, &c.ExtractorID,
		); err != nil {
			return nil, fmt.Errorf("caronte/store: ListAPICallsByCaller scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPICallsByCaller rows: %w", err)
	}
	return out, nil
}

func (s *Store) ListAPICallsByRepo(ctx context.Context, repo string) ([]APICall, error) {
	if s.db == nil {
		return nil, ErrEmptyDB
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT call_id, repo, caller_node_id,
		       COALESCE(target_method,''), COALESCE(target_path_template,''),
		       COALESCE(target_proto,''), COALESCE(target_topic,''),
		       COALESCE(target_graphql_type,''), COALESCE(target_graphql_field,''),
		       COALESCE(base_url_ref,''),
		       confidence, extracted_at, extractor_id
		FROM api_calls WHERE repo = ?
		ORDER BY call_id ASC`, repo,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPICallsByRepo %q: %w", repo, err)
	}
	defer rows.Close()
	out := []APICall{}
	for rows.Next() {
		var c APICall
		if err := rows.Scan(
			&c.CallID, &c.Repo, &c.CallerNodeID,
			&c.TargetMethod, &c.TargetPathTemplate,
			&c.TargetProto, &c.TargetTopic,
			&c.TargetGraphQLType, &c.TargetGraphQLField,
			&c.BaseURLRef,
			&c.Confidence, &c.ExtractedAt, &c.ExtractorID,
		); err != nil {
			return nil, fmt.Errorf("caronte/store: ListAPICallsByRepo scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPICallsByRepo rows: %w", err)
	}
	return out, nil
}

func (s *Store) DeleteAPICallsByFile(ctx context.Context, filePath string) (int, error) {
	if s.db == nil {
		return 0, ErrEmptyDB
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPICallsByFile begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		DELETE FROM api_calls WHERE caller_node_id IN (
			SELECT node_id FROM graph_nodes WHERE file_path = ?
		)`, filePath)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPICallsByFile delete: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPICallsByFile rows-affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPICallsByFile commit: %w", err)
	}
	return int(deleted), nil
}
