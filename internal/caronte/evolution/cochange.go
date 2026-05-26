// SPDX-License-Identifier: MIT
package evolution

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var ErrInsufficientHistory = errors.New("evolution: insufficient git history for co-change/churn (cold-start gate)")

type Builder struct {
	store  *store.Store
	runner GitRunner
	params ParamsAccessor
}

func NewBuilder(s *store.Store, runner GitRunner, params ParamsAccessor) *Builder {
	return &Builder{store: s, runner: runner, params: params}
}

func (b *Builder) paramsFor(projectID string) Params {
	if b.params == nil {
		return DefaultParams()
	}
	p := b.params.CoChangeParams(projectID)
	if err := p.Validate(); err != nil {
		return DefaultParams()
	}
	return p
}

func CouplingDegree(c store.CoChange) float64 {
	denom := float64(c.RevsA+c.RevsB) / 2.0
	if denom == 0 {
		return 0
	}
	return float64(c.SharedRevs) / denom * 100.0
}

type Coupling struct {
	FileA, FileB             string
	SharedRevs, RevsA, RevsB int
	WindowDays               int
	CouplingPercent          float64
}

func (b *Builder) GetCoupling(ctx context.Context, projectID, fileA, fileB string, windowDays int) (Coupling, error) {
	row, err := b.store.GetCoChange(ctx, fileA, fileB, windowDays)
	if err != nil {

		return Coupling{}, err
	}
	return Coupling{
		FileA: row.FileA, FileB: row.FileB,
		SharedRevs: row.SharedRevs, RevsA: row.RevsA, RevsB: row.RevsB,
		WindowDays:      row.WindowDays,
		CouplingPercent: CouplingDegree(row),
	}, nil
}

func (b *Builder) IsCoupled(projectID string, c Coupling) bool {
	return c.CouplingPercent >= b.paramsFor(projectID).MinCouplingPercent
}

type pairKey struct{ a, b string }

func orderedPair(x, y string) pairKey {
	if x <= y {
		return pairKey{x, y}
	}
	return pairKey{y, x}
}

func sinceArg(windowDays int) string {
	if windowDays <= 0 {
		return ""
	}
	cutoff := timeNow().AddDate(0, 0, -windowDays)
	return cutoff.UTC().Format(rfc3339)
}

func (b *Builder) BuildCoChange(ctx context.Context, projectID, repoDir string) error {
	p := b.paramsFor(projectID)

	total, err := b.runner.RevListCount(ctx, repoDir)
	if err != nil {
		return err
	}
	if total < p.MinTotalCommits {
		return fmt.Errorf("%w: %d commits < min_total_commits %d", ErrInsufficientHistory, total, p.MinTotalCommits)
	}

	commits, err := b.logCommits(ctx, repoDir, p)
	if err != nil {
		return err
	}

	fileRevs := map[string]int{}
	pairShared := map[pairKey]int{}
	for _, c := range commits {

		if len(c.Files) == 0 || len(c.Files) > p.MaxChangesetSize {
			continue
		}
		for _, f := range c.Files {
			fileRevs[f]++
		}

		for i := 0; i < len(c.Files); i++ {
			for j := i + 1; j < len(c.Files); j++ {
				pairShared[orderedPair(c.Files[i], c.Files[j])]++
			}
		}
	}

	now := timeNow().Unix()
	for key, shared := range pairShared {
		if shared < p.MinSharedRevisions {
			continue
		}
		if err := b.store.UpsertCoChange(ctx, store.CoChange{
			FileA: key.a, FileB: key.b,
			SharedRevs: shared,
			RevsA:      fileRevs[key.a],
			RevsB:      fileRevs[key.b],
			WindowDays: p.WindowDays,
			UpdatedAt:  now,
		}); err != nil {
			return fmt.Errorf("evolution: persist co-change (%s,%s): %w", key.a, key.b, err)
		}
	}
	return nil
}

func (b *Builder) logCommits(ctx context.Context, repoDir string, p Params) ([]Commit, error) {
	args := []string{
		"--no-merges",
		"--name-only",
		"--pretty=format:" + recSep + "%H" + unitSep + "%ae" + unitSep + "%ct" + unitSep,
	}
	if p.FollowRenames {
		args = append(args, "-M")
	}
	if s := sinceArg(p.WindowDays); s != "" {
		args = append(args, "--since="+s)
	}
	out, err := b.runner.Log(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out)
}

func (b *Builder) ListCoupling(ctx context.Context, projectID, file string, windowDays int) ([]Coupling, error) {
	rows, err := b.store.ListCoChangeForFile(ctx, file, windowDays)
	if err != nil {
		return nil, fmt.Errorf("evolution: ListCoupling: %w", err)
	}
	out := make([]Coupling, 0, len(rows))
	for _, c := range rows {
		out = append(out, Coupling{
			FileA: c.FileA, FileB: c.FileB,
			SharedRevs: c.SharedRevs, RevsA: c.RevsA, RevsB: c.RevsB,
			WindowDays:      c.WindowDays,
			CouplingPercent: CouplingDegree(c),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].CouplingPercent > out[j].CouplingPercent })
	_ = projectID
	return out, nil
}
