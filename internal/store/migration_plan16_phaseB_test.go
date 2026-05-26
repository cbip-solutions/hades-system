package store

import (
	"testing"
)

func TestMigration064_CostLedgerProviderColumn(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	rows, err := st.DB().Query(`SELECT name FROM pragma_table_info('cost_ledger')`)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if col == "provider" {
			found = true
		}
	}
	if !found {
		t.Error("cost_ledger.provider column missing after migration 064")
	}

	_, err = st.DB().Exec(`INSERT INTO cost_ledger
		(idempotency_key, ts, project, profile, tier, model, input_tokens, output_tokens, cost_usd)
		VALUES ('idem-064', 1, 'p', 'pr', 'inhouse', 'm', 1, 1, 0.0)`)
	if err != nil {
		t.Fatalf("insert without provider: %v", err)
	}
	var prov string
	if err := st.DB().QueryRow(`SELECT provider FROM cost_ledger WHERE idempotency_key='idem-064'`).Scan(&prov); err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if prov != "" {
		t.Errorf("default provider = %q, want empty string", prov)
	}
}

func TestMigration065_TierHealthSamplesTable(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()

	var name string
	err := st.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='tier_health_samples'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("tier_health_samples table missing after migration 065: %v", err)
	}

	_, err = st.DB().Exec(`INSERT INTO tier_health_samples
		(ts, provider, tier, success, latency_ms, error_pattern)
		VALUES (?, ?, ?, ?, ?, ?)`,
		1700000000000, "deepseek-direct", "openai-compat", 1, 142, "")
	if err != nil {
		t.Fatalf("insert tier_health_samples row: %v", err)
	}

	var idx string
	err = st.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tier_health_provider_ts'`,
	).Scan(&idx)
	if err != nil {
		t.Errorf("idx_tier_health_provider_ts index missing: %v", err)
	}
}
