package federation

import (
	"strings"
	"testing"
)

func TestSchemaStatementsCoverC2(t *testing.T) {
	all := strings.Join(schemaStatements(), "\n")
	wants := []string{

		"CREATE TABLE IF NOT EXISTS caronte_workspaces",
		"CREATE TABLE IF NOT EXISTS caronte_workspace_members",
		"CREATE TABLE IF NOT EXISTS contract_links",
		"CREATE TABLE IF NOT EXISTS breaking_changes",
		"CREATE TABLE IF NOT EXISTS breaking_change_consumers",

		"CREATE INDEX IF NOT EXISTS idx_contract_links_endpoint ON contract_links(endpoint_id, endpoint_repo, workspace_id)",
		"CREATE INDEX IF NOT EXISTS idx_contract_links_call     ON contract_links(call_id, call_repo, workspace_id)",
		"CREATE INDEX IF NOT EXISTS idx_breaking_changes_endpoint ON breaking_changes(endpoint_id, endpoint_repo, workspace_id)",
		"CREATE INDEX IF NOT EXISTS idx_break_consumers_call ON breaking_change_consumers(call_repo, call_id)",

		"confidence IN ('exact_proto_import','spec_artifact','static_path','fuzzy_path')",
		"link_method IN ('artifact','caronte_yaml','static','fuzzy')",
		"detector_id IN ('oasdiff','buf','gqlparser','node-graphql-inspector')",

		"policy_text     TEXT",

		"FOREIGN KEY (workspace_id) REFERENCES caronte_workspaces(workspace_id) ON DELETE CASCADE",
		"FOREIGN KEY (change_id) REFERENCES breaking_changes(change_id) ON DELETE CASCADE",

		"PRIMARY KEY (call_id, endpoint_id, workspace_id)",
		"PRIMARY KEY (change_id, call_id, call_repo)",
		"PRIMARY KEY (workspace_id, project_id)",
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

func TestSchemaStatementsReturnFresh(t *testing.T) {
	a := schemaStatements()
	b := schemaStatements()
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("schemaStatements() returned an empty slice")
	}
	a[0] = "MUTATED"
	if b[0] == "MUTATED" {
		t.Error("schemaStatements() returned a shared backing array; callers can mutate the canonical DDL")
	}
}
