package reload_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestDefaultValidator_NilSchemaRejected(t *testing.T) {
	v := reload.NewDefaultValidator()
	err := v.Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) = nil; want error")
	}
	if !strings.Contains(err.Error(), "nil schema") {
		t.Errorf("error message = %q; want contains 'nil schema'", err.Error())
	}
}

func TestDefaultValidator_ValidateTighten_NilCandidateRejected(t *testing.T) {
	v := reload.NewDefaultValidator()
	baseline := builtin.MaxScope()
	err := v.ValidateTighten(baseline, nil)
	if err == nil {
		t.Fatal("ValidateTighten(baseline, nil) = nil; want error")
	}
	if !strings.Contains(err.Error(), "nil candidate") {
		t.Errorf("error message = %q; want contains 'nil candidate'", err.Error())
	}
}

func TestDefaultValidator_ValidateTighten_NilBaselineRejected(t *testing.T) {
	v := reload.NewDefaultValidator()
	candidate := builtin.MaxScope()
	err := v.ValidateTighten(nil, candidate)
	if err == nil {
		t.Fatal("ValidateTighten(nil, candidate) = nil; want error")
	}
	if !strings.Contains(err.Error(), "nil baseline") {
		t.Errorf("error message = %q; want contains 'nil baseline'", err.Error())
	}
}

func TestDefaultValidator_HappyPath_ProductionSchema(t *testing.T) {
	v := reload.NewDefaultValidator()
	if err := v.Validate(builtin.MaxScope()); err != nil {
		t.Errorf("Validate(MaxScope) = %v; want nil (fully-valid schema)", err)
	}
	if err := v.Validate(builtin.Default()); err != nil {
		t.Errorf("Validate(Default) = %v; want nil", err)
	}
}

func TestDefaultValidator_RejectsInvalidSchema(t *testing.T) {
	v := reload.NewDefaultValidator()
	bad := *builtin.MaxScope()
	bad.SchemaVersion = ""
	err := v.Validate(&bad)
	if err == nil {
		t.Fatal("Validate(empty SchemaVersion) = nil; want error")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed wrap; got %v", err)
	}
}

func TestDefaultValidator_ValidateTighten_HappyPath(t *testing.T) {
	v := reload.NewDefaultValidator()
	baseline := builtin.MaxScope()

	cand := *baseline
	if err := cand.Validate(); err != nil {
		t.Fatalf("candidate Validate prep: %v", err)
	}
	if err := v.ValidateTighten(baseline, &cand); err != nil {
		t.Errorf("ValidateTighten(MaxScope, MaxScope) = %v; want nil (no loosen)", err)
	}
}
