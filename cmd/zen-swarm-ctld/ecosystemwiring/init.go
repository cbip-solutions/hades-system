//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystemwiring

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type EcosystemHandlerSetter interface {
	SetEcosystemHandler(h ecospec.EcosystemHandler)
}

func TryWire(
	ctx context.Context,
	srv EcosystemHandlerSetter,
	logger *slog.Logger,
	dataRoot, configDir string,
) (adapter *Adapter, cleanup func() error) {

	noop := func() error { return nil }

	if srv == nil || logger == nil {

		return nil, noop
	}

	probePath := filepath.Join(configDir, "ecosystem-embedder.toml")
	if _, err := os.Stat(probePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Info("Plan 14 ecosystem handler: NOT WIRED (no provider configs)",
				"routes", "/v1/ecosystem/* (pin, prune, prune-preview, ingest-delta, sweep/*, new-versions)",
				"effect", "503 ecosystem-handler-not-configured; cron worker stops 404ing once configs land",
				"resolution", "run `zen providers init` (creates ~/.config/zen-swarm/providers/ecosystem-*.toml) and restart daemon",
			)
			return nil, noop
		}
		logger.Warn("Plan 14 ecosystem handler: NOT WIRED (config probe failed)",
			"err", err,
			"probe_path", probePath,
		)
		return nil, noop
	}

	if _, err := config.LoadEcosystemEmbedderConfig(configDir); err != nil {
		logger.Warn("Plan 14 ecosystem handler: NOT WIRED (embedder TOML malformed)",
			"err", err,
			"resolution", "fix ~/.config/zen-swarm/providers/ecosystem-embedder.toml and restart daemon",
		)
		return nil, noop
	}

	for _, probe := range []struct {
		name string
		load func(string) error
	}{
		{"ecosystem-prefix.toml", func(d string) error { _, e := config.LoadEcosystemPrefixConfig(d); return e }},
		{"ecosystem-reranker.toml", func(d string) error { _, e := config.LoadEcosystemRerankerConfig(d); return e }},
		{"ecosystem-router.toml", func(d string) error { _, e := config.LoadEcosystemRouterConfig(d); return e }},
		{"ecosystem-version-detect.toml", func(d string) error { _, e := config.LoadEcosystemVersionDetectConfig(d); return e }},
	} {
		if err := probe.load(configDir); err != nil {
			logger.Warn("Plan 14 ecosystem handler: NOT WIRED (TOML malformed)",
				"file", probe.name,
				"err", err,
				"resolution", fmt.Sprintf("fix ~/.config/zen-swarm/providers/%s and restart daemon", probe.name),
			)
			return nil, noop
		}
	}

	perEcoDB := map[ecosystem.Ecosystem]*sql.DB{}
	openedPaths := []string{}
	for _, eco := range ecosystem.AllEcosystems {
		dbDir := filepath.Join(dataRoot, "global", "ecosystem", string(eco))
		if err := os.MkdirAll(dbDir, 0o700); err != nil {
			logger.Warn("Plan 14 ecosystem handler: mkdir failed",
				"ecosystem", eco,
				"dir", dbDir,
				"err", err,
			)
			continue
		}
		dbPath := filepath.Join(dbDir, "ecosystem.db")

		db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
		if err != nil {
			logger.Warn("Plan 14 ecosystem handler: sql.Open failed",
				"ecosystem", eco,
				"db_path", dbPath,
				"err", err,
			)
			continue
		}
		if err := ecosystem.ApplyMigrations(db); err != nil {
			logger.Warn("Plan 14 ecosystem handler: ApplyMigrations failed",
				"ecosystem", eco,
				"db_path", dbPath,
				"err", err,
			)
			_ = db.Close()
			continue
		}
		perEcoDB[eco] = db
		openedPaths = append(openedPaths, dbPath)
	}

	if len(perEcoDB) == 0 {
		logger.Warn("Plan 14 ecosystem handler: NOT WIRED (no per-ecosystem DB opened)",
			"resolution", "check filesystem permissions on "+filepath.Join(dataRoot, "global", "ecosystem"),
		)
		return nil, noop
	}

	changeExt := ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{})
	symIdx := ecosystem.NewSymbolIndex()

	adapter, err := New(AdapterDeps{
		PerEcosystemDB:  perEcoDB,
		ChangeExtractor: changeExt,
		SymbolIndex:     symIdx,
	})
	if err != nil {
		logger.Warn("Plan 14 ecosystem handler: NOT WIRED (constructor failed)",
			"err", err,
		)

		for _, db := range perEcoDB {
			_ = db.Close()
		}
		return nil, noop
	}

	cleanup = func() error {
		var firstErr error
		for _, db := range perEcoDB {
			if err := db.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	srv.SetEcosystemHandler(adapter)
	logger.Info("Plan 14 ecosystem handler live",
		"routes", "/v1/ecosystem/{pin,prune,prune-preview,ingest-delta,sweep/*,new-versions/*}",
		"ecosystems_wired", len(perEcoDB),
		"db_paths", openedPaths,
		"substrate", "Pin/Prune/PrunePreview/Sweep* end-to-end; IngestDelta + DetectNewVersions return 500 (Source registration follow-up)",
	)

	_ = ctx
	return adapter, cleanup
}
