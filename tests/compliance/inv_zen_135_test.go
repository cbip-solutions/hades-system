package compliance_test

import (
	"errors"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestInv135_TransverseOverrideRejectedAtParse(t *testing.T) {
	for _, src := range []v1.TransverseSource{v1.SourceUserBaseline, v1.SourceUserOverride} {
		err := v1.RejectTransverseOverride(src, map[string]any{"no_stubs": true})
		if err == nil {
			t.Fatalf("source=%v: expected ErrTransverseOverrideAttempted", src)
		}
		if !errors.Is(err, doctrineerrors.ErrTransverseOverrideAttempted) {
			t.Errorf("source=%v: expected ErrTransverseOverrideAttempted; got %v", src, err)
		}
	}
}

func TestInv135_EmbedAllowed(t *testing.T) {
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

func TestInv135_AllFourFieldsFlagged(t *testing.T) {
	raw := map[string]any{
		"no_tech_debt":        true,
		"no_stubs":            true,
		"build_final_product": true,
		"no_defer":            true,
	}
	err := v1.RejectTransverseOverride(v1.SourceUserOverride, raw)
	if err == nil {
		t.Fatal("expected error; even all-true override is rejected (inv-zen-135)")
	}
	var attempt *v1.TransverseOverrideAttempt
	if !errors.As(err, &attempt) {
		t.Fatalf("expected *v1.TransverseOverrideAttempt; got %T", err)
	}
	if len(attempt.Fields) != 4 {
		t.Errorf("Fields len = %d; want 4 (all transverse keys flagged); got %v", len(attempt.Fields), attempt.Fields)
	}
}
