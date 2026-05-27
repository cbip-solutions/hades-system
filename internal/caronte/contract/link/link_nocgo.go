//go:build !cgo

// SPDX-License-Identifier: MIT

package link

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type Linker struct{}

type Result struct {
	ProjectID, Repo                                           string
	CallsScanned, LinksPersisted, UnresolvedRows, SilentDrops int
	TierCounts                                                map[Confidence]int
	Errors                                                    []error
}

type WorkspaceLinkPort interface{ unused() }

type UnresolvedStorePort interface{ unused() }

type ProjectStorePort interface{ unused() }

type FederationReadPort interface{ unused() }

type LinkerDeps interface{ unused() }

func NewLinker(
	_ WorkspaceLinkPort,
	_ UnresolvedStorePort,
	_ federation.AuditEmitter,
	_ map[string]*yaml.Manifest,
	_ map[string][]extract.StubReference,
	_ string,
	_ LinkerDeps,
) *Linker {
	return &Linker{}
}

func (l *Linker) LinkProject(_ context.Context, projectID, repo string) (Result, error) {
	return Result{ProjectID: projectID, Repo: repo, TierCounts: map[Confidence]int{}}, ErrCGODisabled
}

func (l *Linker) ConsumersFor(_ context.Context, _, _, _ string) ([]coordinated.ConsumerRef, error) {
	return []coordinated.ConsumerRef{}, ErrCGODisabled
}
