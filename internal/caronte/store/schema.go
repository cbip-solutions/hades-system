// SPDX-License-Identifier: MIT
package store

import "fmt"

const vecDimensions = 1536

func schemaStatements() []string {
	return []string{

		`CREATE TABLE IF NOT EXISTS graph_nodes (
  node_id      TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  kind         TEXT NOT NULL,
  language     TEXT NOT NULL,
  file_path    TEXT NOT NULL,
  start_line   INTEGER, end_line INTEGER,
  signature    TEXT,
  doc          TEXT,
  coreness     INTEGER,
  scc_id       INTEGER,
  package_id   TEXT,
  content_hash TEXT NOT NULL
)`,

		`CREATE TABLE IF NOT EXISTS graph_edges (
  source_id   TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  kind        TEXT NOT NULL,
  confidence  TEXT NOT NULL,
  reachable   INTEGER,
  site_file   TEXT, site_line INTEGER,
  PRIMARY KEY (source_id, target_id, kind, site_line)
)`,

		`CREATE INDEX IF NOT EXISTS idx_edges_target ON graph_edges(target_id, kind)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_source ON graph_edges(source_id, kind)`,

		`CREATE INDEX IF NOT EXISTS idx_nodes_iface  ON graph_nodes(kind)`,

		`CREATE INDEX IF NOT EXISTS idx_graph_nodes_file_line ON graph_nodes(file_path, start_line)`,

		`CREATE TABLE IF NOT EXISTS co_change_matrix (
  file_a TEXT NOT NULL, file_b TEXT NOT NULL,
  shared_revs INTEGER NOT NULL, revs_a INTEGER NOT NULL, revs_b INTEGER NOT NULL,
  window_days INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  PRIMARY KEY (file_a, file_b, window_days)
)`,
		`CREATE TABLE IF NOT EXISTS churn_metrics (
  path TEXT NOT NULL, window_days INTEGER NOT NULL,
  touch_count INTEGER NOT NULL, author_count INTEGER NOT NULL,
  last_touched INTEGER, updated_at INTEGER NOT NULL,
  PRIMARY KEY (path, window_days)
)`,

		`CREATE TABLE IF NOT EXISTS adr_links (
  adr_id TEXT NOT NULL,
  node_id TEXT, package_id TEXT,
  link_kind TEXT NOT NULL,
  confidence REAL,
  stale INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (adr_id, node_id, link_kind)
)`,
		`CREATE TABLE IF NOT EXISTS lore_trailers (
  commit_sha TEXT NOT NULL, file_path TEXT, node_id TEXT,
  trailer_kind TEXT NOT NULL,
  body TEXT NOT NULL, authored_at INTEGER NOT NULL,
  PRIMARY KEY (commit_sha, trailer_kind, body)
)`,

		fmt.Sprintf(
			`CREATE VIRTUAL TABLE IF NOT EXISTS code_node_vec  USING vec0(embedding float[%d])`,
			vecDimensions,
		),
		`CREATE VIRTUAL TABLE IF NOT EXISTS graph_nodes_fts USING fts5(name, signature, doc, content='graph_nodes', content_rowid='rowid')`,

		`CREATE TABLE IF NOT EXISTS api_endpoints (
  endpoint_id       TEXT PRIMARY KEY,
  repo              TEXT NOT NULL,
  kind              TEXT NOT NULL CHECK (kind IN ('http','grpc','graphql','mq','ws')),
  method            TEXT,
  path_template     TEXT,
  proto_service     TEXT, proto_rpc TEXT,
  topic             TEXT,
  graphql_type      TEXT, graphql_field TEXT,
  handler_node_id   TEXT NOT NULL,
  contract_artifact TEXT,
  extracted_at      INTEGER NOT NULL,
  extractor_id      TEXT NOT NULL
)`,

		`CREATE TABLE IF NOT EXISTS api_calls (
  call_id              TEXT PRIMARY KEY,
  repo                 TEXT NOT NULL,
  caller_node_id       TEXT NOT NULL,
  target_method        TEXT, target_path_template TEXT,
  target_proto         TEXT, target_topic TEXT,
  target_graphql_type  TEXT, target_graphql_field TEXT,
  base_url_ref         TEXT,
  confidence           TEXT NOT NULL CHECK (confidence IN ('exact_proto_import','spec_artifact','static_path','fuzzy_path')),
  extracted_at         INTEGER NOT NULL,
  extractor_id         TEXT NOT NULL
)`,

		`CREATE INDEX IF NOT EXISTS idx_endpoints_http  ON api_endpoints(kind, method, path_template) WHERE kind='http'`,
		`CREATE INDEX IF NOT EXISTS idx_endpoints_proto ON api_endpoints(proto_service, proto_rpc) WHERE kind='grpc'`,
		`CREATE INDEX IF NOT EXISTS idx_endpoints_topic ON api_endpoints(topic) WHERE topic IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_calls_target_http  ON api_calls(target_method, target_path_template) WHERE target_path_template IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_calls_target_proto ON api_calls(target_proto) WHERE target_proto IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_calls_target_topic ON api_calls(target_topic) WHERE target_topic IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_calls_base_url_ref ON api_calls(base_url_ref) WHERE base_url_ref IS NOT NULL`,
	}
}
