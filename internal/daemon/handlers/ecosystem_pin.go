// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_pin.go.
//
// EcosystemPin implements POST /v1/ecosystem/pin.
//
// Sets ecosystem_versions.indefinite_retain=true for the (ecosystem,
// version) tuple. Pinned versions are excluded from the 2-prior-stable
// retention window governed by the design choice cron sweep and rejected by
// DELETE /v1/ecosystem/version with 409 Conflict.
//
// Status codes:
//
// 204 No Content — pin committed (and seam returns nil error).
// 400 Bad Request — invalid JSON body or missing field.
// 404 Not Found — unknown (ecosystem, version) tuple.
// 409 Conflict — already pinned (idempotent failure; CLI maps to recoverable).
// 500 Internal Server Error — opaque seam failure.
// 503 Service Unavailable — EcosystemHandler unavailable.
//
// Wire mirror: matches client.EcosystemPin(eco, ver) → POST body
// {"ecosystem": "<X>", "version": "<Y>"} → 204 on success.
package handlers

import (
	"context"
	"net/http"
)

func EcosystemPin(s any) http.HandlerFunc {
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
		if err := h.Pin(ctx, eco, ver); err != nil {
			code, msg := mapEcosystemError(err)
			http.Error(w, msg, code)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
