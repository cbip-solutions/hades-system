package tessera

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultConfigValues(t *testing.T) {
	c := DefaultConfig()
	if c.BatchMaxAge != 30*time.Second {
		t.Errorf("BatchMaxAge = %v, want 30s (default doctrine per Q4 B)", c.BatchMaxAge)
	}
	if c.BatchMaxSize != 1000 {
		t.Errorf("BatchMaxSize = %d, want 1000 (default doctrine per Q4 B)", c.BatchMaxSize)
	}
	if c.RotationCadenceDays != 365 {
		t.Errorf("RotationCadenceDays = %d, want 365 (default doctrine per Q2 A)", c.RotationCadenceDays)
	}
}

func TestConfigValidateRejectsZeroBatchMaxSize(t *testing.T) {
	c := Config{
		BatchMaxAge:         time.Second,
		BatchMaxSize:        0,
		RotationCadenceDays: 90,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted BatchMaxSize=0; want rejection")
	}
}

func TestConfigValidateRejectsNegativeBatchMaxAge(t *testing.T) {
	c := Config{
		BatchMaxAge:         -time.Second,
		BatchMaxSize:        100,
		RotationCadenceDays: 90,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted negative BatchMaxAge; want rejection")
	}
}

func TestConfigValidateRejectsZeroRotationCadence(t *testing.T) {
	c := Config{
		BatchMaxAge:         time.Second,
		BatchMaxSize:        100,
		RotationCadenceDays: 0,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate accepted RotationCadenceDays=0; want rejection")
	}
}

func TestConfigValidateAcceptsMaxScope(t *testing.T) {

	c := Config{
		BatchMaxAge:         time.Second,
		BatchMaxSize:        100,
		RotationCadenceDays: 90,
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate rejected max-scope config: %v", err)
	}
}

func TestSentinelErrorsExported(t *testing.T) {
	errs := []struct {
		name string
		err  error
	}{
		{"ErrEmptyProjectID", ErrEmptyProjectID},
		{"ErrWitnessKeyMissing", ErrWitnessKeyMissing},
		{"ErrWitnessKeyAlreadyExists", ErrWitnessKeyAlreadyExists},
		{"ErrUnsignedSTH", ErrUnsignedSTH},
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrCrossProjectAccess", ErrCrossProjectAccess},
	}
	for _, e := range errs {
		if e.err == nil {
			t.Errorf("%s is nil; sentinel must be defined", e.name)
		}
	}
}

func TestSentinelErrorsAreDistinguishable(t *testing.T) {
	if errors.Is(ErrEmptyProjectID, ErrWitnessKeyMissing) {
		t.Error("ErrEmptyProjectID and ErrWitnessKeyMissing must be distinguishable")
	}
	if errors.Is(ErrUnsignedSTH, ErrCrossProjectAccess) {
		t.Error("ErrUnsignedSTH and ErrCrossProjectAccess must be distinguishable")
	}
}
