// SPDX-License-Identifier: MIT
package merge

type Mode int

const (
	// ModeUnknown is the zero value and MUST NOT be selected by the
	// orchestrator. ModeFor(ModeUnknown) panics.
	ModeUnknown Mode = iota

	ModeNormal

	ModeDegraded60

	ModeDegraded80

	ModeEmergencyOnly

	ModeHighRisk
)

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "Normal"
	case ModeDegraded60:
		return "Degraded60"
	case ModeDegraded80:
		return "Degraded80"
	case ModeEmergencyOnly:
		return "EmergencyOnly"
	case ModeHighRisk:
		return "HighRisk"
	default:
		return "Unknown"
	}
}

func AllModes() []Mode {
	return []Mode{
		ModeNormal,
		ModeDegraded60,
		ModeDegraded80,
		ModeEmergencyOnly,
		ModeHighRisk,
	}
}

type TestTier int

const (
	TestTierUnknown TestTier = iota

	TestTierFull

	TestTierSmoke

	TestTierSmokeFailFast
)

func (t TestTier) String() string {
	switch t {
	case TestTierFull:
		return "Full"
	case TestTierSmoke:
		return "Smoke"
	case TestTierSmokeFailFast:
		return "SmokeFailFast"
	default:
		return "Unknown"
	}
}

type ModeConfig struct {
	MaxCandidates int

	TestTier TestTier

	FlakeRerunBudget int
}

// ModeFor returns the canonical ModeConfig for the given Mode. Panics
// on ModeUnknown or out-of-range — release cost_gating MUST always
// resolve to a valid Mode; an unmapped Mode is a contract violation
// upstream and the engine fails fast (defense-in-depth).
func ModeFor(m Mode) ModeConfig {
	switch m {
	case ModeNormal:
		return ModeConfig{
			MaxCandidates:    3,
			TestTier:         TestTierFull,
			FlakeRerunBudget: 2,
		}
	case ModeDegraded60:
		return ModeConfig{
			MaxCandidates:    2,
			TestTier:         TestTierFull,
			FlakeRerunBudget: 1,
		}
	case ModeDegraded80:
		return ModeConfig{
			MaxCandidates:    1,
			TestTier:         TestTierSmoke,
			FlakeRerunBudget: 0,
		}
	case ModeEmergencyOnly:
		return ModeConfig{
			MaxCandidates:    1,
			TestTier:         TestTierSmokeFailFast,
			FlakeRerunBudget: 0,
		}
	case ModeHighRisk:

		return ModeConfig{
			MaxCandidates:    3,
			TestTier:         TestTierFull,
			FlakeRerunBudget: 3,
		}
	default:
		panic("merge.ModeFor: unknown Mode (Plan 5 mapping bug — Mode must be set by orchestrator before invoking Merge)")
	}
}

func EscalateForBlastRadius(base Mode, level string) Mode {
	if level == "high" {
		return ModeHighRisk
	}
	return base
}
