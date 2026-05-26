// tests/compliance/inv_zen_182_tierspertool_lint_test.go
//
// Spec §8.6 inv-zen-182 compliance test: the tierspertool doctrine
// lint analyzer (internal/doctrine/lint/analyzers/tierspertool) MUST
// be wired into the Plan 8 lint stack (cmd/zen-doctrine-lint/main.go).
// Static analysis at lint time catches drift between the catalog
// (Phase A internal/onboard/mcp/catalog.go) and the [capa_firewall.tiers]
// TOML section.
//
// location per spec §8.6.
package compliance

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/tierspertool"
)

func TestInvZen182_TiersPerToolAnalyzerExports(t *testing.T) {
	t.Parallel()

	if got := tierspertool.SeverityError.String(); got != "error" {
		t.Errorf("SeverityError.String() = %q; want 'error'", got)
	}
	if got := tierspertool.SeverityWarning.String(); got != "warning" {
		t.Errorf("SeverityWarning.String() = %q; want 'warning'", got)
	}
}

func TestInvZen182_AnalyzerRejectsInvalidTierKey(t *testing.T) {
	t.Parallel()
	doctrineTOML := []byte(`
schema_version = 1
name = "test"
[capa_firewall.tiers]
"validmcp.invalidtier" = "totally-invalid"
"empty." = "low"
`)
	v := tierspertool.NewValidator(nil)
	issues, err := v.ValidateDoctrineFile("test.toml", doctrineTOML)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) < 2 {
		t.Errorf("issue count = %d; want ≥2 (invalid-tier + empty. tool)", len(issues))
	}
}

func TestInvZen182_AnalyzerAcceptsValidTOML(t *testing.T) {
	t.Parallel()
	doctrineTOML := []byte(`
schema_version = 1
name = "test"
[capa_firewall.tiers]
"filesystem-write" = "high"
"filesystem-read.read" = "medium"
"sequential-thinking.think" = "low"
`)
	v := tierspertool.NewValidator(nil)
	issues, err := v.ValidateDoctrineFile("test.toml", doctrineTOML)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	for _, iss := range issues {
		if iss.Severity == tierspertool.SeverityError {
			t.Errorf("unexpected error issue on valid TOML: %s", iss.Reason)
		}
	}
}
