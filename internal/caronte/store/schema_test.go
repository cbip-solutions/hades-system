package store

import (
	"strings"
	"testing"
)

func TestVecDimensions(t *testing.T) {
	if vecDimensions != 1536 {
		t.Errorf("vecDimensions = %d; want 1536 (Jina-code, Plan 14)", vecDimensions)
	}
}

func TestSchemaStatementsCoverSpec42(t *testing.T) {
	all := strings.Join(schemaStatements(), "\n")
	wants := []string{
		"CREATE TABLE IF NOT EXISTS graph_nodes",
		"CREATE TABLE IF NOT EXISTS graph_edges",
		"PRIMARY KEY (source_id, target_id, kind, site_line)",
		"CREATE INDEX IF NOT EXISTS idx_edges_target ON graph_edges(target_id, kind)",
		"CREATE INDEX IF NOT EXISTS idx_edges_source ON graph_edges(source_id, kind)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_iface  ON graph_nodes(kind)",
		"CREATE TABLE IF NOT EXISTS co_change_matrix",
		"PRIMARY KEY (file_a, file_b, window_days)",
		"CREATE TABLE IF NOT EXISTS churn_metrics",
		"PRIMARY KEY (path, window_days)",
		"CREATE TABLE IF NOT EXISTS adr_links",
		"PRIMARY KEY (adr_id, node_id, link_kind)",
		"CREATE TABLE IF NOT EXISTS lore_trailers",
		"PRIMARY KEY (commit_sha, trailer_kind, body)",
		"CREATE VIRTUAL TABLE IF NOT EXISTS code_node_vec  USING vec0(embedding float[1536])",
		"CREATE VIRTUAL TABLE IF NOT EXISTS graph_nodes_fts USING fts5(name, signature, doc, content='graph_nodes', content_rowid='rowid')",
	}
	for _, w := range wants {
		if !strings.Contains(all, w) {
			t.Errorf("schemaStatements() missing fragment:\n  %s", w)
		}
	}
}

func TestSchemaStatementsAllIdempotent(t *testing.T) {
	for i, s := range schemaStatements() {
		up := strings.ToUpper(s)
		if strings.HasPrefix(strings.TrimSpace(up), "CREATE") && !strings.Contains(up, "IF NOT EXISTS") {
			t.Errorf("schemaStatements()[%d] is a CREATE without IF NOT EXISTS:\n%s", i, s)
		}
	}
}

func TestSchemaStatementsCoverAPIEndpointsAndCalls(t *testing.T) {
	all := strings.Join(schemaStatements(), "\n")
	wants := []string{

		"CREATE TABLE IF NOT EXISTS api_endpoints",
		"CREATE TABLE IF NOT EXISTS api_calls",

		"endpoint_id       TEXT PRIMARY KEY",
		"kind              TEXT NOT NULL CHECK (kind IN ('http','grpc','graphql','mq','ws'))",
		"handler_node_id   TEXT NOT NULL",
		"extracted_at      INTEGER NOT NULL",
		"extractor_id      TEXT NOT NULL",

		"call_id              TEXT PRIMARY KEY",
		"caller_node_id       TEXT NOT NULL",
		"confidence           TEXT NOT NULL CHECK (confidence IN ('exact_proto_import','spec_artifact','static_path','fuzzy_path'))",
		"extracted_at         INTEGER NOT NULL",
		"extractor_id         TEXT NOT NULL",

		"CREATE INDEX IF NOT EXISTS idx_endpoints_http  ON api_endpoints(kind, method, path_template) WHERE kind='http'",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_proto ON api_endpoints(proto_service, proto_rpc) WHERE kind='grpc'",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_topic ON api_endpoints(topic) WHERE topic IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_calls_target_http  ON api_calls(target_method, target_path_template) WHERE target_path_template IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_calls_target_proto ON api_calls(target_proto) WHERE target_proto IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_calls_target_topic ON api_calls(target_topic) WHERE target_topic IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_calls_base_url_ref ON api_calls(base_url_ref) WHERE base_url_ref IS NOT NULL",
	}
	for _, w := range wants {
		if !strings.Contains(all, w) {
			t.Errorf("schemaStatements() missing fragment:\n  %s", w)
		}
	}
}
