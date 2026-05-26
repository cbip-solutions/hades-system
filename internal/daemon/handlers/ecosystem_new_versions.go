// SPDX-License-Identifier: MIT
// Package handlers — ecosystem_new_versions.go (Plan 14 Phase G fix-cycle).
//
// EcosystemNewVersions implements GET /v1/ecosystem/new-versions/{eco}.
//
// Queries the upstream registry for the named ecosystem (via
// internal/research/cache.Revalidator-backed Source.FetchManifest on the
// daemon side) and returns the versions newly published since the last
// successful poll. The cron worker (cmd/zen-docs-cron) calls this every
// 6 hours; an empty list (no new versions) is a normal no-op return.
//
// Path parameter:
//
//	{eco}  — one of: go, python, typescript, rust
//
// Status codes:
//
//	200 OK — JSON body {"versions": ["1.23.0", ...]} (Versions non-nil; may be []).
//	400 Bad Request — missing/unknown ecosystem path param.
//	500 Internal Server Error — upstream fetch failure.
//	503 Service Unavailable — EcosystemHandler not wired.
//
// Wire mirror: matches daemonCronClient.DetectNewVersions(ctx, eco) →
// GET /v1/ecosystem/new-versions/<eco> → 200 + JSON body. Note: the
// existing cron worker's daemonCronClient.DetectNewVersions stub returns
// nil/nil (the cron worker uses no-op semantics until this handler
// lands); the handler is wired here so future cron worker iterations
// can call the real GET path without further coordination.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func EcosystemNewVersions(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := resolveEcosystemHandler(s)
		if h == nil {
			ecosystemUnavailable(w)
			return
		}
		eco := strings.TrimSpace(r.PathValue("eco"))
		if eco == "" {
			http.Error(w, "ecosystem path param is required", http.StatusBadRequest)
			return
		}
		if !isValidEcosystem(eco) {
			http.Error(w, fmt.Sprintf("unknown ecosystem %q; must be one of: %s",
				eco, strings.Join(validEcosystems, ", ")), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), ecosystemHandlerTimeout)
		defer cancel()
		versions, err := h.DetectNewVersions(ctx, eco)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if versions == nil {
			versions = []string{}
		}
		writeJSON(w, http.StatusOK, EcosystemNewVersionsResponse{Versions: versions})
	}
}
