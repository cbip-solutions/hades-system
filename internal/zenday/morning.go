// SPDX-License-Identifier: MIT
// Package zenday — morning-brief idempotent composer.
//
// GenerateMorningBrief is the user-facing entry point invoked by both
// the cron firing and the operator-
// pull (`bin/zen day`). Per spec §1 Q13 C: idempotent — skips regen
// when today's archive `~/.config/zen-swarm/zen-day-YYYY-MM-DD.md`
// already exists, unless `force == true`.
//
// # Pipeline
//
// 1. Compute today UTC midnight from deps.Clock.Now().
// 2. Resolve archive path via deps.Paths.MorningBriefPath.
// 3. If !force and os.Stat(path) succeeds → return ErrAlreadyGenerated.
// 4. Collect(ctx, deps.CollectDeps, since=now-24h, eod=false).
// 5. SortByLeverage + truncate to MaxBriefItems.
// 6. Build BriefDoc{Type: BriefTypeMorning, Date: today, …}.
// 7. Render → MkdirAll(parent) → WriteFile(path).
// 8. Emit MorningBriefReadyEvent (best-effort: emit-failure does NOT
// void the brief; the file is on disk regardless).
//
// invariant (7-cap) + invariant (canonical sort) are enforced by
// Render's defense-in-depth Layer 2 panics; the truncation step here is
// Layer 1.
package zenday

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EventEmitter is the contract zenday.GenerateMorningBrief +
// GenerateEODDigest invoke after successful Render + WriteFile.
// Production wiring satisfies eventlog.Recorder.
//
// kind is the canonical EventType.String() value (e.g.
// "MorningBriefReady"); payload is the JSON-encoded typed event body.
//
// Implementations MUST be safe for concurrent invocation: zenday Collect
// fans out across goroutines, and post-Collect emit may overlap with
// other producers.
type EventEmitter interface {
	Emit(ctx context.Context, kind string, payload []byte) error
}

type PathResolver interface {
	MorningBriefPath(date time.Time) string
	EODDigestPath(date time.Time) string
}

type Clock interface {
	Now() time.Time
}

type MorningDeps struct {
	CollectDeps

	Clock Clock

	Paths PathResolver

	Emitter EventEmitter

	WriteFile func(path string, data []byte, perm os.FileMode) error
}

// MorningBriefReadyPayload mirrors eventlog.MorningBriefReadyEvent's
// wire shape locally so zenday does not import eventlog (avoids the
// circular-import concern when is composed before lands;
// keeps invariant boundary discipline as zenday → eventlog only via
// the EventEmitter interface).
//
// JSON tags MUST match eventlog.MorningBriefReadyEvent exactly so the
// payload bytes the daemon receives via Emit decode cleanly into the
// canonical struct downstream.
type MorningBriefReadyPayload struct {
	Date time.Time `json:"date"`

	ItemCount int `json:"item_count"`

	ProjectCount int `json:"project_count"`

	FilePath string `json:"file_path"`
}

// GenerateMorningBrief composes today's morning brief, writes it to the
// archive path (idempotent unless force), and emits
// MorningBriefReadyEvent on success.
//
// # Returns
//
// - doc — the rendered BriefDoc value (post-truncation Items + final
// TruncatedCount). Zero-value on early returns.
// - error — ErrAlreadyGenerated when today's archive exists and
// !force; ErrCollectCancelled wrapped from Collect on ctx cancel;
// wrapped os errors from MkdirAll / WriteFile. Emit failure is
// silent (the file is written regardless).
//
// Note on partial-tolerance: the morning path treats every read-leg
// failure as a soft warning (legErrors slice from Collect). When every
// read leg fails the brief is still produced as zero-items content
// (the eventlog leg is a no-op-success on eod=false per
// collect.go semantics, so successes >=1 always holds for morning).
// Operators see an empty-brief signal rather than a hard failure —
// the brief itself is the outage indicator.
func GenerateMorningBrief(ctx context.Context, deps MorningDeps, force bool) (BriefDoc, error) {
	now := deps.Clock.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	path := deps.Paths.MorningBriefPath(today)

	if !force {
		if _, err := os.Stat(path); err == nil {
			return BriefDoc{}, fmt.Errorf("%w: %s", ErrAlreadyGenerated, path)
		}
	}

	items, _, err := Collect(ctx, deps.CollectDeps, now.Add(-24*time.Hour), false)
	if err != nil {
		return BriefDoc{}, fmt.Errorf("morning brief collect: %w", err)
	}
	SortByLeverage(items)
	items, truncated := truncate(items, MaxBriefItems)

	doc := BriefDoc{
		Date:           today,
		Type:           BriefTypeMorning,
		Items:          items,
		TruncatedCount: truncated,
	}

	body := Render(doc)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return BriefDoc{}, fmt.Errorf("mkdir archive dir: %w", err)
	}
	if err := deps.WriteFile(path, []byte(body), 0644); err != nil {
		return BriefDoc{}, fmt.Errorf("write archive: %w", err)
	}

	payload := MorningBriefReadyPayload{
		Date:         today,
		ItemCount:    len(items),
		ProjectCount: distinctProjectCount(items),
		FilePath:     path,
	}

	_ = emitMorningReady(ctx, deps.Emitter, payload)

	return doc, nil
}

func truncate(items []BriefItem, max int) ([]BriefItem, int) {
	if len(items) <= max {
		return items, 0
	}
	return items[:max], len(items) - max
}

func distinctProjectCount(items []BriefItem) int {
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it.Project == "" {
			continue
		}
		seen[it.Project] = struct{}{}
	}
	return len(seen)
}

func emitMorningReady(ctx context.Context, emitter EventEmitter, payload MorningBriefReadyPayload) error {
	if emitter == nil {
		return fmt.Errorf("zenday: nil EventEmitter")
	}
	raw, _ := json.Marshal(payload)
	return emitter.Emit(ctx, "MorningBriefReady", raw)
}
