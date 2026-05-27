// SPDX-License-Identifier: MIT
// Package cli — doctor_augment.go
//
// §7.4 Augmentation block. Each check probes the daemon's
// /v1/augment/probe?check=<name> route shipped by
//
// Probe ordering (matches §7.4 spec exactly):
// 1. augment.endpoint-reachable
// 2. augment.budget.headroom
// 3. augment.cache.hit-rate
// 4. augment.latency.p50-p99
// 5. augment.5-lane-rrf.healthy
// 6. augment.privacy-filter.tested
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const augmentProbeTimeout = 5 * time.Second

type AugmentProber interface {
	AugmentProbe(ctx context.Context, check string) (*client.AugmentProbeResp, error)
}

func NewDoctorAugmentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "augment",
		Short: "Augmentation pipeline checks (Plan 11; 6 checks per spec §7.4)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Augmentation (Plan 11)", runAugmentChecks)
		},
	}
}

func runAugmentChecks(ctx context.Context, c *client.Client) []CheckResult {
	return runAugmentChecksWith(ctx, c)
}

func runAugmentChecksWith(ctx context.Context, p AugmentProber) []CheckResult {
	checks := []struct {
		probeName  string
		resultName string
		hint       string
	}{
		{
			probeName:  "endpoint-reachable",
			resultName: "augment.endpoint-reachable",
			hint:       "daemon /v1/augment unreachable; check zen daemon status",
		},
		{
			probeName:  "budget-headroom",
			resultName: "augment.budget.headroom",
			hint:       "tighten max_kg_tokens in doctrine config or reduce augmentation frequency; see doctrine.augmentation",
		},
		{
			probeName:  "cache-hit-rate",
			resultName: "augment.cache.hit-rate",
			hint:       "low prompt cache hit rate; ensure stable system-prompt prefix per Anthropic caching spec",
		},
		{
			probeName:  "latency-p50-p99",
			resultName: "augment.latency.p50-p99",
			hint:       "augmentation latency exceeds doctrine ceiling; reduce KG lanes or increase timeout_ms",
		},
		{
			probeName:  "five-lane-rrf-healthy",
			resultName: "augment.5-lane-rrf.healthy",
			hint:       "one or more augmentation lanes failing; check caronte engine and aggregator.db",
		},
		{
			probeName:  "privacy-filter-tested",
			resultName: "augment.privacy-filter.tested",
			hint:       "run: go test -tags=adversarial ./tests/adversarial/ -run TestPrivacyFilter",
		},
	}
	out := make([]CheckResult, 0, 6)
	for _, ch := range checks {
		cctx, cancel := context.WithTimeout(ctx, augmentProbeTimeout)
		r, err := p.AugmentProbe(cctx, ch.probeName)
		cancel()
		out = append(out, augmentResultFrom(ch.resultName, r, err, ch.hint))
	}
	return out
}

func augmentResultFrom(name string, r *client.AugmentProbeResp, err error, hint string) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}
