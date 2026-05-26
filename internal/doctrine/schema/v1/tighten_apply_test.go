package v1_test

import (
	"errors"
	"strings"
	"testing"

	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestValidateTighten_NoOverride(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Fatalf("identical override must pass; got %v", err)
	}
}

func TestValidateTighten_DecreaseTighten(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	ov.Workforce.MaxDepth = 4
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("decrease tighten must pass; got %v", err)
	}
}

func TestValidateTighten_DecreaseLoosen_Rejected(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	ov.Workforce.MaxDepth = 16
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on loosen")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
	var v *v1.TightenViolation
	if !errors.As(err, &v) {
		t.Fatal("expected *TightenViolation in error chain")
	}
	if v.RulePath != "Workforce.MaxDepth" {
		t.Errorf("RulePath = %q; want Workforce.MaxDepth", v.RulePath)
	}
	if v.Direction != "decrease" {
		t.Errorf("Direction = %q; want decrease", v.Direction)
	}
}

func TestValidateTighten_IncreaseTighten(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	ov.Workforce.MinDepth = 3
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("increase tighten must pass; got %v", err)
	}
}

func TestValidateTighten_IncreaseLoosen_Rejected(t *testing.T) {
	bs := goodSchema()
	bs.Workforce.MinDepth = 3
	ov := goodSchema()
	ov.Workforce.MinDepth = 1
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on loosen")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
}

func TestValidateTighten_TruthSame_Pass(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("identical truth field must pass; got %v", err)
	}
}

func TestValidateTighten_TruthDifferent_Rejected(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	ov.Transverse.NoStubs = false
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on truth violation")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
}

func TestValidateTighten_AddOnly_Superset_Pass(t *testing.T) {
	bs := goodSchema()
	bs.Gates.TestTiers.Enabled = []string{"unit", "integration"}
	ov := goodSchema()
	ov.Gates.TestTiers.Enabled = []string{"unit", "integration", "compliance", "analysistest"}
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("superset must pass; got %v", err)
	}
}

func TestValidateTighten_AddOnly_Removal_Rejected(t *testing.T) {
	bs := goodSchema()
	bs.Gates.TestTiers.Enabled = []string{"unit", "integration", "compliance"}
	ov := goodSchema()
	ov.Gates.TestTiers.Enabled = []string{"unit", "integration"}
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on add-only removal")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
	if !strings.Contains(err.Error(), "compliance") {
		t.Errorf("error should cite removed value 'compliance'; got %q", err.Error())
	}
}

func TestValidateTighten_RankTighten(t *testing.T) {
	bs := goodSchema()
	bs.Autonomy.Mode = "agent"
	ov := goodSchema()
	ov.Autonomy.Mode = "assisted"
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("rank tighten must pass; got %v", err)
	}
}

func TestValidateTighten_RankLoosen_Rejected(t *testing.T) {
	bs := goodSchema()
	bs.Autonomy.Mode = "assisted"
	ov := goodSchema()
	ov.Autonomy.Mode = "pure"
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on rank loosen")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
}

func TestValidateTighten_BidirectionalEither_Pass(t *testing.T) {
	bs := goodSchema()
	ov1 := goodSchema()
	ov1.Notifications.QuietHoursStart = "21:00"
	if err := ov1.ValidateTighten(&bs); err != nil {
		t.Errorf("bidirectional shift must pass (a); got %v", err)
	}
	ov2 := goodSchema()
	ov2.Notifications.QuietHoursStart = "23:30"
	if err := ov2.ValidateTighten(&bs); err != nil {
		t.Errorf("bidirectional shift must pass (b); got %v", err)
	}
}

func TestValidateTighten_DoctrineVersion_BumpAllowed(t *testing.T) {
	bs := goodSchema()
	bs.DoctrineVersion = "1.0.0"
	ov := goodSchema()
	ov.DoctrineVersion = "1.0.1"
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("DoctrineVersion bump must pass; got %v", err)
	}
}

func TestValidateTighten_DoctrineVersion_RegressionRejected(t *testing.T) {
	bs := goodSchema()
	bs.DoctrineVersion = "1.0.1"
	ov := goodSchema()
	ov.DoctrineVersion = "1.0.0"
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on doctrine_version regression")
	}
}

func TestValidateTighten_DoctrineVersion_MalformedSemver_Rejected(t *testing.T) {
	cases := []struct {
		desc                    string
		baseline, override      string
		mustContainErrSubstring string
	}{
		{"non-numeric override", "1.0.0", "abc.def.ghi", "malformed semver"},
		{"non-numeric baseline", "abc.def.ghi", "1.0.0", "malformed semver"},
		{"4-segment override", "1.0.0", "1.0.0.1", "3 segments"},
		{"2-segment override", "1.0.0", "1.0", "3 segments"},
		{"empty override", "1.0.0", "", "3 segments"},
		{"pre-release suffix", "1.0.0", "1.0.0-alpha", "not numeric"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			bs := goodSchema()
			bs.DoctrineVersion = c.baseline
			ov := goodSchema()
			ov.DoctrineVersion = c.override
			err := ov.ValidateTighten(&bs)
			if err == nil {
				t.Fatalf("expected error on malformed semver (%s)", c.desc)
			}
			if !errors.Is(err, v1.ErrTightenViolation) {
				t.Errorf("expected ErrTightenViolation; got %v", err)
			}
			if !strings.Contains(err.Error(), c.mustContainErrSubstring) {
				t.Errorf("error must contain %q; got %q", c.mustContainErrSubstring, err.Error())
			}
		})
	}
}

func TestValidateTighten_MultipleViolations_AllReported(t *testing.T) {
	bs := goodSchema()
	ov := goodSchema()
	ov.Workforce.MaxDepth = 16
	ov.Transverse.NoStubs = false
	ov.Autonomy.Mode = "pure"
	bs.Autonomy.Mode = "assisted"
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"Workforce.MaxDepth", "Transverse.NoStubs", "Autonomy.Mode"} {
		if !strings.Contains(msg, want) {
			t.Errorf("multi-violation message missing %q; got %q", want, msg)
		}
	}
}

func TestValidateTighten_GatewayDisabledTools_Superset_Pass(t *testing.T) {
	bs := goodSchema()
	bs.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_caronte_context",
	}
	ov := goodSchema()
	ov.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_caronte_context",
		"mcp_zen-swarm_caronte_impact",
		"mcp_zen-swarm_research_agentic",
	}
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("superset (add-only) must pass; got %v", err)
	}
}

func TestValidateTighten_GatewayDisabledTools_Removal_Rejected(t *testing.T) {
	bs := goodSchema()
	bs.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_caronte_context",
		"mcp_zen-swarm_caronte_impact",
		"mcp_zen-swarm_research_agentic",
	}
	ov := goodSchema()
	ov.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_caronte_context",
		"mcp_zen-swarm_caronte_impact",
	}
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error on add-only removal")
	}
	if !errors.Is(err, v1.ErrTightenViolation) {
		t.Errorf("expected ErrTightenViolation; got %v", err)
	}
	if !strings.Contains(err.Error(), "research_agentic") {
		t.Errorf("error should cite removed value 'research_agentic'; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Gateway.DisabledTools") {
		t.Errorf("error should cite rule path Gateway.DisabledTools; got %q", err.Error())
	}
}

func TestValidateTighten_GatewayDisabledTools_Identical_Pass(t *testing.T) {
	bs := goodSchema()
	bs.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_research_agentic",
	}
	ov := goodSchema()
	ov.Gateway.DisabledTools = []string{
		"mcp_zen-swarm_caronte_query",
		"mcp_zen-swarm_research_agentic",
	}
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("identical override must pass; got %v", err)
	}
}
