//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package ecosystemwiring

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type EcosystemHandlerSetter interface {
	SetEcosystemHandler(h ecospec.EcosystemHandler)
}

func TryWire(
	_ context.Context,
	_ EcosystemHandlerSetter,
	logger *slog.Logger,
	_, _ string,
) (*Adapter, func() error) {
	if logger != nil {
		logger.Info("HADES design ecosystem handler: unavailable (CGO disabled)",
			"effect", "/v1/ecosystem/* returns 503; rebuild daemon with CGO_ENABLED=1 to enable",
		)
	}
	return nil, func() error { return nil }
}

var ErrCGORequired = errors.New("ecosystemwiring: adapter requires CGO; rebuild with CGO_ENABLED=1")

var ErrEcosystemDBNotConfigured = errors.New("ecosystem: per-ecosystem DB not configured (nocgo build)")

var ErrEcosystemSourcesNotConfigured = errors.New("ecosystem: no Sources registered (nocgo build)")

type AdapterDeps struct {
	PerEcosystemDB  map[ecosystem.Ecosystem]*sql.DB
	Ingester        *ecosystem.Ingester
	ChangeExtractor *ecosystem.ChangeExtractor

	SymbolIndex any
	Sources     map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source
}

type Adapter struct{}

func New(_ AdapterDeps) (*Adapter, error) {
	return nil, ErrCGORequired
}

func (a *Adapter) Pin(_ context.Context, _, _ string) error { return ErrCGORequired }
func (a *Adapter) PrunePreview(_ context.Context, _, _ string) (ecospec.EcosystemPrunePreviewResult, error) {
	return ecospec.EcosystemPrunePreviewResult{}, ErrCGORequired
}
func (a *Adapter) Prune(_ context.Context, _, _ string) error               { return ErrCGORequired }
func (a *Adapter) IngestDelta(_ context.Context, _ string) error            { return ErrCGORequired }
func (a *Adapter) SweepChunkFingerprints(_ context.Context, _ string) error { return ErrCGORequired }
func (a *Adapter) SweepChangeNodes(_ context.Context, _ string) error       { return ErrCGORequired }
func (a *Adapter) RebuildSymbolIndex(_ context.Context, _ string) error     { return ErrCGORequired }
func (a *Adapter) CASGarbageCollect(_ context.Context) error                { return ErrCGORequired }
func (a *Adapter) DetectNewVersions(_ context.Context, _ string) ([]string, error) {
	return nil, ErrCGORequired
}
