// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_version_delete.go.
//
// EcosystemVersionDelete implements DELETE /v1/ecosystem/version.
//
// Hard-removes the (ecosystem, version) row from ecosystem_versions and
// cascade-deletes its chunks, chunks_fp32, symbols, changes, and FTS5
// entries from ecosystem.db. The deleted data is rebuildable via
// `hades docs reindex --ecosystem <X> --version <Y>`.
//
// Pinned versions (indefinite_retain=true) are refused with 409 Conflict;
// the operator must `hades docs unpin` first. The CLI safety gate (promptYN
// confirmation) is enforced upstream in docs_prune.go RunDocsPrune; this
// handler is the unguarded transport.
//
// Status codes:
//
// 204 No Content — cascade delete committed.
// 400 Bad Request — invalid JSON body or missing field.
// 404 Not Found — unknown (ecosystem, version) tuple.
// 409 Conflict — version is pinned (operator must unpin first).
// 500 Internal Server Error — opaque seam failure.
// 503 Service Unavailable — EcosystemHandler unavailable.
//
// Wire mirror: matches client.EcosystemPrune(ctx, eco, ver) →
// DELETE /v1/ecosystem/version + JSON body
// {"ecosystem": "<X>", "version": "<Y>"} → 204 on success.
package handlers

import (
	"context"
	"net/http"
)

func EcosystemVersionDelete(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := resolveEcosystemHandler(s)
		if h == nil {
			ecosystemUnavailable(w)
			return
		}
		eco, ver, err := decodeEcosystemAndVersion(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), ecosystemHandlerTimeout)
		defer cancel()
		if err := h.Prune(ctx, eco, ver); err != nil {
			code, msg := mapEcosystemError(err)
			http.Error(w, msg, code)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
