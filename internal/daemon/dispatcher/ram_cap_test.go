package dispatcher

import "testing"

func TestEffectiveCapDefaults(t *testing.T) {
	cfg := DefaultRAMConfig()
	cfg.TotalRAMBytes = 64 * 1024 * 1024 * 1024
	got := EffectiveCap(cfg)
	if got <= 0 {
		t.Errorf("EffectiveCap = %d, want > 0", got)
	}
	if got > cfg.HardCap {
		t.Errorf("EffectiveCap = %d, exceeds HardCap %d", got, cfg.HardCap)
	}
}

func TestEffectiveCapHonoursInitialCap(t *testing.T) {
	cfg := DefaultRAMConfig()
	cfg.TotalRAMBytes = 64 * 1024 * 1024 * 1024
	cfg.InitialUntilMeasured = 50
	got := EffectiveCap(cfg)
	if got != 50 {
		t.Errorf("EffectiveCap = %d, want 50 (InitialUntilMeasured)", got)
	}
}

func TestEffectiveCapZeroRAMReturnsInitial(t *testing.T) {
	cfg := DefaultRAMConfig()
	cfg.TotalRAMBytes = 0
	got := EffectiveCap(cfg)
	if got != cfg.InitialUntilMeasured {
		t.Errorf("EffectiveCap = %d, want %d (initial)", got, cfg.InitialUntilMeasured)
	}
}

// TestEffectiveCapNegativeAvailableReturnsZero exercises the
// `available <= 0` branch in EffectiveCap: if SafetyMarginBytes exceeds
// TotalRAMBytes the system has no headroom for any subagent and the cap
// MUST clamp to 0 (refuse to spawn) rather than wrap to a negative
// dynamic count or fall through to the InitialUntilMeasured floor.
// Setting InitialUntilMeasured=0 ensures the override branch does not
// re-raise the cap.
func TestEffectiveCapNegativeAvailableReturnsZero(t *testing.T) {
	cfg := DefaultRAMConfig()
	cfg.TotalRAMBytes = 1024
	cfg.SafetyMarginBytes = 2048
	cfg.InitialUntilMeasured = 0
	if got := EffectiveCap(cfg); got != 0 {
		t.Errorf("EffectiveCap with negative available = %d, want 0", got)
	}
}

func TestCurrentRAMPressureNonEmpty(t *testing.T) {
	s, err := CurrentRAMPressure()
	if err != nil {
		t.Fatalf("CurrentRAMPressure: %v", err)
	}
	if s == "" {
		t.Error("expected non-empty pressure string")
	}
}
