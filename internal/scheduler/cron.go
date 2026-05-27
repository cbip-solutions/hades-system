// SPDX-License-Identifier: MIT
package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type CronExpr struct {
	Raw      string
	Schedule cron.Schedule
}

const (
	DoctrineDefault      = doctrine.NameDefault
	DoctrineMaxScope     = doctrine.NameMaxScope
	DoctrineCapaFirewall = doctrine.NameCapaFirewall
)

func granularityFloor(d doctrine.Name) time.Duration {
	switch d {
	case doctrine.NameMaxScope:
		return 30 * time.Second
	case doctrine.NameCapaFirewall:
		return 5 * time.Minute
	default:
		return 1 * time.Minute
	}
}

var extendedTokens = []string{
	"L", "W", "?",
	"JAN", "FEB", "MAR", "APR", "MAY", "JUN",
	"JUL", "AUG", "SEP", "OCT", "NOV", "DEC",
	"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN",
}

// ParseCron parses a 5-field vixie cron expression (m h dom mon dow)
// and returns a *CronExpr wrapping robfig/cron/v3's Schedule.
//
// REJECTS (each surfaced as an ErrInvalidCron-wrapping error):
//
// - 6-field forms (with seconds).
// - Descriptors (@hourly, @daily, @reboot, @yearly, @monthly,
// @weekly, @midnight, @annually).
// - Extended syntax: L, W, ?, named months (JAN..DEC), named days
// (MON..SUN). Reason: portability across operator timezones.
// - Leading/trailing whitespace, embedded comments.
// - Empty / out-of-range numeric fields (delegated to robfig/cron/v3).
//
// ENFORCES granularity floor per doctrine (see granularityFloor):
//
// default → 1min
// max-scope → 30s
// capa-firewall → 5min
//
// All errors wrap ErrInvalidCron; callers MUST use errors.Is to
// discriminate.
//
// Why a wrapper, not direct use of robfig/cron/v3:
//
// 1. Boundary control — only this file touches the library; tests can
// stub it if needed without touching every call site.
// 2. Doctrine override hooks — the granularity-floor gate rides on
// the wrapper, not on the library.
// 3. Extended-syntax rejection — the library accepts named tokens by
// default; we reject them so daemon.db only stores portable cron.
// 4. Deterministic test fixtures — the wrapper exposes
// CronExpr.Next which always returns UTC, regardless of the
// `after` argument's location.
func ParseCron(expr string, d doctrine.Name) (*CronExpr, error) {
	if expr == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidCron)
	}

	if expr != strings.TrimSpace(expr) {
		return nil, fmt.Errorf("%w: leading/trailing whitespace not allowed (extended)", ErrInvalidCron)
	}

	if strings.Contains(expr, "#") {
		return nil, fmt.Errorf("%w: embedded comments not allowed (extended)", ErrInvalidCron)
	}

	if strings.HasPrefix(expr, "@") {
		return nil, fmt.Errorf("%w: descriptor %q not allowed (descriptor)", ErrInvalidCron, expr)
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: expected 5-field cron, got %d fields", ErrInvalidCron, len(fields))
	}

	upper := strings.ToUpper(expr)
	for _, bad := range extendedTokens {
		if containsToken(upper, bad) {
			return nil, fmt.Errorf("%w: extended token %q not allowed", ErrInvalidCron, bad)
		}
	}

	for _, suffix := range []string{"L", "W"} {
		if containsAttachedExtension(upper, suffix) {
			return nil, fmt.Errorf("%w: extended modifier %q (Quartz suffix on digit) not allowed", ErrInvalidCron, suffix)
		}
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCron, err)
	}

	floor := granularityFloor(d)
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := sched.Next(t0)
	t2 := sched.Next(t1)
	if delta := t2.Sub(t1); delta < floor {
		return nil, fmt.Errorf("%w: tick interval %v < doctrine floor %v", ErrInvalidCron, delta, floor)
	}
	return &CronExpr{Raw: expr, Schedule: sched}, nil
}

func containsToken(s, tok string) bool {
	idx := 0
	for {
		off := strings.Index(s[idx:], tok)
		if off == -1 {
			return false
		}
		pos := idx + off

		leftOK := pos == 0
		if !leftOK {
			leftOK = isCronDelim(s[pos-1])
		}

		end := pos + len(tok)
		rightOK := end == len(s)
		if !rightOK {
			rightOK = isCronDelim(s[end])
		}
		if leftOK && rightOK {
			return true
		}
		idx = pos + 1
		if idx >= len(s) {
			return false
		}
	}
}

func isCronDelim(c byte) bool {
	return c == ' ' || c == ',' || c == '/' || c == '-'
}

func containsAttachedExtension(s, ext string) bool {
	for i := 0; i+len(ext) <= len(s); i++ {
		if s[i:i+len(ext)] != ext {
			continue
		}

		if i == 0 {
			continue
		}
		if c := s[i-1]; c < '0' || c > '9' {
			continue
		}

		end := i + len(ext)
		if end == len(s) {
			return true
		}
		if isCronDelim(s[end]) {
			return true
		}
	}
	return false
}

func (e *CronExpr) Next(after time.Time) time.Time {
	if e == nil || e.Schedule == nil {
		return time.Time{}
	}
	return e.Schedule.Next(after.UTC()).UTC()
}

func (e *CronExpr) String() string {
	if e == nil {
		return ""
	}
	return e.Raw
}
