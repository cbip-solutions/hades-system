// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_ingest_delta.go.
//
// EcosystemIngestDelta implements POST /v1/ecosystem/ingest-delta.
//
// Schedules a delta-index run for the named ecosystem. The cron worker
// (cmd/zen-docs-cron) calls this every 6 hours after DetectNewVersions
// surfaces a newly-published upstream version; the operator may also
// trigger it manually via `zen docs reindex --ecosystem <X>`.
//
// The daemon-side seam dispatches to
// internal/research/ecosystem.Dispatcher.IngestDelta which:
//
// 1. Fetches the upstream package manifest (cache-revalidator backed).
// 2. Computes the delta vs the local ecosystem_versions snapshot.
// 3. Schedules new-version chunk/symbol/change-node ingestion.
//
// The handler returns once dispatch is scheduled; actual ingestion runs
// async on the daemon side per spec §4.4 (cron worker tolerates partial
// failure — errors per ecosystem are surfaced via errors.Join in the
// cron worker's PollUpstream aggregator).
//
// Status codes:
//
// 204 No Content — delta-ingest scheduled successfully.
// 400 Bad Request — invalid JSON body or unknown ecosystem.
// 500 Internal Server Error — dispatch failure.
// 503 Service Unavailable — EcosystemHandler not wired.
//
// Wire mirror: matches daemonCronClient.IngestDelta(ctx, eco) →
// POST /v1/ecosystem/ingest-delta + JSON body
// {"ecosystem": "<X>"} → 204 on success.
package handlers

import (
	"context"
	"net/http"
)

func EcosystemIngestDelta(s any) http.HandlerFunc {
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
		if err := h.IngestDelta(ctx, eco); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
