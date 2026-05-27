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

func (s *Store) InsertAPIEndpoint(ctx context.Context, e APIEndpoint) error {
	if s.db == nil {
		return ErrEmptyDB
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_endpoints
			(endpoint_id, repo, kind, method, path_template,
			 proto_service, proto_rpc, topic, graphql_type, graphql_field,
			 handler_node_id, contract_artifact, extracted_at, extractor_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint_id) DO UPDATE SET
			repo              = excluded.repo,
			kind              = excluded.kind,
			method            = excluded.method,
			path_template     = excluded.path_template,
			proto_service     = excluded.proto_service,
			proto_rpc         = excluded.proto_rpc,
			topic             = excluded.topic,
			graphql_type      = excluded.graphql_type,
			graphql_field     = excluded.graphql_field,
			handler_node_id   = excluded.handler_node_id,
			contract_artifact = excluded.contract_artifact,
			extracted_at      = excluded.extracted_at,
			extractor_id      = excluded.extractor_id`,
		e.EndpointID, e.Repo, e.Kind, e.Method, e.PathTemplate,
		e.ProtoService, e.ProtoRPC, e.Topic, e.GraphQLType, e.GraphQLField,
		e.HandlerNodeID, e.ContractArtifact, e.ExtractedAt, e.ExtractorID,
	)
	if err != nil {
		return fmt.Errorf("caronte/store: InsertAPIEndpoint %q: %w", e.EndpointID, err)
	}
	return nil
}

func (s *Store) GetAPIEndpoint(ctx context.Context, endpointID string) (APIEndpoint, error) {
	if s.db == nil {
		return APIEndpoint{}, ErrEmptyDB
	}
	var e APIEndpoint
	err := s.db.QueryRowContext(ctx, `
		SELECT endpoint_id, repo, kind, COALESCE(method,''), COALESCE(path_template,''),
		       COALESCE(proto_service,''), COALESCE(proto_rpc,''), COALESCE(topic,''),
		       COALESCE(graphql_type,''), COALESCE(graphql_field,''),
		       handler_node_id, COALESCE(contract_artifact,''),
		       extracted_at, extractor_id
		FROM api_endpoints WHERE endpoint_id = ?`, endpointID,
	).Scan(
		&e.EndpointID, &e.Repo, &e.Kind, &e.Method, &e.PathTemplate,
		&e.ProtoService, &e.ProtoRPC, &e.Topic, &e.GraphQLType, &e.GraphQLField,
		&e.HandlerNodeID, &e.ContractArtifact,
		&e.ExtractedAt, &e.ExtractorID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return APIEndpoint{}, fmt.Errorf("caronte/store: GetAPIEndpoint %q: %w", endpointID, ErrNotFound)
	}
	if err != nil {
		return APIEndpoint{}, fmt.Errorf("caronte/store: GetAPIEndpoint %q: %w", endpointID, err)
	}
	return e, nil
}

func (s *Store) ListAPIEndpointsByFile(ctx context.Context, filePath string) ([]APIEndpoint, error) {
	if s.db == nil {
		return nil, ErrEmptyDB
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.endpoint_id, e.repo, e.kind, COALESCE(e.method,''), COALESCE(e.path_template,''),
		       COALESCE(e.proto_service,''), COALESCE(e.proto_rpc,''), COALESCE(e.topic,''),
		       COALESCE(e.graphql_type,''), COALESCE(e.graphql_field,''),
		       e.handler_node_id, COALESCE(e.contract_artifact,''),
		       e.extracted_at, e.extractor_id
		FROM api_endpoints e
		JOIN graph_nodes n ON n.node_id = e.handler_node_id
		WHERE n.file_path = ?
		ORDER BY e.endpoint_id ASC`, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByFile %q: %w", filePath, err)
	}
	defer rows.Close()
	out := []APIEndpoint{}
	for rows.Next() {
		var e APIEndpoint
		if err := rows.Scan(
			&e.EndpointID, &e.Repo, &e.Kind, &e.Method, &e.PathTemplate,
			&e.ProtoService, &e.ProtoRPC, &e.Topic, &e.GraphQLType, &e.GraphQLField,
			&e.HandlerNodeID, &e.ContractArtifact,
			&e.ExtractedAt, &e.ExtractorID,
		); err != nil {
			return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByFile scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByFile rows: %w", err)
	}
	return out, nil
}

func (s *Store) ListAPIEndpointsByRepo(ctx context.Context, repo string) ([]APIEndpoint, error) {
	if s.db == nil {
		return nil, ErrEmptyDB
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT endpoint_id, repo, kind, COALESCE(method,''), COALESCE(path_template,''),
		       COALESCE(proto_service,''), COALESCE(proto_rpc,''), COALESCE(topic,''),
		       COALESCE(graphql_type,''), COALESCE(graphql_field,''),
		       handler_node_id, COALESCE(contract_artifact,''),
		       extracted_at, extractor_id
		FROM api_endpoints WHERE repo = ?
		ORDER BY endpoint_id ASC`, repo,
	)
	if err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByRepo %q: %w", repo, err)
	}
	defer rows.Close()
	out := []APIEndpoint{}
	for rows.Next() {
		var e APIEndpoint
		if err := rows.Scan(
			&e.EndpointID, &e.Repo, &e.Kind, &e.Method, &e.PathTemplate,
			&e.ProtoService, &e.ProtoRPC, &e.Topic, &e.GraphQLType, &e.GraphQLField,
			&e.HandlerNodeID, &e.ContractArtifact,
			&e.ExtractedAt, &e.ExtractorID,
		); err != nil {
			return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByRepo scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/store: ListAPIEndpointsByRepo rows: %w", err)
	}
	return out, nil
}

func (s *Store) DeleteAPIEndpointsByFile(ctx context.Context, filePath string) (int, error) {
	if s.db == nil {
		return 0, ErrEmptyDB
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPIEndpointsByFile begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		DELETE FROM api_endpoints WHERE handler_node_id IN (
			SELECT node_id FROM graph_nodes WHERE file_path = ?
		)`, filePath)
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPIEndpointsByFile delete: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPIEndpointsByFile rows-affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("caronte/store: DeleteAPIEndpointsByFile commit: %w", err)
	}
	return int(deleted), nil
}
