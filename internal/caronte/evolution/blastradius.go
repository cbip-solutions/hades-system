// SPDX-License-Identifier: MIT
package evolution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/structure"
)

var ErrRiskWeightsInvalid = errors.New("evolution: risk weights invalid (must be ≥0 and sum to 1.0)")

var ErrRiskThresholdsInvalid = errors.New("evolution: risk thresholds invalid (need 0 ≤ MediumAt ≤ HighAt ≤ 1)")

type RiskWeights struct {
	Cone     float64
	Coreness float64
	Churn    float64
	Coupling float64
}

func DefaultRiskWeights() RiskWeights {
	return RiskWeights{Cone: 0.35, Coreness: 0.30, Churn: 0.20, Coupling: 0.15}
}

// Validate enforces: every weight ≥ 0 AND the four sum to 1.0 (within 1e-9).
// Returns ErrRiskWeightsInvalid (wrapped) on violation. The daemon's doctrine
// loader MUST call this (or use DefaultRiskWeights) so a hand-edited doctrine
// cannot produce a score outside [0,1].
func (w RiskWeights) Validate() error {
	for name, v := range map[string]float64{"Cone": w.Cone, "Coreness": w.Coreness, "Churn": w.Churn, "Coupling": w.Coupling} {
		if v < 0 {
			return fmt.Errorf("%w: %s=%v < 0", ErrRiskWeightsInvalid, name, v)
		}
	}
	sum := w.Cone + w.Coreness + w.Churn + w.Coupling
	if math.Abs(sum-1.0) > 1e-9 {
		return fmt.Errorf("%w: sum=%v != 1.0", ErrRiskWeightsInvalid, sum)
	}
	return nil
}

type RiskThresholds struct {
	MediumAt float64
	HighAt   float64
}

func DefaultRiskThresholds() RiskThresholds {
	return RiskThresholds{MediumAt: 0.30, HighAt: 0.60}
}

func (t RiskThresholds) Level(score float64) string {
	switch {
	case score >= t.HighAt:
		return "high"
	case score >= t.MediumAt:
		return "medium"
	default:
		return "low"
	}
}

func (t RiskThresholds) Validate() error {
	if t.MediumAt < 0 || t.HighAt > 1 || t.MediumAt > t.HighAt {
		return fmt.Errorf("%w: MediumAt=%v HighAt=%v", ErrRiskThresholdsInvalid, t.MediumAt, t.HighAt)
	}
	return nil
}

func percentileRank(value int, population []int) float64 {
	if len(population) == 0 {
		return 0
	}
	le := 0
	for _, p := range population {
		if p <= value {
			le++
		}
	}
	return float64(le) / float64(len(population))
}

// normLog1p returns log1p(x) / log1p(p95), clamped to [0,1]. The cone +
// coupling terms use this log-compression so a handful of huge fan-outs do not
// dwarf the rest (spec §9.1 `log1p(...)/log1p(P95_...)`). Guards: p95 ≤ 0
// (empty/degenerate graph) → 0 (no Inf/NaN); x > p95 → clamped to 1 (a change
// past the 95th percentile is maximally risky on the axis).
func normLog1p(x, p95 float64) float64 {
	if p95 <= 0 {
		return 0
	}
	return clamp01(math.Log1p(x) / math.Log1p(p95))
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func coneEdgeKinds() []store.EdgeKind { return []store.EdgeKind{store.EdgeCalls, store.EdgeInvoke} }

func reverseCone(ctx context.Context, s *store.Store, seeds []string) (int, []string, error) {
	visited := map[string]bool{}
	for _, seed := range seeds {
		visited[seed] = true
	}

	inCount := map[string]int{}
	frontier := append([]string{}, seeds...)
	sort.Strings(frontier)
	for len(frontier) > 0 {
		var next []string
		for _, target := range frontier {
			for _, kind := range coneEdgeKinds() {
				edges, err := s.ListEdgesByTarget(ctx, target, kind)
				if err != nil {
					return 0, nil, fmt.Errorf("evolution: reverseCone ListEdgesByTarget(%s,%s): %w", target, kind, err)
				}
				for _, e := range edges {
					caller := e.SourceID
					inCount[caller]++
					if visited[caller] {
						continue
					}
					visited[caller] = true
					next = append(next, caller)
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}

	seedSet := map[string]bool{}
	for _, seed := range seeds {
		seedSet[seed] = true
	}
	top := make([]string, 0, len(visited))
	for n := range visited {
		if !seedSet[n] {
			top = append(top, n)
		}
	}

	sort.Slice(top, func(i, j int) bool {
		if inCount[top[i]] != inCount[top[j]] {
			return inCount[top[i]] > inCount[top[j]]
		}
		return top[i] < top[j]
	})
	return len(top), top, nil
}

func enumerateAllNodes(ctx context.Context, s *store.Store) ([]store.Node, error) {
	var all []store.Node
	for _, k := range store.AllNodeKinds() {
		ns, err := s.ListNodesByKind(ctx, k)
		if err != nil {
			return nil, fmt.Errorf("evolution: enumerateAllNodes(%s): %w", k, err)
		}
		all = append(all, ns...)
	}
	return all, nil
}

func maxCoreness(ctx context.Context, s *store.Store, d structure.Decomposition) (int, error) {
	nodes, err := enumerateAllNodes(ctx, s)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, n := range nodes {
		if n.Coreness > max {
			max = n.Coreness
		}
		if d.Coreness != nil {
			if c := d.CorenessOf(n.NodeID); c > max {
				max = c
			}
		}
	}
	return max, nil
}

func maxChurn(ctx context.Context, b *Builder, files []string, windowDays int) (int, error) {
	max := 0
	for _, f := range files {
		ch, err := b.store.GetChurn(ctx, f, windowDays)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return 0, fmt.Errorf("evolution: maxChurn GetChurn(%s): %w", f, err)
		}
		if ch.TouchCount > max {
			max = ch.TouchCount
		}
	}
	return max, nil
}

func couplingFanout(ctx context.Context, b *Builder, files []string, windowDays int) (int, error) {
	minPct := b.paramsFor("").MinCouplingPercent
	peers := map[string]bool{}
	for _, f := range files {
		rows, err := b.ListCoupling(ctx, "", f, windowDays)
		if err != nil {
			return 0, fmt.Errorf("evolution: couplingFanout ListCoupling(%s): %w", f, err)
		}
		for _, c := range rows {
			if c.CouplingPercent < minPct {
				continue
			}
			peer := c.FileB
			if peer == f {
				peer = c.FileA
			}
			peers[peer] = true
		}
	}
	return len(peers), nil
}

func p95Cone(ctx context.Context, s *store.Store) (float64, error) {
	nodes, err := enumerateAllNodes(ctx, s)
	if err != nil {
		return 0, err
	}
	if len(nodes) == 0 {
		return 0, nil
	}
	sizes := make([]int, 0, len(nodes))
	for _, n := range nodes {
		sz, _, err := reverseCone(ctx, s, []string{n.NodeID})
		if err != nil {
			return 0, err
		}
		sizes = append(sizes, sz)
	}
	return p95(sizes), nil
}

func p95Fanout(ctx context.Context, s *store.Store, b *Builder, windowDays int) (float64, error) {
	nodes, err := enumerateAllNodes(ctx, s)
	if err != nil {
		return 0, err
	}
	fileSet := map[string]bool{}
	for _, n := range nodes {
		if n.FilePath != "" {
			fileSet[n.FilePath] = true
		}
	}
	if len(fileSet) == 0 {
		return 0, nil
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	fanouts := make([]int, 0, len(files))
	for _, f := range files {
		fo, err := couplingFanout(ctx, b, []string{f}, windowDays)
		if err != nil {
			return 0, err
		}
		fanouts = append(fanouts, fo)
	}
	return p95(fanouts), nil
}

func p95(sample []int) float64 {
	if len(sample) == 0 {
		return 0
	}
	cp := append([]int{}, sample...)
	sort.Ints(cp)

	idx := int(math.Ceil(0.95*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return float64(cp[idx])
}

type RiskScore struct {
	Score       float64
	Level       string
	Cone        float64
	Coreness    float64
	Churn       float64
	Coupling    float64
	TopAffected []string
}

const maxTopAffected = 20

func BlastRadius(
	ctx context.Context,
	s *store.Store,
	b *Builder,
	d structure.Decomposition,
	weights RiskWeights,
	thresholds RiskThresholds,
	projectID string,
	changedSymbols, changedFiles []string,
) (RiskScore, error) {
	if err := weights.Validate(); err != nil {
		return RiskScore{}, fmt.Errorf("evolution: BlastRadius: %w", err)
	}
	if err := thresholds.Validate(); err != nil {
		return RiskScore{}, fmt.Errorf("evolution: BlastRadius: %w", err)
	}
	// Empty change → no risk (do not fabricate from nothing).
	if len(changedSymbols) == 0 && len(changedFiles) == 0 {
		return RiskScore{Level: thresholds.Level(0)}, nil
	}

	window := b.paramsFor(projectID).WindowDays

	coneSize, top, err := reverseCone(ctx, s, changedSymbols)
	if err != nil {
		return RiskScore{}, err
	}
	p95c, err := p95Cone(ctx, s)
	if err != nil {
		return RiskScore{}, err
	}
	coneTerm := normLog1p(float64(coneSize), p95c)

	seedCoreness := 0
	for _, sym := range changedSymbols {
		c := 0
		if d.Coreness != nil {
			c = d.CorenessOf(sym)
		}
		if c == 0 {
			n, gerr := s.GetNode(ctx, sym)
			if gerr != nil {
				if !errors.Is(gerr, store.ErrNotFound) {
					return RiskScore{}, fmt.Errorf("evolution: BlastRadius GetNode(%s): %w", sym, gerr)
				}
			} else if n.Coreness > c {
				c = n.Coreness
			}
		}
		if c > seedCoreness {
			seedCoreness = c
		}
	}
	maxC, err := maxCoreness(ctx, s, d)
	if err != nil {
		return RiskScore{}, err
	}
	corenessTerm := 0.0
	if maxC > 0 {
		corenessTerm = clamp01(float64(seedCoreness) / float64(maxC))
	}

	mc, err := maxChurn(ctx, b, changedFiles, window)
	if err != nil {
		return RiskScore{}, err
	}
	churnTerm := 0.0
	if mc > 0 {
		churnPop, popErr := churnPopulation(ctx, s, b, window)
		if popErr != nil {
			return RiskScore{}, popErr
		}
		churnTerm = clamp01(percentileRank(mc, churnPop))
	}

	fanout, err := couplingFanout(ctx, b, changedFiles, window)
	if err != nil {
		return RiskScore{}, err
	}
	p95f, err := p95Fanout(ctx, s, b, window)
	if err != nil {
		return RiskScore{}, err
	}
	couplingTerm := normLog1p(float64(fanout), p95f)

	score := clamp01(
		weights.Cone*coneTerm +
			weights.Coreness*corenessTerm +
			weights.Churn*churnTerm +
			weights.Coupling*couplingTerm,
	)
	if len(top) > maxTopAffected {
		top = top[:maxTopAffected]
	}
	return RiskScore{
		Score:       score,
		Level:       thresholds.Level(score),
		Cone:        coneTerm,
		Coreness:    corenessTerm,
		Churn:       churnTerm,
		Coupling:    couplingTerm,
		TopAffected: top,
	}, nil
}

func churnPopulation(ctx context.Context, s *store.Store, b *Builder, windowDays int) ([]int, error) {
	nodes, err := enumerateAllNodes(ctx, s)
	if err != nil {
		return nil, err
	}
	fileSet := map[string]bool{}
	for _, n := range nodes {
		if n.FilePath != "" {
			fileSet[n.FilePath] = true
		}
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	pop := make([]int, 0, len(files))
	for _, f := range files {
		ch, err := b.store.GetChurn(ctx, f, windowDays)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				pop = append(pop, 0)
				continue
			}
			return nil, fmt.Errorf("evolution: churnPopulation GetChurn(%s): %w", f, err)
		}
		pop = append(pop, ch.TouchCount)
	}
	return pop, nil
}
