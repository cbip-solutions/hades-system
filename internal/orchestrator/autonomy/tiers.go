// SPDX-License-Identifier: MIT
package autonomy

import (
	"fmt"
	"strings"
)

type Tier uint8

const (
	TierHard Tier = iota + 1

	TierSoft

	TierInformational
)

func (t Tier) String() string {
	switch t {
	case TierHard:
		return "hard"
	case TierSoft:
		return "soft"
	case TierInformational:
		return "informational"
	default:
		return fmt.Sprintf("tier(%d)", uint8(t))
	}
}

const (
	CheckResearchMCPUp           = "research_mcp_up"
	CheckVerifyDocs              = "verify_docs"
	CheckCaronteIndexCurrency    = "caronte_index_currency"
	CheckSystemStateTOML         = "system_state_toml"
	CheckCaronteEngineUp         = "caronte_engine_up"
	CheckADRsValid               = "adrs_valid"
	CheckWatcherRunning          = "watcher_running"
	CheckAmendmentDryRunApproved = "amendment_dry_run_approved"
	CheckLintClean               = "lint_clean"
	CheckPlans49Green            = "plans_4_9_green"
	CheckCIConsecutiveGreen      = "ci_consecutive_green"
)

const (
	doctrineMaxScope     = "max-scope"
	doctrineNameDefault  = "default"
	doctrineCapaFirewall = "capa-firewall"
)

func AllCheckNames() []string {
	return []string{
		CheckResearchMCPUp, CheckVerifyDocs, CheckCaronteIndexCurrency,
		CheckSystemStateTOML, CheckCaronteEngineUp, CheckADRsValid,
		CheckWatcherRunning, CheckAmendmentDryRunApproved,
		CheckLintClean, CheckPlans49Green, CheckCIConsecutiveGreen,
	}
}

func AllDoctrineNames() []string {
	return []string{doctrineMaxScope, doctrineNameDefault, doctrineCapaFirewall}
}

var tierTable = map[string]map[string]Tier{
	CheckResearchMCPUp: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckVerifyDocs: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckCaronteIndexCurrency: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierSoft, doctrineCapaFirewall: TierHard,
	},
	CheckSystemStateTOML: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierSoft, doctrineCapaFirewall: TierHard,
	},
	CheckCaronteEngineUp: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckADRsValid: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckWatcherRunning: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierSoft, doctrineCapaFirewall: TierHard,
	},
	CheckAmendmentDryRunApproved: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierInformational, doctrineCapaFirewall: TierHard,
	},
	CheckLintClean: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckPlans49Green: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierHard, doctrineCapaFirewall: TierHard,
	},
	CheckCIConsecutiveGreen: {
		doctrineMaxScope: TierHard, doctrineNameDefault: TierSoft, doctrineCapaFirewall: TierHard,
	},
}

func tierStrictness(t Tier) int {
	switch t {
	case TierInformational:
		return 1
	case TierSoft:
		return 2
	case TierHard:
		return 3
	default:
		return 0
	}
}

func applyOverride(baseline, override Tier) Tier { return ApplyOverride(baseline, override) }

// ApplyOverride returns the stricter of (baseline, override). Override Tier(0)
// (the zero value, indicating "no override for this check") leaves baseline
// unchanged. ApplyOverride is the engine-side hook; callers MUST run
// ValidateOverrides at config-load time to reject loosening attempts before
// any engine call. The engine cannot tell the difference between a
// "deliberate loosen-attempt" and a "no override" without that prior
// validation, so misconfiguration must surface at TOML load, not build start.
func ApplyOverride(baseline, override Tier) Tier {
	if override == 0 {
		return baseline
	}
	if tierStrictness(override) > tierStrictness(baseline) {
		return override
	}
	return baseline
}

// ValidateOverrides verifies a per-project override map only TIGHTENS the
// baseline matrix for the given doctrine. Any attempt to loosen
// (hard→soft, hard→informational, soft→informational) is rejected with an
// explicit error citing the (baseline, attempted) pair. Unknown check or
// doctrine names are also rejected so misspelled config surfaces fail-closed.
//
// Callers (hadessystem.toml loader) MUST invoke this at load time and refuse to
// start the daemon on error — runtime "soft" tolerance of misconfig is not
// a recoverable posture for autonomy gating.
func ValidateOverrides(doctrine string, overrides map[string]Tier) error {
	for name, override := range overrides {
		base, err := TierForCheck(name, doctrine)
		if err != nil {
			return fmt.Errorf("autonomy override %q: %w", name, err)
		}
		if tierStrictness(override) == 0 {
			return fmt.Errorf(
				"autonomy override %q: invalid tier value %v (want hard|soft|informational)",
				name, override,
			)
		}
		if tierStrictness(override) < tierStrictness(base) {
			return fmt.Errorf(
				"autonomy override %q: cannot loosen from %s to %s (tighten-only)",
				name, base, override,
			)
		}
	}
	return nil
}

func TierForCheck(check, doctrine string) (Tier, error) {
	c := strings.TrimSpace(check)
	d := strings.ToLower(strings.TrimSpace(doctrine))
	row, ok := tierTable[c]
	if !ok {
		return 0, fmt.Errorf("autonomy: unknown check %q", check)
	}
	t, ok := row[d]
	if !ok {
		return 0, fmt.Errorf("autonomy: unknown doctrine %q for check %q", doctrine, check)
	}
	return t, nil
}
