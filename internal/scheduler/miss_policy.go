// SPDX-License-Identifier: MIT
package scheduler

import (
	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func DoctrineMissPolicy(d doctrine.Name) MissPolicy {
	switch d {
	case doctrine.NameDefault:
		return MissPolicySkip
	case doctrine.NameMaxScope:
		return MissPolicyCatchUpBounded
	case doctrine.NameCapaFirewall:
		return MissPolicyNotifyOnly
	default:
		return MissPolicySkip
	}
}

// EffectiveMissPolicy resolves the policy actually applied to a fire.
// A non-zero per-Schedule override wins over the doctrine default;
// otherwise the doctrine default is used.
//
// Subtlety (load-bearing): MissPolicySkip is BOTH the zero value of
// MissPolicy AND the default doctrine's default. We cannot distinguish
// "operator set Skip explicitly" from "operator didn't set anything"
// without a separate nullable column on the schedules table — adding
// one to disambiguate would cost a migration for a UX win that
// matters only to a small minority of schedules.
//
// Convention when the doctrine is NOT default and the Schedule's
// MissPolicy is the zero value (Skip), we treat the zero as "unset"
// and fall through to the doctrine default. When the doctrine IS
// default, the zero is honoured directly because Skip is what the
// operator would get by either path.
//
// Implications for callers:
//
//   - To downgrade a max-scope routine from CatchUpBounded to Skip,
//     the operator must set a different non-zero value (e.g.
//     NotifyOnly) — explicit Skip on max-scope is interpreted as
//     "use doctrine default".
//   - aggregate routines on max-scope set MissPolicyCoalesce
//     explicitly; this is non-zero so resolution is unambiguous.
//
// nil-safe: a nil receiver returns DoctrineMissPolicy(d) directly so
// adapter paths that may construct a Schedule from a partial
// daemon.db row do not panic.
//
// Inv-zen-121 contract.
func EffectiveMissPolicy(s *Schedule, d doctrine.Name) MissPolicy {
	if s == nil {
		return DoctrineMissPolicy(d)
	}
	if d != doctrine.NameDefault && s.MissPolicy == MissPolicySkip {
		return DoctrineMissPolicy(d)
	}
	return s.MissPolicy
}
