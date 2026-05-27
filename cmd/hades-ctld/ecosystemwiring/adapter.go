//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package ecosystemwiring assembles the production-side
// handlers.EcosystemHandler adapter for the release daemon HTTP surface.
//
// Adapter satisfies handlers.EcosystemHandler by composing per-ecosystem
// *sql.DB handles + the release substrate primitives (*Ingester,
// *ChangeExtractor, *SymbolIndex, per-ecosystem Source maps).
//
// G-7 fix-cycle shipped the daemon-side EcosystemHandler interface
// + 8 handlers + 1 GET; production wiring was deferred per option B
// graceful-degradation (handlers return 503 when SetEcosystemHandler hasn't
// run). This package ships the deferred adapter + the boot-time wiring
// helper (TryWire) that cmd/hades-ctld main.go calls to inject it.
//
// Architecture (per task spec "option B-realized"):
//
// - One *sql.DB per ecosystem ("go", "python", "typescript", "rust") opened
// at <dataRoot>/global/ecosystem/<eco>/ecosystem.db with sqlite-vec auto-
// extension + foreign_keys=ON + WAL journal mode. Migrations applied via
// internal/research/ecosystem.ApplyMigrations on each open.
//
// - Pin / PrunePreview / Prune execute SQL directly against the per-eco DB
// because these are pure storage ops (ecosystem_versions UPDATE/DELETE
// with FK cascade to chunks / fp32 / symbols / changes / fts / vec_bin).
// No upstream call, no LLM, no embedder required.
//
// - IngestDelta delegates to *Ingester.Ingest(IngestRequest{DeltaOnly:true}).
// Requires Source map registered for the requested ecosystem; absent
// sources surface as wrapped error.
//
// - SweepChunkFingerprints recomputes sha256(content_text) per chunk and
// re-writes chunk_fingerprint where drift detected (invariant
// fingerprint-stability sweep).
//
// - SweepChangeNodes delegates to *ChangeExtractor.SweepChangeNodes which
// rejects orphan (version_from, version_to) tuples against
// ecosystem_versions (invariant).
//
// - RebuildSymbolIndex delegates to *SymbolIndex.Rebuild — atomic reload
// of the per-ecosystem in-memory symbol set from ecosystem_symbols.
//
// - CASGarbageCollect deletes unreferenced ecosystem_chunks_vec_bin rows
// where the chunk_id no longer exists in ecosystem_chunks (FK CASCADE
// handles the normal case; this sweep catches inconsistency introduced
// by partial-write recovery). The DB-level CAS GC F
// (research-findings CAS dir) is orthogonal and lives in a different
// subsystem; this method scopes only to ecosystem.db.
//
// - DetectNewVersions invokes Source.FetchManifest on every registered
// source for the ecosystem, collects all upstream Manifest.Packages[].
// Versions, diffs against ecosystem_versions, returns the new set.
//
// Boundary this package is in cmd/hades-ctld/ (composition root); it
// imports internal/store transitively via internal/research/ecosystem's
// migrations (sqlite_vec auto-extension) but not internal/store directly.
// invariant enforces the boundary at internal/research/ecosystem/*
// (compliance test tests/compliance/no_store_in_ecosystem_test.go), not
// at cmd/.
//
// Why a sub-package (not in package main): the cmd/hades-ctld test
// binary suffers a pre-existing baseline driver double-registration
// panic (mattn/go-sqlite3 + ncruces/go-sqlite3 both register "sqlite3"
// via init); see internal/daemon/server_knowledge_aggregator.go lines
// 13-24 for the canonical doc. Production main.go works because mattn
// registers first; the test binary's link order yields a panic. Hosting
// the adapter in its own sub-package keeps the unit tests runnable
// (the package's transitive imports do not pull internal/knowledge,
// which is the source of the ncruces driver init).
//
// Graceful-degradation contract: when constructor receives nil per-eco DB
// for an ecosystem, that ecosystem's ops return a typed error mapped to 503
// by the handler. The adapter never panics on missing deps — every method
// nil-checks at entry.
//
// (handlers shipped; production adapter deferred).

package ecosystemwiring

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

var ErrEcosystemDBNotConfigured = errors.New(
	"ecosystem: per-ecosystem DB not configured for ecosystem (run `hades providers init` then restart daemon)",
)

var ErrEcosystemSourcesNotConfigured = errors.New(
	"ecosystem: no Sources registered for ecosystem (operator must wire source impls in TryWire)",
)

type AdapterDeps struct {
	PerEcosystemDB map[ecosystem.Ecosystem]*sql.DB

	Ingester *ecosystem.Ingester

	ChangeExtractor *ecosystem.ChangeExtractor

	SymbolIndex *ecosystem.SymbolIndex

	Sources map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source
}

type Adapter struct {
	deps AdapterDeps
}

// New constructs the adapter from deps. The
// caller (TryWire) is responsible for opening + migrating
// the per-ecosystem DBs before invoking this; deps validation here is
// shape-only (rejects all-nil deps so a misconfigured caller fails loud
// instead of silently injecting a no-op adapter).
//
// Returns an error when PerEcosystemDB is empty — at least one ecosystem
// MUST be wired for the adapter to add value over the 503 graceful-fail
// path (an empty adapter would surface 500 on every op, worse than 503).
func New(deps AdapterDeps) (*Adapter, error) {
	if len(deps.PerEcosystemDB) == 0 {
		return nil, errors.New("ecosystem: New: at least one PerEcosystemDB entry required")
	}
	return &Adapter{deps: deps}, nil
}

func (a *Adapter) dbFor(ecoName string) (*sql.DB, error) {
	if a == nil {
		return nil, errors.New("ecosystem: nil Adapter")
	}
	eco := ecosystem.Ecosystem(ecoName)
	db, ok := a.deps.PerEcosystemDB[eco]
	if !ok || db == nil {
		return nil, fmt.Errorf("%w: %s", ErrEcosystemDBNotConfigured, ecoName)
	}
	return db, nil
}

func (a *Adapter) Pin(ctx context.Context, ecoName, version string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.Pin")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return err
	}

	const countMatching = `
		SELECT COUNT(*) FROM ecosystem_versions v
		JOIN ecosystem_packages p ON v.package_id = p.id
		WHERE p.ecosystem = ? AND v.version = ?
	`
	const countPinned = `
		SELECT COUNT(*) FROM ecosystem_versions v
		JOIN ecosystem_packages p ON v.package_id = p.id
		WHERE p.ecosystem = ? AND v.version = ? AND v.indefinite_retain = 1
	`
	const doPin = `
		UPDATE ecosystem_versions SET indefinite_retain = 1
		WHERE version = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`

	var matching int
	if err := db.QueryRowContext(ctx, countMatching, ecoName, version).Scan(&matching); err != nil {
		return fmt.Errorf("ecosystem Pin count-matching: %w", err)
	}
	if matching == 0 {
		return ecospec.ErrEcosystemVersionNotFound
	}

	var pinned int
	if err := db.QueryRowContext(ctx, countPinned, ecoName, version).Scan(&pinned); err != nil {
		return fmt.Errorf("ecosystem Pin count-pinned: %w", err)
	}
	if pinned == matching {
		return ecospec.ErrEcosystemPinAlreadyPinned
	}

	if _, err := db.ExecContext(ctx, doPin, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Pin update: %w", err)
	}
	return nil
}

func (a *Adapter) PrunePreview(ctx context.Context, ecoName, version string) (ecospec.EcosystemPrunePreviewResult, error) {
	if a == nil {
		return ecospec.EcosystemPrunePreviewResult{}, errors.New("ecosystem: nil Adapter.PrunePreview")
	}
	if err := ctx.Err(); err != nil {
		return ecospec.EcosystemPrunePreviewResult{}, err
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return ecospec.EcosystemPrunePreviewResult{}, err
	}

	res := ecospec.EcosystemPrunePreviewResult{
		Ecosystem: ecoName,
		Version:   version,
	}

	const matchingCounts = `
		SELECT
			COUNT(*) AS total,
			COUNT(CASE WHEN v.indefinite_retain = 1 THEN 1 END) AS pinned
		FROM ecosystem_versions v
		JOIN ecosystem_packages p ON v.package_id = p.id
		WHERE p.ecosystem = ? AND v.version = ?
	`
	var total, pinned int
	if err := db.QueryRowContext(ctx, matchingCounts, ecoName, version).Scan(&total, &pinned); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview version-count: %w", err)
	}
	if total == 0 {
		return res, ecospec.ErrEcosystemVersionNotFound
	}
	res.Pinned = (pinned == total)

	const chunkCount = `
		SELECT COUNT(*) FROM ecosystem_chunks
		WHERE version_introduced = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if err := db.QueryRowContext(ctx, chunkCount, version, ecoName).Scan(&res.ChunkCount); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview chunks: %w", err)
	}

	const fp32Count = `
		SELECT COUNT(*) FROM ecosystem_chunks_fp32
		WHERE chunk_id IN (
			SELECT c.id FROM ecosystem_chunks c
			WHERE c.version_introduced = ? AND c.package_id IN (
				SELECT id FROM ecosystem_packages WHERE ecosystem = ?
			)
		)
	`
	if err := db.QueryRowContext(ctx, fp32Count, version, ecoName).Scan(&res.ChunkFP32Count); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview chunks_fp32: %w", err)
	}

	const symCount = `
		SELECT COUNT(*) FROM ecosystem_symbols
		WHERE introduced_in = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if err := db.QueryRowContext(ctx, symCount, version, ecoName).Scan(&res.SymbolCount); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview symbols: %w", err)
	}

	const changeCount = `
		SELECT COUNT(*) FROM ecosystem_changes
		WHERE (version_from = ? OR version_to = ?) AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if err := db.QueryRowContext(ctx, changeCount, version, version, ecoName).Scan(&res.ChangeCount); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview changes: %w", err)
	}

	const fts5Count = `
		SELECT COUNT(*) FROM ecosystem_chunks_fts
		WHERE chunk_id IN (
			SELECT c.id FROM ecosystem_chunks c
			WHERE c.version_introduced = ? AND c.package_id IN (
				SELECT id FROM ecosystem_packages WHERE ecosystem = ?
			)
		)
	`
	if err := db.QueryRowContext(ctx, fts5Count, version, ecoName).Scan(&res.FTS5Count); err != nil {
		return res, fmt.Errorf("ecosystem PrunePreview chunks_fts: %w", err)
	}

	return res, nil
}

func (a *Adapter) Prune(ctx context.Context, ecoName, version string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.Prune")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ecosystem Prune begin tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	const checkSQL = `
		SELECT
			COUNT(*) AS total,
			COUNT(CASE WHEN v.indefinite_retain = 1 THEN 1 END) AS pinned
		FROM ecosystem_versions v
		JOIN ecosystem_packages p ON v.package_id = p.id
		WHERE p.ecosystem = ? AND v.version = ?
	`
	var total, pinned int
	if err := tx.QueryRowContext(ctx, checkSQL, ecoName, version).Scan(&total, &pinned); err != nil {
		return fmt.Errorf("ecosystem Prune check: %w", err)
	}
	if total == 0 {
		return ecospec.ErrEcosystemVersionNotFound
	}
	if pinned > 0 {

		return ecospec.ErrEcosystemVersionPinned
	}

	// Step 2: cascading DELETE.
	//
	// FK CASCADE on ecosystem_chunks.package_id → ecosystem_packages.id is
	// NOT what we want here (that would drop all chunks for the package,
	// not just the one version). Instead we delete ecosystem_chunks WHERE
	// version_introduced = ? AND package_id IN (eco pkgs), then delete
	// ecosystem_versions itself. CASCADE handles chunks_fp32 + symbols +
	// changes + fts + vec_bin via per-table FK chains.
	//
	// Order matters only for the FTS5/vec_bin shadow tables — SQLite
	// cascade orders them automatically once chunks delete fires.
	//
	// ecosystem_chunks_fts to ecosystem_chunks (FTS5 virtual table has
	// no FK constraints), so the FTS rows MUST be deleted manually.
	// Likewise ecosystem_chunks_vec_bin (vec0 virtual table) — we delete
	// by chunk_id explicitly to avoid orphan vec rows.
	const deleteFTS = `
		DELETE FROM ecosystem_chunks_fts WHERE chunk_id IN (
			SELECT c.id FROM ecosystem_chunks c
			WHERE c.version_introduced = ? AND c.package_id IN (
				SELECT id FROM ecosystem_packages WHERE ecosystem = ?
			)
		)
	`
	if _, err := tx.ExecContext(ctx, deleteFTS, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete fts: %w", err)
	}

	const deleteVecBin = `
		DELETE FROM ecosystem_chunks_vec_bin WHERE chunk_id IN (
			SELECT c.id FROM ecosystem_chunks c
			WHERE c.version_introduced = ? AND c.package_id IN (
				SELECT id FROM ecosystem_packages WHERE ecosystem = ?
			)
		)
	`
	if _, err := tx.ExecContext(ctx, deleteVecBin, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete vec_bin: %w", err)
	}

	const deleteChunks = `
		DELETE FROM ecosystem_chunks
		WHERE version_introduced = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if _, err := tx.ExecContext(ctx, deleteChunks, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete chunks: %w", err)
	}

	const deleteSymbols = `
		DELETE FROM ecosystem_symbols
		WHERE introduced_in = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if _, err := tx.ExecContext(ctx, deleteSymbols, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete symbols: %w", err)
	}

	const deleteChanges = `
		DELETE FROM ecosystem_changes
		WHERE (version_from = ? OR version_to = ?) AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if _, err := tx.ExecContext(ctx, deleteChanges, version, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete changes: %w", err)
	}

	const deleteVersions = `
		DELETE FROM ecosystem_versions
		WHERE version = ? AND package_id IN (
			SELECT id FROM ecosystem_packages WHERE ecosystem = ?
		)
	`
	if _, err := tx.ExecContext(ctx, deleteVersions, version, ecoName); err != nil {
		return fmt.Errorf("ecosystem Prune delete versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ecosystem Prune commit: %w", err)
	}
	return nil
}

func (a *Adapter) IngestDelta(ctx context.Context, ecoName string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.IngestDelta")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.deps.Ingester == nil {
		return errors.New("ecosystem IngestDelta: Ingester not configured (operator must wire IngesterOptions in TryWire)")
	}
	eco := ecosystem.Ecosystem(ecoName)
	if _, ok := a.deps.Sources[eco]; !ok {
		return fmt.Errorf("%w: %s", ErrEcosystemSourcesNotConfigured, ecoName)
	}

	req := ecosystem.IngestRequest{
		Ecosystem: eco,
		DeltaOnly: true,
	}
	if _, err := a.deps.Ingester.Ingest(ctx, req); err != nil {
		return fmt.Errorf("ecosystem IngestDelta: %w", err)
	}
	return nil
}

func (a *Adapter) SweepChunkFingerprints(ctx context.Context, ecoName string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.SweepChunkFingerprints")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return err
	}

	const scanSQL = `
		SELECT c.id, c.content_text, c.chunk_fingerprint
		FROM ecosystem_chunks c
		JOIN ecosystem_packages p ON c.package_id = p.id
		WHERE p.ecosystem = ?
	`
	rows, err := db.QueryContext(ctx, scanSQL, ecoName)
	if err != nil {
		return fmt.Errorf("ecosystem SweepChunkFingerprints scan: %w", err)
	}
	defer rows.Close()

	type drift struct {
		id          int64
		newFingerpr string
	}
	var drifts []drift
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var id int64
		var content, currentFP string
		if err := rows.Scan(&id, &content, &currentFP); err != nil {
			return fmt.Errorf("ecosystem SweepChunkFingerprints scan row: %w", err)
		}
		sum := sha256.Sum256([]byte(content))
		newFP := hex.EncodeToString(sum[:])
		if newFP != currentFP {
			drifts = append(drifts, drift{id: id, newFingerpr: newFP})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ecosystem SweepChunkFingerprints rows.Err: %w", err)
	}
	if len(drifts) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ecosystem SweepChunkFingerprints begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const repairSQL = `UPDATE ecosystem_chunks SET chunk_fingerprint = ? WHERE id = ?`
	for _, d := range drifts {
		if _, err := tx.ExecContext(ctx, repairSQL, d.newFingerpr, d.id); err != nil {
			return fmt.Errorf("ecosystem SweepChunkFingerprints repair chunk %d: %w", d.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ecosystem SweepChunkFingerprints commit: %w", err)
	}
	return nil
}

// SweepChangeNodes invokes ChangeExtractor.SweepChangeNodes against the
// per-ecosystem DB. Returns wrapped error if ChangeExtractor wasn't
// wired at construction.
//
// The underlying sweep verifies invariant (Change-node graph
// consistency): every (version_from, version_to) pair MUST have
// matching ecosystem_versions rows. Orphans surface as a wrapped error
// listing the affected ids.
func (a *Adapter) SweepChangeNodes(ctx context.Context, ecoName string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.SweepChangeNodes")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.deps.ChangeExtractor == nil {
		return errors.New("ecosystem SweepChangeNodes: ChangeExtractor not configured")
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return err
	}
	if err := a.deps.ChangeExtractor.SweepChangeNodes(ctx, db); err != nil {
		return fmt.Errorf("ecosystem SweepChangeNodes: %w", err)
	}
	return nil
}

func (a *Adapter) RebuildSymbolIndex(ctx context.Context, ecoName string) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.RebuildSymbolIndex")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.deps.SymbolIndex == nil {
		return errors.New("ecosystem RebuildSymbolIndex: SymbolIndex not configured")
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return err
	}
	eco := ecosystem.Ecosystem(ecoName)
	if err := a.deps.SymbolIndex.Rebuild(ctx, db, eco); err != nil {
		return fmt.Errorf("ecosystem RebuildSymbolIndex: %w", err)
	}
	return nil
}

func (a *Adapter) CASGarbageCollect(ctx context.Context) error {
	if a == nil {
		return errors.New("ecosystem: nil Adapter.CASGarbageCollect")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	const sweepVecBin = `
		DELETE FROM ecosystem_chunks_vec_bin
		WHERE chunk_id NOT IN (SELECT id FROM ecosystem_chunks)
	`
	const sweepFTS = `
		DELETE FROM ecosystem_chunks_fts
		WHERE chunk_id NOT IN (SELECT id FROM ecosystem_chunks)
	`

	var errs []error
	for eco, db := range a.deps.PerEcosystemDB {
		if err := ctx.Err(); err != nil {
			return err
		}
		if db == nil {
			continue
		}
		if _, err := db.ExecContext(ctx, sweepVecBin); err != nil {
			errs = append(errs, fmt.Errorf("ecosystem %s CASGarbageCollect vec_bin: %w", eco, err))
			continue
		}
		if _, err := db.ExecContext(ctx, sweepFTS); err != nil {
			errs = append(errs, fmt.Errorf("ecosystem %s CASGarbageCollect fts: %w", eco, err))
			continue
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (a *Adapter) DetectNewVersions(ctx context.Context, ecoName string) ([]string, error) {
	if a == nil {
		return nil, errors.New("ecosystem: nil Adapter.DetectNewVersions")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := a.dbFor(ecoName)
	if err != nil {
		return nil, err
	}
	eco := ecosystem.Ecosystem(ecoName)
	srcMap, ok := a.deps.Sources[eco]
	if !ok || len(srcMap) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrEcosystemSourcesNotConfigured, ecoName)
	}

	upstream := map[string]struct{}{}
	for _, src := range srcMap {
		manifest, mErr := src.FetchManifest(ctx)
		if mErr != nil {

			continue
		}
		if manifest == nil {
			continue
		}
		for _, pkg := range manifest.Packages {
			for _, ver := range pkg.Versions {
				if ver == "" {
					continue
				}
				upstream[ver] = struct{}{}
			}
			if pkg.LatestStableVersion != "" {
				upstream[pkg.LatestStableVersion] = struct{}{}
			}
		}
	}

	if len(upstream) == 0 {
		return []string{}, nil
	}

	const existingVersionsSQL = `
		SELECT DISTINCT v.version FROM ecosystem_versions v
		JOIN ecosystem_packages p ON v.package_id = p.id
		WHERE p.ecosystem = ?
	`
	rows, err := db.QueryContext(ctx, existingVersionsSQL, ecoName)
	if err != nil {
		return nil, fmt.Errorf("ecosystem DetectNewVersions query existing: %w", err)
	}
	defer rows.Close()
	existing := map[string]struct{}{}
	for rows.Next() {
		var ver string
		if err := rows.Scan(&ver); err != nil {
			return nil, fmt.Errorf("ecosystem DetectNewVersions scan: %w", err)
		}
		existing[ver] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ecosystem DetectNewVersions rows.Err: %w", err)
	}

	out := make([]string, 0, len(upstream))
	for ver := range upstream {
		if _, found := existing[ver]; !found {
			out = append(out, ver)
		}
	}

	sortStringsAscending(out)
	return out, nil
}

func sortStringsAscending(s []string) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j-1] > s[j] {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
