// SPDX-License-Identifier: MIT
// Package handlers — zenday.go.
//
// Three routes for the zen day operator surface:
//
// POST /v1/zen-day/morning — generate (or re-render via force) today's morning brief
// POST /v1/zen-day/eod — generate (or re-render via force) today's EOD digest
// POST /v1/zen-day/check-pending — ephemeral introspection (next-fire + counts since last)
//
// These dispatch to a *zenday.Generator wired at daemon boot (
// composes MorningDeps / EODDeps / CheckPendingDeps from inbox +
// scheduler + eventlog + clock + cost-ledger and hands the Generator to
// SetDayGenerator). The Generator façade closes over the dependency
// bundles so handlers hold a single value-type and do not re-assemble
// deps each call.
//
// Status-code mapping (mirrors the projects_p7 + inbox_p7 patterns):
//
// 503 — DayGenerator() not yet wired (cmd/zen-swarm-ctld registers
// the generator at boot; tests inject fakes via SetDayGenerator).
// 400 — invalid JSON body.
// 409 — today's brief already generated and force=false (idempotency
// per spec §1 Q13 C; CLI surfaces as ErrRecoverable so the
// operator sees exit 1 + a hint to re-run with --force).
// 500 — opaque collection / disk / emit errors.
// 200 — success; body is the rendered BriefDoc JSON (handler decides
// to return the doc rather than the markdown so the CLI can
// Render with the canonical zenday.Render template; this keeps
// the rendering logic in ONE place — the zenday package — and
// the wire shape is the typed BriefDoc per spec §1 Q15 alias).
//
// invariant boundary: this handler imports internal/zenday value
// types only (BriefDoc / Generator / sentinel errors). No internal/store
// imports — the Generator interface is structural and the daemon-side
// accessor returns it as the same interface, keeping the boundary at
// the interface layer (mirrors handlers.InboxStore + handlers.QuietStore
// gate patterns).
//
// CLI surface (handled in internal/cli/day.go):
//
// zen day — default, runs morning brief
// zen day --force — re-renders today's morning brief
// zen day --eod — runs EOD digest
// zen day --check-pending — runs introspection preview
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type DayGenerator interface {
	GenerateMorningBrief(ctx context.Context, force bool) (zenday.BriefDoc, error)
	GenerateEODDigest(ctx context.Context, force bool) (zenday.BriefDoc, error)
	CheckPending(ctx context.Context) (zenday.BriefDoc, error)
}

type dayGeneratorAccessor interface {
	DayGenerator() DayGenerator
}

func resolveDayGenerator(s any) DayGenerator {
	acc, ok := s.(dayGeneratorAccessor)
	if !ok {
		return nil
	}
	return acc.DayGenerator()
}

func dayUnavailable(w http.ResponseWriter) {
	http.Error(w, "zen day generator not configured", http.StatusServiceUnavailable)
}

const dayHandlerTimeout = 25 * time.Second

type DayMorningRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayEODRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayCheckPendingRequest struct{}

func DayMorningHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gen := resolveDayGenerator(s)
		if gen == nil {
			dayUnavailable(w)
			return
		}
		req := DayMorningRequest{}

		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), dayHandlerTimeout)
		defer cancel()
		doc, err := gen.GenerateMorningBrief(ctx, req.Force)
		if err != nil {
			if errors.Is(err, zenday.ErrAlreadyGenerated) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

func DayEODHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gen := resolveDayGenerator(s)
		if gen == nil {
			dayUnavailable(w)
			return
		}
		req := DayEODRequest{}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), dayHandlerTimeout)
		defer cancel()
		doc, err := gen.GenerateEODDigest(ctx, req.Force)
		if err != nil {
			if errors.Is(err, zenday.ErrAlreadyGenerated) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

func DayCheckPendingHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gen := resolveDayGenerator(s)
		if gen == nil {
			dayUnavailable(w)
			return
		}
		req := DayCheckPendingRequest{}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), dayHandlerTimeout)
		defer cancel()
		doc, err := gen.CheckPending(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}
