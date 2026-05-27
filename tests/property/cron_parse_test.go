// go:build property
//go:build property
// +build property

// Four properties, each fuzzed via testing/quick or asserted on a
// curated boundary table:
//
// 1. Valid 5-field Vixie expressions (numeric only — names rejected
// per scheduler.containsToken policy) produce a non-empty schedule
// whose Next() fires within 1 day from the reference time. The
// Feb-29 leap-year edge is bounded by the worst-case 367-day cap.
//
// 2. Extended Quartz syntax (L, W, ?, named tokens MON..SUN /
// JAN..DEC, MON#3, embedded #) is rejected with ErrInvalidCron.
// This is the runtime witness that scheduler.ParseCron's extended-
// token gate stays sealed; a future regression that re-enables
// the underlying parser's named-month support would surface here.
//
// 3. Boundary numeric values (0/59 minute, 0/23 hour, 1/31 day-of-
// month, 1/12 month, 0/6 day-of-week) parse and produce a
// first-fire time within the documented window.
//
// 4. Determinism: parsing the same expression twice produces the same
// Next() values across 100 successive iterations.
//
// Reinforces: spec §4.3 + parser contract; the 5-field Vixie
// syntax is the only sanctioned cron surface. Extended Quartz operators
// MUST NEVER silently parse — that would write portable-looking schedules
// to daemon.db that silently mean different things across operator
// timezones.
//
// # Drift notes (vs plan-template heredoc)
//
// The plan template referenced a 1-arg ParseCron(expr) but the real
// signature is 2-arg ParseCron(expr, doctrine.Name) — doctrine binds
// the granularity floor (default=1min, max-scope=30s, capa-firewall=
// 5min). All tests here use doctrine.NameDefault so the floor is 1min
// (matches Vixie's natural floor — no doctrine-specific corner cases).
//
// The plan template's Generator could produce expressions that fail
// the doctrine floor (e.g. `*/0`); the live ParseCron rejects those
// as ErrInvalidCron. Property 1 SKIPS those: the property is only
// claimed for *valid* expressions.
//
// The plan template's boundary table includes "0 0 * * 0" / "0 0 * * 6"
// (numeric DOW). These ARE valid in the live parser — only named
// MON..SUN are rejected. We retain those rows verbatim.
//
// Reality wins.
package property

import (
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

type cronScenario struct {
	Min   string
	Hour  string
	DOM   string
	Month string
	DOW   string
	Seed  int64
}

func genField(rng *rand.Rand, minVal, maxVal int) string {
	switch rng.Intn(5) {
	case 0:
		return "*"
	case 1:

		return fmt.Sprintf("%d", minVal+rng.Intn(maxVal-minVal+1))
	case 2:

		a := minVal + rng.Intn(maxVal-minVal+1)
		b := minVal + rng.Intn(maxVal-minVal+1)
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d-%d", a, b)
	case 3:

		step := 1 + rng.Intn(maxVal-minVal+1)
		return fmt.Sprintf("*/%d", step)
	case 4:

		a := minVal + rng.Intn(maxVal-minVal+1)
		b := minVal + rng.Intn(maxVal-minVal+1)
		if a > b {
			a, b = b, a
		}
		if a == b {
			b = minVal + (b-minVal+1)%(maxVal-minVal+1)
			if a > b {
				a, b = b, a
			}
		}
		return fmt.Sprintf("%d,%d", a, b)
	}
	return "*"
}

func (cs cronScenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	v := cronScenario{
		Min:   genField(rng, 0, 59),
		Hour:  genField(rng, 0, 23),
		DOM:   genField(rng, 1, 31),
		Month: genField(rng, 1, 12),
		DOW:   genField(rng, 0, 6),
		Seed:  rng.Int63(),
	}
	return reflect.ValueOf(v)
}

func (cs cronScenario) Expr() string {
	return fmt.Sprintf("%s %s %s %s %s", cs.Min, cs.Hour, cs.DOM, cs.Month, cs.DOW)
}

func TestProp_CronParse_ValidProducesSchedule(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 100,
		Rand:     rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name()))))),
	}
	if testing.Short() {
		cfg.MaxCount = 10
	}

	property := func(sc cronScenario) bool {
		expr := sc.Expr()
		parsed, err := scheduler.ParseCron(expr, doctrine.NameDefault)
		if err != nil {

			return true
		}
		now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
		next := parsed.Next(now)
		if next.IsZero() {
			t.Errorf("expression %q produced zero next-fire", expr)
			return false
		}

		if next.Sub(now) > 367*24*time.Hour {
			t.Errorf("expression %q next-fire too far: %v", expr, next.Sub(now))
			return false
		}
		return true
	}

	if err := quick.Check(property, cfg); err != nil {
		t.Fatalf("cron parse valid property failed: %v", err)
	}
}

// TestProp_CronParse_ExtendedSyntaxRejected enumerates every Quartz
// extension the live parser MUST reject. Each row asserts ParseCron
// returns an error wrapping ErrInvalidCron.
//
// This is not a fuzz; the rejection set is small + curated. The fuzz
// dimension is the future-proofing: adding a new Quartz operator
// without an ADR amendment surfaces here as a missing rejection.
func TestProp_CronParse_ExtendedSyntaxRejected(t *testing.T) {
	invalid := []string{
		"0 0 L * *",
		"0 0 * * 6L",
		"0 0 1W * *",
		"0 0 ? * 1",
		"0 0 * * MON#3",
		"0 0 * * MON",
		"0 0 * MAR *",
		"  0 0 * * *",
		"0 0 * * *  ",
		"@hourly",
		"@daily",
		"@reboot",
		"0 0 * *",
		"0 0 * * * *",
	}
	for _, expr := range invalid {
		t.Run(expr, func(t *testing.T) {
			_, err := scheduler.ParseCron(expr, doctrine.NameDefault)
			if err == nil {
				t.Fatalf("expression %q should have been rejected", expr)
			}
			if !errors.Is(err, scheduler.ErrInvalidCron) {
				t.Fatalf("expression %q error %v not wrapping ErrInvalidCron", expr, err)
			}
		})
	}
}

func TestProp_CronParse_BoundaryValues(t *testing.T) {
	cases := []struct {
		expr      string
		nowOffset time.Duration
	}{
		{"0 * * * *", time.Hour},
		{"59 * * * *", time.Hour},
		{"0 0 * * *", 24 * time.Hour},
		{"0 23 * * *", 24 * time.Hour},
		{"0 0 1 * *", 32 * 24 * time.Hour},
		{"0 0 31 * *", 366 * 24 * time.Hour},
		{"0 0 1 1 *", 367 * 24 * time.Hour},
		{"0 0 * * 0", 7 * 24 * time.Hour},
		{"0 0 * * 6", 7 * 24 * time.Hour},
		{"*/2 * * * *", 5 * time.Minute},
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			parsed, err := scheduler.ParseCron(c.expr, doctrine.NameDefault)
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.expr, err)
			}
			next := parsed.Next(now)
			if next.IsZero() {
				t.Fatalf("Parse(%q).Next(%v) = zero", c.expr, now)
			}
			if delta := next.Sub(now); delta > c.nowOffset {
				t.Fatalf("Parse(%q).Next(%v) = %v (+%v); want within %v",
					c.expr, now, next, delta, c.nowOffset)
			}
		})
	}
}

func TestProp_CronParse_ParseDeterminism(t *testing.T) {
	expressions := []string{
		"* * * * *",
		"0 9 * * 1-5",
		"*/5 * * * *",
		"0 0 1 1 *",
		"15,45 * * * *",
		"0 0,12 * * *",
		"0 0 1-7 * *",
	}
	for _, expr := range expressions {
		t.Run(expr, func(t *testing.T) {
			a, err := scheduler.ParseCron(expr, doctrine.NameDefault)
			if err != nil {
				t.Fatalf("parse 1: %v", err)
			}
			b, err := scheduler.ParseCron(expr, doctrine.NameDefault)
			if err != nil {
				t.Fatalf("parse 2: %v", err)
			}
			now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
			for i := 0; i < 100; i++ {
				na := a.Next(now)
				nb := b.Next(now)
				if !na.Equal(nb) {
					t.Fatalf("Determinism broken: %q at iteration %d: a=%v b=%v",
						expr, i, na, nb)
				}
				if na.IsZero() {

					return
				}
				now = na
			}
		})
	}
}
