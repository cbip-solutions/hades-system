//go:build test_helpers

package v1

import (
	"testing"
)

func TestGetRuleMetadataInt_UnknownKey(t *testing.T) {
	defer resetTestRevertMetadata()
	s := Schema{SchemaVersion: "1.0"}
	SetTestRevertMetadata(&s, "Whatever", "x", 0.5, 3, 7)

	if got := getRuleMetadataInt(&s, "Whatever", "revert_cooldown_hours"); got != 7 {
		t.Errorf("revert_cooldown_hours: want 7; got %d", got)
	}
	if got := getRuleMetadataInt(&s, "Whatever", "revert_window_sessions"); got != 3 {
		t.Errorf("revert_window_sessions: want 3; got %d", got)
	}
	if got := getRuleMetadataInt(&s, "Whatever", "unknown_key"); got != 0 {
		t.Errorf("unknown_key: want 0; got %d", got)
	}
}

func TestGetRevertCooldownHours_HappyPath(t *testing.T) {
	defer resetTestRevertMetadata()
	s := Schema{SchemaVersion: "1.0"}
	SetTestRevertMetadata(&s, "Workforce.MaxDepth", "cost", 0.0, 0, 12)

	if got := GetRevertCooldownHours(&s, "Workforce.MaxDepth"); got != 12 {
		t.Errorf("want 12; got %d", got)
	}
	if got := GetRevertCooldownHours(&s, "Other.Field"); got != 0 {
		t.Errorf("want 0 on miss; got %d", got)
	}
}

func TestSetTestRevertMetadata_Smoke(t *testing.T) {
	defer resetTestRevertMetadata()
	s := Schema{SchemaVersion: "1.0"}

	SetTestRevertMetadata(&s, "Workforce.MaxDepth", "merge", 0.85, 5, 24)

	if got := GetRevertCooldownHours(&s, "Workforce.MaxDepth"); got != 24 {
		t.Errorf("GetRevertCooldownHours = %d; want 24", got)
	}

	if got := getRuleMetadataInt(&s, "Workforce.MaxDepth", "revert_window_sessions"); got != 5 {
		t.Errorf("getRuleMetadataInt(window) = %d; want 5", got)
	}

	rule, ok := lookupRevertRuleMeta("Workforce.MaxDepth")
	if !ok {
		t.Fatal("lookupRevertRuleMeta must find the inserted rule")
	}
	if rule.Category != "merge" {
		t.Errorf("Category = %q; want merge", rule.Category)
	}
	if rule.ThresholdPct != 0.85 {
		t.Errorf("ThresholdPct = %v; want 0.85", rule.ThresholdPct)
	}
	if rule.WindowSessions != 5 {
		t.Errorf("WindowSessions = %d; want 5", rule.WindowSessions)
	}
	if rule.CooldownH != 24 {
		t.Errorf("CooldownH = %d; want 24", rule.CooldownH)
	}

}

func TestSetRuleMetadata_TypeMismatch_Ignored(t *testing.T) {
	defer resetTestRevertMetadata()
	s := Schema{SchemaVersion: "1.0"}

	setRuleMetadata(&s, "Test.Path", "revert_category", 42)
	setRuleMetadata(&s, "Test.Path", "revert_threshold_pct", "not-a-float")
	setRuleMetadata(&s, "Test.Path", "revert_window_sessions", "not-an-int")
	setRuleMetadata(&s, "Test.Path", "revert_cooldown_hours", 3.14)

	rule, ok := lookupRevertRuleMeta("Test.Path")
	if !ok {
		t.Fatal("rule should be present even with all-bad-types (entry created but values not set)")
	}
	if rule.Category != "" {
		t.Errorf("Category should be empty (int rejected); got %q", rule.Category)
	}
	if rule.ThresholdPct != 0 {
		t.Errorf("ThresholdPct should be 0 (string rejected); got %v", rule.ThresholdPct)
	}
	if rule.WindowSessions != 0 {
		t.Errorf("WindowSessions should be 0 (string rejected); got %d", rule.WindowSessions)
	}
	if rule.CooldownH != 0 {
		t.Errorf("CooldownH should be 0 (float64 rejected, expects int); got %d", rule.CooldownH)
	}

	setRuleMetadata(&s, "Test.Path", "totally_unknown_key", "value")

}
