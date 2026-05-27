package v1_test

import (
	"errors"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestCurrentSchemaVersionConst(t *testing.T) {
	if schema.CurrentSchemaVersion != "1.0" {
		t.Errorf("CurrentSchemaVersion = %q; want 1.0", schema.CurrentSchemaVersion)
	}
}

func TestSupportedSchemaVersions(t *testing.T) {
	if len(schema.SupportedSchemaVersions) == 0 {
		t.Fatal("SupportedSchemaVersions must not be empty")
	}
	if schema.SupportedSchemaVersions[0] != "1.0" {
		t.Errorf("SupportedSchemaVersions[0] = %q; want 1.0", schema.SupportedSchemaVersions[0])
	}
}

func TestValidateSchemaVersion_Current_Pass(t *testing.T) {
	if err := v1.ValidateSchemaVersion("1.0"); err != nil {
		t.Errorf("expected nil; got %v", err)
	}
}

func TestValidateSchemaVersion_TooOld_Rejected(t *testing.T) {
	err := v1.ValidateSchemaVersion("0.9")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld) {
		t.Errorf("expected ErrSchemaVersionTooOld; got %v", err)
	}
}

func TestValidateSchemaVersion_Unsupported_Rejected(t *testing.T) {
	err := v1.ValidateSchemaVersion("999.0")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionUnsupported) {
		t.Errorf("expected ErrSchemaVersionUnsupported; got %v", err)
	}
}

func TestValidateDoctrineVersion_Valid(t *testing.T) {
	for _, v := range []string{"1.0.0", "0.0.1", "2.3.1", "100.200.300"} {
		if err := v1.ValidateDoctrineVersion(v); err != nil {
			t.Errorf("%q must validate; got %v", v, err)
		}
	}
}

func TestValidateDoctrineVersion_Invalid(t *testing.T) {
	for _, v := range []string{"", "1", "1.0", "1.0.0-alpha", "v1.0.0", "1.0.0.0", "abc"} {
		if err := v1.ValidateDoctrineVersion(v); err == nil {
			t.Errorf("%q must NOT validate; got nil", v)
		}
	}
}

// TestSentinelAliasing — schema/v1's local sentinels MUST be the same value as
// internal/doctrine/errors so external callers ( parser,
// applier) can errors.Is on the canonical sentinel.
func TestSentinelAliasing(t *testing.T) {
	if !errors.Is(v1.ErrTightenViolation, doctrineerrors.ErrTightenViolation) {
		t.Error("v1.ErrTightenViolation must alias doctrineerrors.ErrTightenViolation")
	}
	if !errors.Is(v1.ErrValidationFailed, doctrineerrors.ErrValidationFailed) {
		t.Error("v1.ErrValidationFailed must alias doctrineerrors.ErrValidationFailed")
	}
	if !errors.Is(v1.ErrTransverseOverrideAttempted, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Error("v1.ErrTransverseOverrideAttempted must alias doctrineerrors.ErrTransverseOverrideAttempted")
	}
}
