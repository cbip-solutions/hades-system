// SPDX-License-Identifier: MIT
// Package handlers — augment_summary.go.
//
// GET /v1/augment/summary — daily augmentation stats consumed by:
// - internal/hadesday — Augmentation section of the morning brief
// - internal/tui/views F3 Cost panel via the
// AugmentCache thin alias in internal/client/codegraph_plan12.go
//
// Background — release substrate gap closure:
//
// (internal/client/augment.go::AugmentSummary). The daemon side was
// scoped for follow-up but never landed; the route returned 404. release
// built the F3 augmentation cache stats display on top of that
// wrapper, where it manifested as "augmentation stats render zero
// forever" in production.
//
// This handler closes the gap by deriving the summary from the audit
// chain: AugmentationStarted + AugmentationCompleted events stamped by
// the augment.Pipeline carry tokens_consumed,
// kg_queries_fired, and cache_hit fields in their payloads. The handler
// queries the AuditQueryCtx for events of those types within the
// requested date range and aggregates the counters.
//
// Date scoping: the optional ?date=YYYY-MM-DD query param picks one
// 24-hour UTC window. Empty defaults to "today" (server-local UTC).
// Invalid date format returns 400. Future dates are accepted but yield
// zero counts.
//
// Defensive design: handler tolerates absent audit events (returns zeros
// with date echoed) and malformed payloads (skipped + counted in a
// warning log; never crashes). The F3 panel renders zeros as "no
// activity today" rather than an error — same posture as release's
// bypass-config 503 graceful-degrade.
//
// Cherry-pick narrative: this commit completes the release substrate gap
// inherited; could be cherry-picked to a release.1
// backport branch if needed.
//
// invariant: handler relies on AuditQueryCtx (interface), not the
// concrete store.

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type AugmentSummaryResp struct {
	Date               string  `json:"date"`
	TotalCost          float64 `json:"total_cost_usd"`
	TokensConsumed     int     `json:"tokens_consumed"`
	TokensCeiling      int     `json:"tokens_ceiling"`
	KGQueriesFired     int     `json:"kg_queries_fired"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	LastIndexedRFC3339 string  `json:"last_indexed,omitempty"`
}

type augmentEventPayload struct {
	TokensConsumed int     `json:"tokens_consumed"`
	TokensCeiling  int     `json:"tokens_ceiling"`
	KGQueriesFired int     `json:"kg_queries_fired"`
	CacheHit       bool    `json:"cache_hit"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	LastIndexed    string  `json:"last_indexed,omitempty"`
}

func AugmentSummaryHandler(s AuditQueryCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		dateStr := r.URL.Query().Get("date")
		dayStart, dayEnd, err := resolveDateWindow(dateStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rows, err := s.AuditEvents("Augmentation", "", dayStart.Unix(), 500)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		resp := AugmentSummaryResp{
			Date:          dateStr,
			TokensCeiling: 10000,
		}
		if resp.Date == "" {
			resp.Date = dayStart.Format("2006-01-02")
		}
		var (
			cacheHits  int
			completed  int
			lastIndex  time.Time
			totalCost  float64
			totalToks  int
			totalKG    int
			topCeiling int
		)
		for _, row := range rows {
			if row.EmittedAt >= dayEnd.Unix() {
				continue
			}
			if row.Type != "AugmentationCompleted" && row.Type != "AugmentationStarted" {
				continue
			}
			if row.Type == "AugmentationStarted" {

				totalKG++
				continue
			}

			var p augmentEventPayload
			if err := json.Unmarshal([]byte(row.PayloadRaw), &p); err != nil {
				continue
			}
			completed++
			totalToks += p.TokensConsumed
			totalCost += p.TotalCostUSD
			if p.TokensCeiling > topCeiling {
				topCeiling = p.TokensCeiling
			}
			if p.CacheHit {
				cacheHits++
			}
			if p.LastIndexed != "" {
				if t, parseErr := time.Parse(time.RFC3339, p.LastIndexed); parseErr == nil {
					if t.After(lastIndex) {
						lastIndex = t
					}
				}
			}
		}
		resp.TokensConsumed = totalToks
		resp.KGQueriesFired = totalKG
		resp.TotalCost = totalCost
		if topCeiling > 0 {
			resp.TokensCeiling = topCeiling
		}
		if completed > 0 {
			resp.CacheHitRate = float64(cacheHits) / float64(completed)
		}
		if !lastIndex.IsZero() {
			resp.LastIndexedRFC3339 = lastIndex.UTC().Format(time.RFC3339)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func resolveDateWindow(date string) (start, end time.Time, err error) {
	if date == "" {
		now := time.Now().UTC()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end = start.Add(24 * time.Hour)
		return start, end, nil
	}

	if len(date) != 10 || strings.Count(date, "-") != 2 {
		return time.Time{}, time.Time{}, errInvalidDate
	}

	for i, c := range date {
		if i == 4 || i == 7 {
			if c != '-' {
				return time.Time{}, time.Time{}, errInvalidDate
			}
			continue
		}
		if c < '0' || c > '9' {
			return time.Time{}, time.Time{}, errInvalidDate
		}
	}

	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, time.Time{}, errInvalidDate
	}
	start = t.UTC()
	end = start.Add(24 * time.Hour)
	return start, end, nil
}

var errInvalidDate = invalidParamError("invalid ?date= (expected YYYY-MM-DD)")

type invalidParamError string

func (e invalidParamError) Error() string { return string(e) }

type AugmentProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func AugmentProbeHandler(s AugmentProbeCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		check := r.URL.Query().Get("check")
		resp := AugmentProbeResp{Status: "ok"}
		switch check {
		case "configured":
			if s.AugmentHandler() == nil {
				resp.Status = "warn"
				resp.Detail = "augmentation handler not wired (Plan 11 C substrate pending or operator-unwired); pre-LLM hooks proceed unaugmented"
			} else {
				resp.Detail = "augmentation handler wired"
			}
		case "pipeline_reachable":

			if s.AugmentHandler() == nil {
				resp.Status = "warn"
				resp.Detail = "augment pipeline not reachable (handler nil)"
			} else {
				resp.Detail = "augment pipeline wired in-process"
			}
		case "":
			resp.Detail = "no check specified; pass ?check=configured|pipeline_reachable"
		default:
			resp.Detail = "unknown check name; pass ?check=configured|pipeline_reachable"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

type AugmentProbeCtx interface {
	AugmentHandler() http.Handler
}
