// SPDX-License-Identifier: MIT
// Package quota implements hades-system's 3-layer quota system
// design spec §1 Q4 + §1 Q10:
//
// Layer 1 — Hierarchical budgets (per-project + global daemon + per-tier)
// Layer 2 — Weighted Fair Queueing (token bucket per project)
// Layer 3 — Operator override (boost weight ×N for TTL window)
//
// All thresholds are doctrine-tunable: per-doctrine defaults live in
// DoctrineDefaults; per-project overrides come via hadessystem.toml at
// activation time and flow into ResolveThresholds ( subsequent
// tasks).
//
// # Boundary contract
//
// Per invariant generalised (spec §2.8): this package imports stdlib
// + internal/doctrine only. Storage access (the priority_overrides
// table that backs Layer 3) flows via the OverrideStore interface; the
// concrete implementation lives in internal/daemon/quotaadapter/ which
// is the only package permitted to import internal/store.
//
// # LLM dispatch contract
//
// Per invariant: PreFlight returns a decision only — never invokes a
// provider. The dispatcher consumes the decision and proceeds
// or denies; this preserves the single-egress-point invariant.
//
// # Doctrine taxonomy
//
// The canonical doctrine taxonomy is owned by internal/doctrine.Name
// (constants doctrine.NameMaxScope, doctrine.NameDefault,
// doctrine.NameCapaFirewall). consumes those names directly
// to avoid a parallel taxonomy; round-trip across the package boundary
// is byte-lossless (see TestDoctrineConstantsMatchInternalDoctrineNames).
package quota

import (
	"errors"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type Mode int

const (
	// ModeWarnOnly logs a warning but does NOT deny. Used by max-scope.
	ModeWarnOnly Mode = iota

	ModeSoftHard

	ModeExtraMargin
)

// String returns a stable human label for logs. Downstream observability
// (audit events, status responses) depend on these exact strings; do not
// rename without coordinating an invariant update.
func (m Mode) String() string {
	switch m {
	case ModeWarnOnly:
		return "warn-only"
	case ModeSoftHard:
		return "soft-hard"
	case ModeExtraMargin:
		return "extra-margin"
	default:
		return "unknown"
	}
}

// Thresholds carries the per-doctrine soft + hard cap percentages plus
// the Mode that determines what to do at HardCapPct.
//
// Invariants (enforced by the matrix; ResolveThresholds will re-verify
// at config-load time in subsequent tasks):
//
// - 0 < SoftCapPct <= HardCapPct <= 100
// - HardCapPct == 100 ⇔ Mode != ModeExtraMargin
// - Pulido §3.5 keeps SoftCapPct = 80 across all doctrines so the
// warning surface is uniform; only HardCapPct + Mode vary.
type Thresholds struct {
	// SoftCapPct is the percentage of the cap at which a warning fires
	// (typically 80). Below SoftCapPct: silent. Between SoftCapPct and
	// HardCapPct CapStatusSoftWarn (declared in subsequent B-2 task).
	SoftCapPct int

	HardCapPct int

	Mode Mode
}

func DoctrineDefaults(d doctrine.Name) Thresholds {
	switch d {
	case doctrine.NameMaxScope:
		return Thresholds{SoftCapPct: 80, HardCapPct: 100, Mode: ModeWarnOnly}
	case doctrine.NameCapaFirewall:
		return Thresholds{SoftCapPct: 80, HardCapPct: 95, Mode: ModeExtraMargin}
	case doctrine.NameDefault:
		return Thresholds{SoftCapPct: 80, HardCapPct: 100, Mode: ModeSoftHard}
	default:

		return Thresholds{SoftCapPct: 80, HardCapPct: 100, Mode: ModeSoftHard}
	}
}

var ErrDoctrineMatrixAnchor = errors.New("quota: doctrine matrix anchor (inv-hades-115)")

type ProjectQuotaOverride struct {
	SoftCapPct int

	HardCapPct int
}

// validProjectQuotaOverride returns true when the override values are
// sensible (1..100 inclusive for both, soft ≤ hard).
//
// The threshold range is inclusive on both ends:
// - 1 means "warn at the very first dollar" (legal but unusual)
// - 100 means "warn at the cap exactly" (effectively no soft warning;
// legal because some operators want hard-deny only).
// - soft == hard is legal: warn and deny coincide at the same point.
func validProjectQuotaOverride(o ProjectQuotaOverride) bool {
	if o.SoftCapPct < 1 || o.SoftCapPct > 100 {
		return false
	}
	if o.HardCapPct < 1 || o.HardCapPct > 100 {
		return false
	}
	if o.SoftCapPct > o.HardCapPct {
		return false
	}
	return true
}

func ResolveThresholds(projectAlias string, d doctrine.Name, override *ProjectQuotaOverride) Thresholds {
	base := DoctrineDefaults(d)
	if override == nil {
		return base
	}
	if !validProjectQuotaOverride(*override) {
		return base
	}
	return Thresholds{
		SoftCapPct: override.SoftCapPct,
		HardCapPct: override.HardCapPct,
		Mode:       base.Mode,
	}
}

// CapStatus is the 4-state result of ClassifyUsage. The four states
// are load-bearing per spec §1 Q4: max-scope MUST never silently deny
// (operator self-throttles), so HardLogOnly is distinct from
// HardDeny. Dispatcher reads CapStatus + Mode together to decide
// runtime action.
type CapStatus int

const (
	CapStatusOK CapStatus = iota
	// CapStatusSoftWarn — used ≥ SoftCapPct AND used < HardCapPct.
	// Caller logs a warning + emits info-immediate notification but
	// proceeds with the dispatch.
	CapStatusSoftWarn

	CapStatusHardDeny

	CapStatusHardLogOnly
)

// String returns a stable label for logs / audit trail. Downstream
// observability (audit events, status responses) depends on these exact
// strings; do not rename without coordinating an invariant update.
func (s CapStatus) String() string {
	switch s {
	case CapStatusOK:
		return "ok"
	case CapStatusSoftWarn:
		return "soft-warn"
	case CapStatusHardDeny:
		return "hard-deny"
	case CapStatusHardLogOnly:
		return "hard-log-only"
	default:
		return "unknown"
	}
}

// ClassifyUsage compares used vs cap against Thresholds and returns
// the CapStatus.
//
// # Algorithm
//
// if cap <= 0: return CapStatusOK // no cap configured
// if used < 0: used = 0 // defensive
// pct = used * 100 / cap // integer arithmetic
// if pct < SoftCapPct: return CapStatusOK
// if pct < HardCapPct: return CapStatusSoftWarn
// if Mode == ModeWarnOnly: return CapStatusHardLogOnly
// else: return CapStatusHardDeny
//
// Integer arithmetic is intentional — avoids floating-point compare
// edge cases at the threshold boundary. used is int64 so multiplying
// by 100 stays in range for any realistic usage (max int64 ~9.2e18,
// /100 leaves headroom up to 9.2e16 cents = $9.2e14, far above any
// real cap).
//
// Boundary semantics: the comparisons use strict less-than against
// thresholds (`pct < SoftCapPct` is OK, `pct >= SoftCapPct` is
// SoftWarn). This means a cap with SoftCapPct=80 fires SoftWarn at
// exactly 80% — matches operator intuition ("at 80% I get a warning").
//
// The 4-state distinction is load-bearing per spec §1 Q4: max-scope
// MUST never silently deny (operator is the gating authority), so even
// at 100%+ the status is HardLogOnly, not HardDeny. Default and
// capa-firewall both deny, but capa-firewall denies earlier (95% vs
// 100%) — the threshold value differentiates their safety margins, the
// Mode tag carries the dispatcher semantic.
//
// cap is named `capUSDCents` deliberately to avoid shadowing Go's
// built-in `cap()` (slice capacity); callers reading the body should
// note the parameter rename does not change the public signature
// described above.
func ClassifyUsage(used, capUSDCents int64, t Thresholds) CapStatus {
	if capUSDCents <= 0 {
		return CapStatusOK
	}
	if used < 0 {
		used = 0
	}
	pct := used * 100 / capUSDCents
	soft := int64(t.SoftCapPct)
	hard := int64(t.HardCapPct)
	if pct < soft {
		return CapStatusOK
	}
	if pct < hard {
		return CapStatusSoftWarn
	}
	if t.Mode == ModeWarnOnly {
		return CapStatusHardLogOnly
	}
	return CapStatusHardDeny
}
