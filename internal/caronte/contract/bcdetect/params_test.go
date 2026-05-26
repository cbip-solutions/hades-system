package bcdetect

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultParamsMatchSpec(t *testing.T) {
	p := DefaultParams()
	if p.MaxSpecBytes != 5*1024*1024 {
		t.Errorf("MaxSpecBytes = %d; want 5 MiB", p.MaxSpecBytes)
	}
	if p.NodeBinaryPath != "" {
		t.Errorf("NodeBinaryPath = %q; want \"\"", p.NodeBinaryPath)
	}
	if p.NodeSpawnTimeout != 30*time.Second {
		t.Errorf("NodeSpawnTimeout = %v; want 30s", p.NodeSpawnTimeout)
	}
	if p.BufRulesetLevel != "WIRE_JSON" {
		t.Errorf("BufRulesetLevel = %q; want WIRE_JSON", p.BufRulesetLevel)
	}
}

// TestDefaultParamsValidate gates that the defaults pass Validate (they
// MUST; defaults are doctrine-vetted).
func TestDefaultParamsValidate(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Errorf("DefaultParams().Validate() = %v; want nil", err)
	}
}

func TestParamsValidateEnforcesFloors(t *testing.T) {
	cases := []struct {
		name string
		p    Params
	}{
		{"MaxSpecBytes below 64 KiB floor", Params{
			MaxSpecBytes:     63 * 1024,
			NodeSpawnTimeout: 30 * time.Second,
			BufRulesetLevel:  "WIRE_JSON",
		}},
		{"NodeSpawnTimeout below 1s floor", Params{
			MaxSpecBytes:     5 * 1024 * 1024,
			NodeSpawnTimeout: 999 * time.Millisecond,
			BufRulesetLevel:  "WIRE_JSON",
		}},
		{"BufRulesetLevel unknown", Params{
			MaxSpecBytes:     5 * 1024 * 1024,
			NodeSpawnTimeout: 30 * time.Second,
			BufRulesetLevel:  "TOTAL",
		}},
		{"BufRulesetLevel empty", Params{
			MaxSpecBytes:     5 * 1024 * 1024,
			NodeSpawnTimeout: 30 * time.Second,
			BufRulesetLevel:  "",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if !errors.Is(err, ErrParamsBelowFloor) {
				t.Errorf("Validate() = %v; want ErrParamsBelowFloor", err)
			}
		})
	}
}

func TestParamsValidateAcceptsTuningAboveFloor(t *testing.T) {
	cases := []struct {
		name string
		p    Params
	}{
		{"50 MiB MaxSpecBytes", Params{
			MaxSpecBytes:     50 * 1024 * 1024,
			NodeSpawnTimeout: 30 * time.Second,
			BufRulesetLevel:  "WIRE_JSON",
		}},
		{"60s NodeSpawnTimeout + FILE level", Params{
			MaxSpecBytes:     5 * 1024 * 1024,
			NodeSpawnTimeout: 60 * time.Second,
			BufRulesetLevel:  "FILE",
		}},
		{"PACKAGE level + custom NodeBinaryPath", Params{
			MaxSpecBytes:     1 * 1024 * 1024,
			NodeBinaryPath:   "/usr/local/bin/node",
			NodeSpawnTimeout: 5 * time.Second,
			BufRulesetLevel:  "PACKAGE",
		}},
		{"WIRE level minimum acceptable", Params{
			MaxSpecBytes:     64 * 1024,
			NodeSpawnTimeout: time.Second,
			BufRulesetLevel:  "WIRE",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.p.Validate(); err != nil {
				t.Errorf("Validate() = %v; want nil", err)
			}
		})
	}
}
