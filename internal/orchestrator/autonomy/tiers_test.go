package autonomy_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

func TestTierForCheck_Q13DMatrix(t *testing.T) {
	type cell struct {
		check, doctrine string
		want            autonomy.Tier
	}

	cells := []cell{

		{"research_mcp_up", "max-scope", autonomy.TierHard},
		{"research_mcp_up", "default", autonomy.TierHard},
		{"research_mcp_up", "capa-firewall", autonomy.TierHard},

		{"verify_docs", "max-scope", autonomy.TierHard},
		{"verify_docs", "default", autonomy.TierHard},
		{"verify_docs", "capa-firewall", autonomy.TierHard},

		{"caronte_index_currency", "max-scope", autonomy.TierHard},
		{"caronte_index_currency", "default", autonomy.TierSoft},
		{"caronte_index_currency", "capa-firewall", autonomy.TierHard},

		{"system_state_toml", "max-scope", autonomy.TierHard},
		{"system_state_toml", "default", autonomy.TierSoft},
		{"system_state_toml", "capa-firewall", autonomy.TierHard},

		{"caronte_engine_up", "max-scope", autonomy.TierHard},
		{"caronte_engine_up", "default", autonomy.TierHard},
		{"caronte_engine_up", "capa-firewall", autonomy.TierHard},

		{"adrs_valid", "max-scope", autonomy.TierHard},
		{"adrs_valid", "default", autonomy.TierHard},
		{"adrs_valid", "capa-firewall", autonomy.TierHard},

		{"watcher_running", "max-scope", autonomy.TierHard},
		{"watcher_running", "default", autonomy.TierSoft},
		{"watcher_running", "capa-firewall", autonomy.TierHard},

		{"amendment_dry_run_approved", "max-scope", autonomy.TierHard},
		{"amendment_dry_run_approved", "default", autonomy.TierInformational},
		{"amendment_dry_run_approved", "capa-firewall", autonomy.TierHard},

		{"lint_clean", "max-scope", autonomy.TierHard},
		{"lint_clean", "default", autonomy.TierHard},
		{"lint_clean", "capa-firewall", autonomy.TierHard},

		{"plans_4_9_green", "max-scope", autonomy.TierHard},
		{"plans_4_9_green", "default", autonomy.TierHard},
		{"plans_4_9_green", "capa-firewall", autonomy.TierHard},

		{"ci_consecutive_green", "max-scope", autonomy.TierHard},
		{"ci_consecutive_green", "default", autonomy.TierSoft},
		{"ci_consecutive_green", "capa-firewall", autonomy.TierHard},
	}
	for _, c := range cells {
		got, err := autonomy.TierForCheck(c.check, c.doctrine)
		if err != nil {
			t.Errorf("%s/%s: unexpected error %v", c.check, c.doctrine, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s/%s: want %v got %v", c.check, c.doctrine, c.want, got)
		}
	}
}

func TestTierForCheck_TolerantOfWhitespaceAndCase(t *testing.T) {
	got, err := autonomy.TierForCheck("  research_mcp_up  ", "  Default  ")
	if err != nil {
		t.Fatalf("trimmed/uppercased input must succeed: %v", err)
	}
	if got != autonomy.TierHard {
		t.Fatalf("want TierHard, got %v", got)
	}
}

func TestTierForCheck_UnknownDoctrine_FailsClosed(t *testing.T) {
	if _, err := autonomy.TierForCheck("research_mcp_up", "no-such"); err == nil {
		t.Fatalf("unknown doctrine must error")
	}
}

func TestTierForCheck_UnknownCheck_FailsClosed(t *testing.T) {
	if _, err := autonomy.TierForCheck("no_such_check", "default"); err == nil {
		t.Fatalf("unknown check must error")
	}
}

func TestTierString(t *testing.T) {
	for _, c := range []struct {
		t    autonomy.Tier
		want string
	}{
		{autonomy.TierHard, "hard"},
		{autonomy.TierSoft, "soft"},
		{autonomy.TierInformational, "informational"},
	} {
		if c.t.String() != c.want {
			t.Fatalf("Tier.String %v: want %q got %q", c.t, c.want, c.t.String())
		}
	}
	var bogus autonomy.Tier = 99
	if got := bogus.String(); got != "tier(99)" {
		t.Fatalf("Tier.String unknown: want tier(99), got %q", got)
	}
}

func TestAllChecksHaveAllDoctrines(t *testing.T) {

	for _, ck := range autonomy.AllCheckNames() {
		for _, d := range autonomy.AllDoctrineNames() {
			if _, err := autonomy.TierForCheck(ck, d); err != nil {
				t.Errorf("matrix incomplete: (%s, %s): %v", ck, d, err)
			}
		}
	}
	if got := len(autonomy.AllCheckNames()); got != 11 {
		t.Fatalf("AllCheckNames must list the 11 spec checks; got %d", got)
	}
	if got := len(autonomy.AllDoctrineNames()); got != 3 {
		t.Fatalf("AllDoctrineNames must list 3 doctrines; got %d", got)
	}
}
