// SPDX-License-Identifier: MIT
package federation

func schemaStatements() []string {
	return []string{

		"CREATE TABLE IF NOT EXISTS caronte_workspaces (\n  workspace_id    TEXT PRIMARY KEY,\n  owning_project  TEXT NOT NULL,\n  policy_locked   INTEGER NOT NULL,\n  created_at      INTEGER NOT NULL,\n  schema_version  INTEGER NOT NULL,\n  -- policy_text: operator-mutable policy carrier (JSON-encoded doctrine\n  --             snapshot). Distinct from policy_locked (which is the\n  --             registration-time snapshot, immutable). SetWorkspacePolicy\n  --             writes here; GetWorkspacePolicy reads here. NULLABLE so a\n  --             workspace registered before a policy is set has no row drift.\n  policy_text     TEXT,\n  -- enable_graphql_node_fallback: HADES design stage invariant opt-in flag.\n  --              When 1, the bcdetect graphql detector MAY shell out to\n  --              node graphql-inspector when the Go path returns\n  --              SevInsufficient AND the workspace consents. When 0\n  --              (default), the graphql detector returns SevInsufficient\n  --              verbatim and the operator sees the unclassified rule\n  --              surfaced. The bcdetect package consults this via the\n  --              workspace seam; stage hades workspace policy set surfaces\n  --              the operator-facing knob.\n  enable_graphql_node_fallback INTEGER NOT NULL DEFAULT 0\n)",

		`CREATE TABLE IF NOT EXISTS caronte_workspace_members (
  workspace_id    TEXT NOT NULL,
  project_id      TEXT NOT NULL,
  registered_at   INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, project_id),
  FOREIGN KEY (workspace_id) REFERENCES caronte_workspaces(workspace_id) ON DELETE CASCADE
)`,

		`CREATE TABLE IF NOT EXISTS contract_links (
  call_id         TEXT NOT NULL, call_repo TEXT NOT NULL,
  endpoint_id     TEXT NOT NULL, endpoint_repo TEXT NOT NULL,
  confidence      TEXT NOT NULL CHECK (confidence IN ('exact_proto_import','spec_artifact','static_path','fuzzy_path')),
  workspace_id    TEXT NOT NULL,
  resolved_at     INTEGER NOT NULL,
  link_method     TEXT NOT NULL CHECK (link_method IN ('artifact','caronte_yaml','static','fuzzy')),
  PRIMARY KEY (call_id, endpoint_id, workspace_id),
  FOREIGN KEY (workspace_id) REFERENCES caronte_workspaces(workspace_id) ON DELETE CASCADE
)`,

		`CREATE INDEX IF NOT EXISTS idx_contract_links_endpoint ON contract_links(endpoint_id, endpoint_repo, workspace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contract_links_call     ON contract_links(call_id, call_repo, workspace_id)`,

		`CREATE TABLE IF NOT EXISTS breaking_changes (
  change_id       TEXT PRIMARY KEY,
  workspace_id    TEXT NOT NULL,
  endpoint_id     TEXT NOT NULL, endpoint_repo TEXT NOT NULL,
  kind            TEXT NOT NULL,
  detail          TEXT NOT NULL,
  detected_at     INTEGER NOT NULL,
  detector_id     TEXT NOT NULL CHECK (detector_id IN ('oasdiff','buf','gqlparser','node-graphql-inspector')),
  lore_author     TEXT, lore_commit_sha TEXT,
  lore_adr_refs   TEXT,
  lore_supersedes TEXT,
  FOREIGN KEY (workspace_id) REFERENCES caronte_workspaces(workspace_id) ON DELETE CASCADE
)`,
		`CREATE INDEX IF NOT EXISTS idx_breaking_changes_endpoint ON breaking_changes(endpoint_id, endpoint_repo, workspace_id)`,

		`CREATE TABLE IF NOT EXISTS breaking_change_consumers (
  change_id    TEXT NOT NULL,
  call_id      TEXT NOT NULL, call_repo TEXT NOT NULL,
  PRIMARY KEY (change_id, call_id, call_repo),
  FOREIGN KEY (change_id) REFERENCES breaking_changes(change_id) ON DELETE CASCADE
)`,
		`CREATE INDEX IF NOT EXISTS idx_break_consumers_call ON breaking_change_consumers(call_repo, call_id)`,

		`CREATE TABLE IF NOT EXISTS unresolved_calls (
  workspace_id  TEXT NOT NULL,
  call_id       TEXT NOT NULL,
  call_repo     TEXT NOT NULL,
  base_url_ref  TEXT NOT NULL,
  reason        TEXT NOT NULL,
  recorded_at   INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, call_id, call_repo),
  FOREIGN KEY (workspace_id) REFERENCES caronte_workspaces(workspace_id) ON DELETE CASCADE
)`,
		`CREATE INDEX IF NOT EXISTS idx_unresolved_calls_repo ON unresolved_calls(call_repo, workspace_id)`,
	}
}
