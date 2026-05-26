//go:build chaos

// DST regression-seed replay tests (Plan 15 Phase F sub-task F-5).
//
// The single tracked regression-seed manifest lives at
// tests/chaos/dst/seeds/regression/seeds.txt. Initially empty; each
// new entry MUST also be reproducible via the harness — the test
// below asserts that on every seed in the manifest.

package dst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegressionSeedsManifestLoads pins the manifest file's syntactic
// validity. An empty manifest is fine (initial state); a malformed
// entry MUST fail loud here, not silently truncate replay coverage.
func TestRegressionSeedsManifestLoads(t *testing.T) {
	path := filepath.Join("seeds", "regression", "seeds.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("manifest open: %v", err)
	}
	defer func() { _ = f.Close() }()
	recs, err := ParseRegressionSeeds(f)
	if err != nil {
		t.Fatalf("manifest parse: %v", err)
	}

	if recs == nil {

		t.Log("regression seeds: empty manifest (no DST-found bugs yet)")
	}
	for _, r := range recs {
		if r.Steps <= 0 {
			t.Errorf("seed %d: steps=%d must be > 0", r.Seed, r.Steps)
		}
		if err := r.Mix.Validate(); err != nil {
			t.Errorf("seed %d: mix invalid: %v", r.Seed, err)
		}
	}
}

func TestRegressionSeedsReplayAll(t *testing.T) {
	path := filepath.Join("seeds", "regression", "seeds.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("manifest open: %v", err)
	}
	defer func() { _ = f.Close() }()
	recs, err := ParseRegressionSeeds(f)
	if err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	for _, rec := range recs {
		rec := rec
		name := rec.BugID
		if name == "" {
			name = "seed_" + intStr(rec.Seed)
		}
		t.Run(name, func(t *testing.T) {
			cfg := RunConfig{
				Seed:    rec.Seed,
				Mix:     rec.Mix,
				Steps:   rec.Steps,
				SkipBub: true,
			}
			if len(rec.Stream) > 0 {
				if err := Replay(t, cfg, NewNoopSUT(), rec.Stream); err != nil {
					t.Fatalf("stream-pinned replay diverged: %v", err)
				}
			} else {
				if _, err := Run(t, cfg, NewNoopSUT()); err != nil {
					t.Fatalf("seed replay errored: %v", err)
				}
			}
		})
	}
}

func TestParseRegressionLineRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		line string
		want RegressionRecord
	}{
		{
			"minimal",
			"bug=demo seed=42 steps=10 mix=sleep:1,yield:1,inject:1,recover:1 max_sleep=10ms",
			RegressionRecord{BugID: "demo", Seed: 42, Steps: 10, Mix: Mix{Sleep: 1, Yield: 1, Inject: 1, Recover: 1, MaxSleep: 10_000_000}},
		},
		{
			"sleep_only_mix",
			"bug=sleep_only seed=7 steps=5 mix=sleep:1 max_sleep=1ms",
			RegressionRecord{BugID: "sleep_only", Seed: 7, Steps: 5, Mix: Mix{Sleep: 1, MaxSleep: 1_000_000}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseRegressionLine(c.line)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.BugID != c.want.BugID || got.Seed != c.want.Seed || got.Steps != c.want.Steps {
				t.Errorf("header drift: got %+v, want %+v", got, c.want)
			}
			if got.Mix.Sleep != c.want.Mix.Sleep || got.Mix.Yield != c.want.Mix.Yield ||
				got.Mix.Inject != c.want.Mix.Inject || got.Mix.Recover != c.want.Mix.Recover ||
				got.Mix.MaxSleep != c.want.Mix.MaxSleep {
				t.Errorf("mix drift: got %+v, want %+v", got.Mix, c.want.Mix)
			}
		})
	}
}

func TestParseRegressionLineRejectsUnknownToken(t *testing.T) {
	_, err := ParseRegressionLine("seed=1 steps=1 mix=sleep:1 max_sleep=1ms whoops=42")
	if err == nil {
		t.Fatal("expected error on unknown token; got nil")
	}
	if !strings.Contains(err.Error(), "unknown token") {
		t.Errorf("err = %v, want 'unknown token' message", err)
	}
}

func TestParseRegressionLineSkipsCommentsAndBlanks(t *testing.T) {
	for _, line := range []string{"", "   ", "# a comment", "#   bug=foo seed=1"} {
		rec, err := ParseRegressionLine(line)
		if err != nil {
			t.Errorf("line %q: unexpected err %v", line, err)
		}
		if rec.Seed != 0 || rec.Steps != 0 || rec.BugID != "" {
			t.Errorf("line %q: expected zero record, got %+v", line, rec)
		}
	}
}

func TestParseRegressionSeedsCarriesStreamPin(t *testing.T) {
	mix := DefaultMix()
	sched := NewScheduler(11, mix)
	stream := sched.Stream(5)
	formatted := FormatStream(stream)
	line := "bug=pin seed=11 steps=5 mix=sleep:1,yield:1,inject:1,recover:1 max_sleep=100ms stream=" + formatted
	rec, err := ParseRegressionLine(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rec.Stream) != len(stream) {
		t.Fatalf("stream length drift: %d vs %d", len(rec.Stream), len(stream))
	}
	for i := range stream {
		if rec.Stream[i] != stream[i] {
			t.Errorf("stream[%d] = %s, want %s", i, rec.Stream[i], stream[i])
		}
	}

	cfg := RunConfig{
		Seed:    rec.Seed,
		Mix:     rec.Mix,
		Steps:   rec.Steps,
		SkipBub: true,
	}
	if err := Replay(t, cfg, NewNoopSUT(), rec.Stream); err != nil {
		t.Fatalf("stream-pinned replay diverged: %v", err)
	}
}

func intStr(s Seed) string {
	const digits = "0123456789"
	if s == 0 {
		return "0"
	}
	neg := s < 0
	if neg {
		s = -s
	}
	var buf [20]byte
	i := len(buf)
	for s > 0 {
		i--
		buf[i] = digits[s%10]
		s /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
