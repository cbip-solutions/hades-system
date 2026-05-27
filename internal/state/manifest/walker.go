// SPDX-License-Identifier: MIT
package manifest

import (
	"context"
	"sort"
	"time"

	"github.com/cbip-solutions/hades-system/internal/state/manifest/walkers"
)

type WalkerConfig struct {
	GitRepoRoot string

	ADRIndexPath string

	GoModPath string

	InvariantGrepRoot string

	AutonomyStampPath string

	HadesSystemVersion string

	DoctrineRegistryFn func() []string
}

type WalkResult struct {
	Manifest Manifest

	MissingSources []string
}

type Walker struct {
	cfg WalkerConfig
}

func NewWalker(cfg WalkerConfig) *Walker { return &Walker{cfg: cfg} }

func (w *Walker) Walk(ctx context.Context) (WalkResult, error) {
	var missing []string
	addMissing := func(srcs ...string) { missing = append(missing, srcs...) }

	gw := walkers.NewGitWalker(w.cfg.GitRepoRoot)
	gres, _ := gw.Walk(ctx)
	addMissing(gres.MissingSources...)

	aw := walkers.NewADRWalker(w.cfg.ADRIndexPath)
	ares, _ := aw.Walk(ctx)
	addMissing(ares.MissingSources...)

	dw := walkers.NewDoctrineWalker(w.cfg.DoctrineRegistryFn)
	dres, _ := dw.Walk(ctx)
	addMissing(dres.MissingSources...)

	gomod := walkers.NewGoModWalker(w.cfg.GoModPath, w.cfg.HadesSystemVersion)
	gores, _ := gomod.Walk(ctx)
	addMissing(gores.MissingSources...)

	iw := walkers.NewInvariantsWalker(w.cfg.InvariantGrepRoot)
	ires, _ := iw.Walk(ctx)
	addMissing(ires.MissingSources...)

	authw := walkers.NewAutonomyWalker(w.cfg.AutonomyStampPath)
	authres, _ := authw.Walk(ctx)
	addMissing(authres.MissingSources...)

	mcps := MCPsSection{Entries: map[string]MCPEntry{}}

	manifest := Manifest{
		HadesSystem: HadesSystemSection{
			Version:   gores.Version,
			Substrate: "openclaude",
		},
		Plans: PlansSection{
			Released:          gres.Released,
			InProgress:        gres.InProgress,
			BrainstormPending: gres.BrainstormPending,
		},
		Invariants: InvariantsSection{
			Count:     ires.Count,
			VerifyCmd: "make verify-invariants",
		},
		Doctrines: DoctrinesSection{
			Declared: dres.Declared,
		},
		MCPs: mcps,
		ADR: ADRSection{
			Count:    ares.Count,
			Location: ares.Location,
		},
		AutonomousMode: AutonomousModeSection{

			PrerequisitesMet: authres.PrerequisitesMet,
			LastCheck:        authres.LastCheck,
		},
		Provenance: Provenance{
			LastRegenerate: time.Now().UTC(),
			MissingSources: missing,
		},
	}

	sort.Strings(missing)
	return WalkResult{Manifest: manifest, MissingSources: missing}, nil
}
