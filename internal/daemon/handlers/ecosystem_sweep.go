// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_sweep.go (Plan 14 Phase G fix-cycle).
//
// Four weekly-sweep handlers backing the cron worker's WeeklySweep
// (cmd/zen-docs-cron Sunday 03:00 local; see also master plan §5 G-6).
// Each handler is idempotent per inv-zen-204 (re-running produces zero
// schema diff). Operator-initiated invocations from `zen doctor` and
// the cron worker share the same code path.
//
//	POST /v1/ecosystem/sweep/fingerprints              — chunk fingerprint sweep
//	POST /v1/ecosystem/sweep/change-nodes              — Change-node graph sweep
//	POST /v1/ecosystem/sweep/rebuild-symbol-index      — symbol index rebuild
//	POST /v1/ecosystem/sweep/cas-gc                    — Plan 9 F CAS garbage collect
//
// Status codes (shared across all four):
//
//	204 No Content — sweep completed successfully (or zero work).
//	400 Bad Request — invalid JSON body, unknown ecosystem (per-eco sweeps only).
//	500 Internal Server Error — sweep failure (chunk count mismatch, etc).
//	503 Service Unavailable — EcosystemHandler not wired.
//
// Wire mirror per endpoint:
//
//	daemonCronClient.SweepChunkFingerprints(ctx, eco) →
//	  POST /v1/ecosystem/sweep/fingerprints + {"ecosystem": "<X>"}
//	daemonCronClient.SweepChangeNodes(ctx, eco) →
//	  POST /v1/ecosystem/sweep/change-nodes + {"ecosystem": "<X>"}
//	daemonCronClient.RebuildSymbolIndex(ctx, eco) →
//	  POST /v1/ecosystem/sweep/rebuild-symbol-index + {"ecosystem": "<X>"}
//	daemonCronClient.CASGarbageCollect(ctx) →
//	  POST /v1/ecosystem/sweep/cas-gc (no body)
//
// Concurrency each sweep operation runs sequentially within an
// ecosystem; the cron worker fans out across all four ecosystems in
// parallel goroutines so the four handlers may run concurrently for
// different ecosystem values. The seam implementation is responsible
// for any internal locking (ecosystem.db SQLite WAL provides reader/
// writer isolation; ApplyMigrations is idempotent).
package handlers

import (
	"context"
	"net/http"
)

func EcosystemSweepFingerprints(s any) http.HandlerFunc {
	return ecosystemSweepEcoHandler(s, func(ctx context.Context, h EcosystemHandler, eco string) error {
		return h.SweepChunkFingerprints(ctx, eco)
	})
}

func EcosystemSweepChangeNodes(s any) http.HandlerFunc {
	return ecosystemSweepEcoHandler(s, func(ctx context.Context, h EcosystemHandler, eco string) error {
		return h.SweepChangeNodes(ctx, eco)
	})
}

func EcosystemSweepRebuildSymbolIndex(s any) http.HandlerFunc {
	return ecosystemSweepEcoHandler(s, func(ctx context.Context, h EcosystemHandler, eco string) error {
		return h.RebuildSymbolIndex(ctx, eco)
	})
}

func EcosystemSweepCASGC(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := resolveEcosystemHandler(s)
		if h == nil {
			ecosystemUnavailable(w)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), sweepHandlerTimeout)
		defer cancel()
		if err := h.CASGarbageCollect(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ecosystemSweepEcoHandler(
	s any,
	fn func(context.Context, EcosystemHandler, string) error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := resolveEcosystemHandler(s)
		if h == nil {
			ecosystemUnavailable(w)
			return
		}
		eco, err := decodeEcosystemOnly(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), sweepHandlerTimeout)
		defer cancel()
		if err := fn(ctx, h, eco); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
