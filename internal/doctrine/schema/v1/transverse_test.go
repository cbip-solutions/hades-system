package v1_test

import (
	"errors"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestTransverseExpected(t *testing.T) {
	got := v1.TransverseExpected()
	if !got.NoTechDebt || !got.NoStubs || !got.BuildFinalProduct || !got.NoDefer {
		t.Fatalf("TransverseExpected = %+v; want all four true", got)
	}
}

func TestRejectTransverseOverride_UserBaseline_Rejected(t *testing.T) {
	raw := map[string]any{"no_tech_debt": false}
	err := v1.RejectTransverseOverride(v1.SourceUserBaseline, raw)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Errorf("expected ErrTransverseOverrideAttempted; got %v", err)
	}
	var attempt *v1.TransverseOverrideAttempt
	if !errors.As(err, &attempt) {
		t.Fatalf("expected *v1.TransverseOverrideAttempt; got %T", err)
	}
	if attempt.Source != v1.SourceUserBaseline.String() {
		t.Errorf("Source = %v; want SourceUserBaseline", attempt.Source)
	}
	if len(attempt.Fields) != 1 || attempt.Fields[0] != "no_tech_debt" {
		t.Errorf("Fields = %v; want [no_tech_debt]", attempt.Fields)
	}
}

func TestRejectTransverseOverride_UserOverride_Rejected(t *testing.T) {
	raw := map[string]any{
		"no_tech_debt":        true,
		"no_stubs":            true,
		"build_final_product": true,
		"no_defer":            true,
	}
	err := v1.RejectTransverseOverride(v1.SourceUserOverride, raw)
	if err == nil {
		t.Fatal("expected error; got nil — even all-true override is rejected (inv-zen-135 hardcoded)")
	}
	if !errors.Is(err, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Errorf("expected ErrTransverseOverrideAttempted; got %v", err)
	}
	var attempt *v1.TransverseOverrideAttempt
	if !errors.As(err, &attempt) {
		t.Fatal("expected *v1.TransverseOverrideAttempt")
	}
	if len(attempt.Fields) != 4 {
		t.Errorf("Fields len = %d; want 4 (all transverse keys flagged)", len(attempt.Fields))
	}
}

func TestRejectTransverseOverride_Embed_Allowed(t *testing.T) {
	raw := map[string]any{
		"no_tech_debt":        true,
		"no_stubs":            true,
		"build_final_product": true,
		"no_defer":            true,
	}
	if err := v1.RejectTransverseOverride(v1.SourceEmbed, raw); err != nil {
		t.Fatalf("embed source must be allowed; got %v", err)
	}
}

func TestRejectTransverseOverride_Empty_Allowed(t *testing.T) {
	for _, src := range []v1.TransverseSource{v1.SourceEmbed, v1.SourceUserBaseline, v1.SourceUserOverride} {
		if err := v1.RejectTransverseOverride(src, nil); err != nil {
			t.Errorf("source=%v empty raw must be allowed; got %v", src, err)
		}
		if err := v1.RejectTransverseOverride(src, map[string]any{}); err != nil {
			t.Errorf("source=%v empty raw must be allowed; got %v", src, err)
		}
	}
}
