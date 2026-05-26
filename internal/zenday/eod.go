// SPDX-License-Identifier: MIT
// Package zenday — EOD digest idempotent composer.
//
// GenerateEODDigest is the user-facing entry point invoked by both the
// cron firing (Phase D scheduler at 0 18 * * 1-5) and the operator-pull
// (`bin/zen day --eod`). Per spec §1 Q15 C: the EOD digest composes
// per-project status sections from `HandoffPosted` events read from the
// per-project event-log + aggregator cache, with per-project opt-out
// via `zenswarm.toml [project] zen_day_eod_summary = false` (default
// `true`).
//
// Pipeline
//
//  1. Compute today UTC midnight from deps.Clock.Now().
//  2. Resolve archive path via deps.Paths.EODDigestPath.
//  3. If !force and os.Stat(path) succeeds → return ErrAlreadyGenerated.
//  4. Query HandoffPosted events for [today, endOfDay) via
//     deps.Eventlog.QueryByType. HARD failure: digest cannot be
//     authoritative without the event-log probe.
//  5. Decode each EventRecord.PayloadJSON into HandoffPostedPayload;
//     malformed payloads are silently skipped (doctor probe surfaces).
//     Dedup per-project: latest Timestamp wins.
//  6. Read AutonomySnapshot to discover projects with NO handoff today
//     (manual-mode coverage). SOFT failure: continue with handoffs only.
//  7. For each project alias (sorted asc), consult
//     deps.ProjectConfig.IsEODOptedIn → skip section if opted out.
//  8. Build ProjectStatusSection per project; HandoffPosted wins over
//     AutonomySnapshot when both exist.
//  9. Sum cost ledger across projects via deps.Cost.SpendByProject.
//     SOFT failure: CostWatchUSD = 0.
//  10. Render → MkdirAll(parent) → WriteFile(path).
//  11. Emit EODDigestReadyEvent (best-effort: emit-failure does NOT
//     void the digest; the file is on disk regardless).
//
// Latest-wins semantics: if `/handoff` was invoked twice today (operator
// clarified midstream), the second event's contents represent the
// canonical EOD. We dedup by ProjectAlias, keeping the row with the
// greatest Timestamp.
//
// Privacy contract: HandoffPostedEvent fields (Summary, Blockers,
// NextSession) MUST be pre-redacted by the producer (plugin /handoff
// command); zenday consumes them verbatim into the rendered markdown.
package zenday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ProjectConfigReader is the contract zenday consumes for per-project
// opt-out resolution. Production wire-up satisfies via
// projectctx-derived loaders that read [project].zen_day_eod_summary
// from each project's zenswarm.toml; tests substitute fakes.
//
// Default semantics: opt-IN unless explicitly disabled. An unknown
// alias MUST return true so newly-onboarded projects flow into the
// digest without per-project config edits.
type ProjectConfigReader interface {
	IsEODOptedIn(alias string) bool
}

type EODDeps struct {
	MorningDeps

	ProjectConfig ProjectConfigReader
}

// HandoffPostedPayload mirrors eventlog.HandoffPostedEvent's wire shape
// locally so zenday does not import eventlog (avoids the circular-
// import concern with Plan 5; keeps inv-zen-031 boundary discipline as
// zenday → eventlog only via the EventReader interface).
//
// JSON tags MUST match eventlog.HandoffPostedEvent exactly so the
// payload bytes the daemon receives via the eventlog Querier decode
// cleanly into this local struct.
type HandoffPostedPayload struct {
	ProjectID string `json:"project_id"`

	ProjectAlias string `json:"project_alias"`

	Timestamp time.Time `json:"timestamp"`

	Summary string `json:"summary"`

	RecentCommits []string `json:"recent_commits"`

	AutonomousState string `json:"autonomous_state"`

	Blockers []string `json:"blockers"`

	NextSession string `json:"next_session_action"`
}

// EODDigestReadyPayload mirrors eventlog.EODDigestReadyEvent's wire
// shape locally. JSON tags MUST match the canonical eventlog struct so
// the daemon can decode the emitter bytes into the typed downstream.
type EODDigestReadyPayload struct {
	Date time.Time `json:"date"`

	ProjectCount int `json:"project_count"`

	TotalCostUSD float64 `json:"total_cost_usd"`

	FilePath string `json:"file_path"`
}

func GenerateEODDigest(ctx context.Context, deps EODDeps, force bool) (BriefDoc, error) {
	if err := ctx.Err(); err != nil {
		return BriefDoc{}, fmt.Errorf("%w: %v", ErrCollectCancelled, err)
	}

	now := deps.Clock.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := today.Add(24 * time.Hour)
	path := deps.Paths.EODDigestPath(today)

	if !force {
		if _, err := os.Stat(path); err == nil {
			return BriefDoc{}, fmt.Errorf("%w: %s", ErrAlreadyGenerated, path)
		}
	}

	records, err := deps.Eventlog.QueryByType(ctx, "HandoffPosted", today, endOfDay)
	if err != nil {
		return BriefDoc{}, fmt.Errorf("eod handoff query: %w", err)
	}

	byAlias := make(map[string]HandoffPostedPayload)
	for _, r := range records {
		var hp HandoffPostedPayload
		if err := json.Unmarshal(r.PayloadJSON, &hp); err != nil {
			continue
		}
		if existing, ok := byAlias[hp.ProjectAlias]; ok && !hp.Timestamp.After(existing.Timestamp) {
			continue
		}
		byAlias[hp.ProjectAlias] = hp
	}

	snaps, err := deps.Autonomy.Snapshot(ctx)
	if err != nil {
		snaps = nil
	}

	knownAliases := make(map[string]struct{}, len(byAlias)+len(snaps))
	for alias := range byAlias {
		knownAliases[alias] = struct{}{}
	}
	for _, s := range snaps {
		knownAliases[s.ProjectAlias] = struct{}{}
	}

	aliases := make([]string, 0, len(knownAliases))
	for a := range knownAliases {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)

	sections := make([]ProjectStatusSection, 0, len(aliases))
	for _, alias := range aliases {
		if !deps.ProjectConfig.IsEODOptedIn(alias) {
			continue
		}
		hp, hasHandoff := byAlias[alias]
		if hasHandoff {
			sections = append(sections, ProjectStatusSection{
				Alias:           alias,
				AutonomousState: hp.AutonomousState,
				HandoffSummary:  hp.Summary,
				Tomorrow:        hp.NextSession,
				Blockers:        hp.Blockers,
			})
		} else {
			sections = append(sections, ProjectStatusSection{
				Alias:           alias,
				AutonomousState: "manual",
			})
		}
	}

	totalCost := 0.0
	if statuses, err := deps.Cost.SpendByProject(ctx, today, endOfDay); err == nil {
		for _, c := range statuses {
			totalCost += c.SpendUSD
		}
	}

	doc := BriefDoc{
		Date:             today,
		Type:             BriefTypeEOD,
		PerProjectStatus: sections,
		CostWatchUSD:     totalCost,
	}

	body := Render(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return BriefDoc{}, fmt.Errorf("mkdir archive dir: %w", err)
	}
	if err := deps.WriteFile(path, []byte(body), 0644); err != nil {
		return BriefDoc{}, fmt.Errorf("write archive: %w", err)
	}

	payload := EODDigestReadyPayload{
		Date:         today,
		ProjectCount: len(sections),
		TotalCostUSD: totalCost,
		FilePath:     path,
	}
	_ = emitEODReady(ctx, deps.Emitter, payload)

	return doc, nil
}

func emitEODReady(ctx context.Context, emitter EventEmitter, payload EODDigestReadyPayload) error {
	if emitter == nil {
		return fmt.Errorf("zenday: nil EventEmitter")
	}
	raw, _ := json.Marshal(payload)
	return emitter.Emit(ctx, "EODDigestReady", raw)
}
