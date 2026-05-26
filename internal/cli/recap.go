// SPDX-License-Identifier: MIT
// Package cli — recap.go (Plan 7 Phase F Task F-11).
//
// `zen recap --since <duration>` walks past zen day archives in
// `~/.config/zen-swarm/zen-day-*.md` (and `-eod.md`) within the
// supplied duration window and concatenates them into a single
// chronological view. Useful for catching up after a vacation or
// audit lookback per spec §6.1.
//
// Recap is filesystem-only: no daemon dependency, no network, no
// HTTP transport. The command stands on its own so an operator can
// run `zen recap --since 7d` even when the daemon is down (for
// post-incident audits, in particular). This is the same posture
// described in spec lines 3635-3636 ("Lives standalone (no daemon
// dependency) — recap reads filesystem archives directly").
//
// Duration parsing:
//
// Stdlib `time.ParseDuration` does not understand `d` (days) or `w`
// (weeks); the spec advertises `--since 7d` as a working example, so
// the parser is extended on top of stdlib units. Format:
//
//   - `<int>d` → N * 24h (1d, 7d, 30d).
//   - `<int>w` → N * 7 * 24h (1w, 2w).
//   - everything else → delegated to time.ParseDuration unchanged
//     (so `24h`, `90m`, `1h30m`, `45s` continue to work).
//
// Negatives, fractionals, bare numbers, and unknown unit suffixes all
// error — same posture as stdlib, no silent zero-duration fallthrough.
//
// Filename grammar:
//
// Two shapes match the glob `zen-day-*.md`:
//
//   - `zen-day-YYYY-MM-DD.md` (morning brief)
//   - `zen-day-YYYY-MM-DD-eod.md` (EOD digest)
//
// Both are parsed via `time.Parse("2006-01-02", ...)` against the
// `YYYY-MM-DD` slice. Anything else (`zen-day-bogus.md`,
// `zen-day-2026-13-99.md`, `zen-day-archive-foo.md`) is silently
// skipped.
//
// Sort chronological by parsed date primary; for same-date pairs
// morning (isEOD=false) emits before EOD digest (isEOD=true) — note
// raw basename order (`-` < `.`) would emit eod first which is
// operator-confusing. `sort.SliceStable` keeps glob-enumeration order
// for ties beyond date+isEOD (which can't happen given one filename
// shape per (date, isEOD) tuple, but stable is the defensive choice).
// `\n---\n` separator between entries; no leading or trailing
// separator.
//
// Coverage target: 100% on this file. Tests live in recap_test.go.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func parseRecapSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("--since is empty")
	}

	if s[0] == '-' || s[0] == '+' {
		return 0, fmt.Errorf("--since must be positive, got %q", s)
	}

	if n := len(s); n >= 2 {
		last := s[n-1]
		if last == 'd' || last == 'w' {
			head := s[:n-1]
			v, err := strconv.Atoi(head)
			if err != nil {
				return 0, fmt.Errorf("--since: parse %q: %w", s, err)
			}
			if v <= 0 {
				return 0, fmt.Errorf("--since must be positive, got %q", s)
			}
			multiplier := 24 * time.Hour
			if last == 'w' {
				multiplier = 7 * 24 * time.Hour
			}
			return time.Duration(v) * multiplier, nil
		}
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--since: parse %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("--since must be positive, got %q", s)
	}
	return d, nil
}

func NewRecapCmd() *cobra.Command {
	var since string

	cmd := &cobra.Command{
		Use:   "recap",
		Short: "Walk past zen day briefs in chronological order",
		Long: `zen recap --since <duration> concatenates past zen day archives
(~/.config/zen-swarm/zen-day-*.md) within the supplied window into a
single chronological view. Default --since 24h.

Duration grammar:
  - Nd: days (e.g. 1d, 7d, 30d)
  - Nw: weeks (e.g. 1w, 2w)
  - everything else: stdlib time.ParseDuration (e.g. 24h, 90m, 1h30m)

Recap is filesystem-only: no daemon required. Use this for vacation
catch-up, post-incident audits, or sprint retrospectives where you
need the rolled-up sequence of daily briefs without re-running them.`,
		Example: `  # Default 24h window
  zen recap

  # Last week
  zen recap --since 7d

  # Last 2 weeks
  zen recap --since 2w`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dur, err := parseRecapSince(since)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("parse --since: %w", err))
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("home dir: %w", err))
			}
			archiveDir := filepath.Join(home, ".config", "zen-swarm")
			return runRecap(archiveDir, dur, time.Now, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h",
		"duration window (e.g. 7d, 24h, 90m); supports Nd / Nw + stdlib units")
	return cmd
}

type recapEntry struct {
	path  string
	date  time.Time
	isEOD bool
}

func runRecap(archiveDir string, since time.Duration, nowFn func() time.Time, out io.Writer) error {
	pattern := filepath.Join(archiveDir, "zen-day-*.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("glob %q: %w", pattern, err))
	}
	if len(paths) == 0 {
		return nil
	}

	cutoff := nowFn().UTC().Add(-since)
	keep := filterAndParse(paths, cutoff)
	if len(keep) == 0 {
		return nil
	}

	sort.SliceStable(keep, func(i, j int) bool {
		if !keep[i].date.Equal(keep[j].date) {
			return keep[i].date.Before(keep[j].date)
		}
		return !keep[i].isEOD && keep[j].isEOD
	})

	for i, e := range keep {
		body, readErr := os.ReadFile(e.path)
		if readErr != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read %s: %w", e.path, readErr))
		}
		if i > 0 {
			if _, werr := io.WriteString(out, "\n---\n"); werr != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("write separator: %w", werr))
			}
		}
		if _, werr := out.Write(body); werr != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("write %s: %w", e.path, werr))
		}
	}
	return nil
}

func filterAndParse(paths []string, cutoff time.Time) []recapEntry {
	keep := make([]recapEntry, 0, len(paths))
	const prefix = "zen-day-"
	for _, p := range paths {

		base := filepath.Base(p)
		datePart := base[len(prefix):]

		var dateStr string
		var isEOD bool
		switch {
		case len(datePart) == 13 && datePart[10:13] == ".md":
			dateStr = datePart[:10]
		case len(datePart) == 17 && datePart[10:14] == "-eod" && datePart[14:17] == ".md":
			dateStr = datePart[:10]
			isEOD = true
		default:
			continue
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			continue
		}
		keep = append(keep, recapEntry{path: p, date: t, isEOD: isEOD})
	}
	return keep
}
