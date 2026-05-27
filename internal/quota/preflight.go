// SPDX-License-Identifier: MIT
package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

// PreFlightDecision is the result of a 3-layer pre-flight check.
//
// review CRITICAL #7 reconciliation (2026-05-01): this struct is
// the canonical decision shape consumed by scheduler via the
// `scheduler.QuotaPreFlightChecker` interface. The scheduler MUST NOT
// declare a parallel `QuotaPreFlightDecision` type.
type PreFlightDecision struct {
	Allowed bool

	SoftWarn bool

	Reason string

	NextRetryAt time.Time
}

type PreFlightDeps struct {
	Thresholds Thresholds

	Used int64

	Cap int64

	GlobalCap int64

	GlobalUsed int64

	PerTierCaps map[string]int64

	PerTierUsed map[string]int64

	RequestTier string

	Wfq *WfqQueue

	CongestionThreshold int

	Override *Override

	Now func() time.Time
}

func PreFlight(ctx context.Context, projectAlias string, d doctrine.Name, deps PreFlightDeps) (PreFlightDecision, error) {
	_ = ctx

	if projectAlias == "" {
		return PreFlightDecision{}, fmt.Errorf("quota.PreFlight: projectAlias is empty")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	doctrineLabel := normaliseDoctrine(d)

	dec := PreFlightDecision{Allowed: true}

	overrideActive := deps.Override != nil && deps.Override.IsActive(now())

	projStatus := ClassifyUsage(deps.Used, deps.Cap, deps.Thresholds)
	switch projStatus {
	case CapStatusOK:

	case CapStatusSoftWarn:
		dec.SoftWarn = true
		appendReason(&dec, "layer1", doctrineLabel, "soft-warn",
			fmt.Sprintf("project_cap usage=%d%% cap=%d", pctOf(deps.Used, deps.Cap), deps.Cap))
	case CapStatusHardLogOnly:
		dec.SoftWarn = true
		appendReason(&dec, "layer1", doctrineLabel, "hard-log-only",
			fmt.Sprintf("project_cap usage=%d%% cap=%d", pctOf(deps.Used, deps.Cap), deps.Cap))
	case CapStatusHardDeny:
		if overrideActive {
			appendReason(&dec, "layer3", doctrineLabel, "boost-applied",
				fmt.Sprintf("multiplier=%v expires=%s",
					deps.Override.Multiplier, deps.Override.ExpiresAt.UTC().Format(time.RFC3339)))
		} else {
			dec.Allowed = false
			appendReason(&dec, "layer1", doctrineLabel, "hard-deny",
				fmt.Sprintf("project_cap usage=%d%% cap=%d", pctOf(deps.Used, deps.Cap), deps.Cap))
			return dec, nil
		}
	}

	globalThresholds := DoctrineDefaults(doctrine.NameDefault)
	globalStatus := ClassifyUsage(deps.GlobalUsed, deps.GlobalCap, globalThresholds)
	switch globalStatus {
	case CapStatusOK:

	case CapStatusSoftWarn:
		dec.SoftWarn = true
		appendReason(&dec, "layer1", "default", "soft-warn",
			fmt.Sprintf("daemon_cap usage=%d%% cap=%d", pctOf(deps.GlobalUsed, deps.GlobalCap), deps.GlobalCap))
	case CapStatusHardDeny:
		dec.Allowed = false

		dec.Reason = fmt.Sprintf("layer1:default:hard-deny:daemon_cap usage=%d%% cap=%d",
			pctOf(deps.GlobalUsed, deps.GlobalCap), deps.GlobalCap)
		return dec, nil

	}

	if deps.RequestTier != "" && deps.PerTierCaps != nil {
		if tierCap, ok := deps.PerTierCaps[deps.RequestTier]; ok && tierCap > 0 {
			used := deps.PerTierUsed[deps.RequestTier]
			tierStatus := ClassifyUsage(used, tierCap, globalThresholds)
			switch tierStatus {
			case CapStatusHardDeny:
				dec.Allowed = false
				dec.Reason = fmt.Sprintf("layer1:default:hard-deny:per_tier %s usage=%d%% cap=%d",
					deps.RequestTier, pctOf(used, tierCap), tierCap)
				return dec, nil
			case CapStatusSoftWarn:
				dec.SoftWarn = true
				appendReason(&dec, "layer1", "default", "soft-warn",
					fmt.Sprintf("per_tier %s usage=%d%%", deps.RequestTier, pctOf(used, tierCap)))
			case CapStatusOK, CapStatusHardLogOnly:

			}
		}
	}

	congestThreshold := deps.CongestionThreshold
	if congestThreshold <= 0 {
		congestThreshold = DefaultStarveDepthThreshold
	}
	if deps.Wfq != nil {
		depth := deps.Wfq.Depth(projectAlias)
		if depth >= congestThreshold {
			if overrideActive {
				appendReason(&dec, "layer3", doctrineLabel, "boost-applied",
					fmt.Sprintf("wfq_congestion_bypass depth=%d", depth))
			} else {
				dec.Allowed = false
				retry := now().Add(30 * time.Second)
				dec.NextRetryAt = retry
				dec.Reason = fmt.Sprintf("layer2:%s:wfq-congested:queue_depth=%d retry_at=%s",
					doctrineLabel, depth, retry.UTC().Format(time.RFC3339))
				return dec, nil
			}
		}
	}

	return dec, nil
}

func pctOf(used, cap int64) int64 {
	if cap <= 0 {
		return 0
	}
	if used < 0 {
		return 0
	}
	return used * 100 / cap
}

func normaliseDoctrine(d doctrine.Name) string {
	switch d {
	case doctrine.NameMaxScope:
		return string(doctrine.NameMaxScope)
	case doctrine.NameCapaFirewall:
		return string(doctrine.NameCapaFirewall)
	case doctrine.NameDefault:
		return string(doctrine.NameDefault)
	default:
		return string(doctrine.NameDefault)
	}
}

func appendReason(dec *PreFlightDecision, layer, doctrineLabel, status, detail string) {
	s := fmt.Sprintf("%s:%s:%s:%s", layer, doctrineLabel, status, detail)
	if dec.Reason == "" {
		dec.Reason = s
		return
	}
	dec.Reason = dec.Reason + " | " + s
}

func IsCongested(wfq *WfqQueue, alias string, threshold int) bool {
	if wfq == nil {
		return false
	}
	if threshold <= 0 {
		threshold = DefaultStarveDepthThreshold
	}
	return wfq.Depth(alias) >= threshold
}

type PreFlightAdapter struct {
	DepsBuilder func(ctx context.Context, projectAlias string, d doctrine.Name) (PreFlightDeps, error)

	Deps PreFlightDeps
}

func (a *PreFlightAdapter) PreFlight(ctx context.Context, alias string, d doctrine.Name) (PreFlightDecision, error) {
	deps := a.Deps
	if a.DepsBuilder != nil {
		built, err := a.DepsBuilder(ctx, alias, d)
		if err != nil {
			return PreFlightDecision{}, fmt.Errorf("quota.PreFlightAdapter.PreFlight: build deps: %w", err)
		}
		deps = built
	}
	return PreFlight(ctx, alias, d, deps)
}
