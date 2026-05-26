package tierspertool_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/tierspertool"
)

var curatedMCPs = []string{"playwright", "sequential-thinking", "filesystem-write"}

func TestValidateCleanProducesNoIssues(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "clean", "clean.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	v := tierspertool.NewValidator(curatedMCPs)
	issues, err := v.ValidateDoctrineFile("clean.toml", body)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("clean fixture produced %d issues; want 0; issues=%+v", len(issues), issues)
	}
}

func TestValidateUnknownMCPSurfacesIssue(t *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "violation_unknown_mcp", "violation.toml"))
	v := tierspertool.NewValidator(curatedMCPs)
	issues, err := v.ValidateDoctrineFile("unknown.toml", body)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(issues))
	}
	if issues[0].Severity != tierspertool.SeverityError {
		t.Errorf("severity = %v, want SeverityError", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Reason, "unknown MCP name") {
		t.Errorf("reason = %q, want substring 'unknown MCP name'", issues[0].Reason)
	}
}

func TestValidateInvalidTierSurfacesIssue(t *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "violation_invalid_tier", "violation.toml"))
	v := tierspertool.NewValidator(curatedMCPs)
	issues, err := v.ValidateDoctrineFile("invalid.toml", body)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(issues))
	}
	if !strings.Contains(issues[0].Reason, "invalid tier value") {
		t.Errorf("reason = %q, want substring 'invalid tier value'", issues[0].Reason)
	}
}

func TestValidateMalformedSurfacesIssues(t *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "violation_malformed", "violation.toml"))
	v := tierspertool.NewValidator(curatedMCPs)
	issues, err := v.ValidateDoctrineFile("malformed.toml", body)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2 (.dangling + playwright.)", len(issues))
	}
	gotReasons := []string{issues[0].Reason, issues[1].Reason}
	wantSubs := []string{"empty MCP name", "empty tool name"}
	for _, want := range wantSubs {
		found := false
		for _, got := range gotReasons {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no issue mentions %q; got=%+v", want, gotReasons)
		}
	}
}

func TestValidateEmptyCatalogSkipsMembership(t *testing.T) {
	v := tierspertool.NewValidator(nil)
	body := []byte(`[capa_firewall.tiers]
"unknown-but-valid-key" = "low"
`)
	issues, err := v.ValidateDoctrineFile("x.toml", body)
	if err != nil {
		t.Fatalf("ValidateDoctrineFile: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("empty catalog should skip membership check; got issues=%+v", issues)
	}
}

func TestValidateTOMLParseError(t *testing.T) {
	v := tierspertool.NewValidator(curatedMCPs)
	_, err := v.ValidateDoctrineFile("bad.toml", []byte("[broken syntax"))
	if err == nil {
		t.Errorf("ValidateDoctrineFile broken: err=nil, want non-nil")
	}
}

func TestSeverityStringStable(t *testing.T) {
	if tierspertool.SeverityError.String() != "error" {
		t.Errorf("SeverityError.String = %q, want 'error'", tierspertool.SeverityError.String())
	}
	if tierspertool.SeverityWarning.String() != "warning" {
		t.Errorf("SeverityWarning.String = %q, want 'warning'", tierspertool.SeverityWarning.String())
	}
	if tierspertool.Severity(99).String() != "unknown" {
		t.Errorf("OOR.String = %q, want 'unknown'", tierspertool.Severity(99).String())
	}
}

func TestValidateOrderedKeys(t *testing.T) {
	body := []byte(`[capo_firewall.tiers]
"z-mcp" = "high"
"a-mcp" = "high"
`)

	v := tierspertool.NewValidator(curatedMCPs)
	issues, _ := v.ValidateDoctrineFile("wrong-table.toml", body)
	if len(issues) != 0 {
		t.Errorf("wrong-table TOML produced issues; want 0: %+v", issues)
	}
}
