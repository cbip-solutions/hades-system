//go:build chaos

// SPDX-License-Identifier: MIT

package dst

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

type Seed = int64

type Action int

const (
	ActionUnknown Action = iota

	ActionSleep

	ActionYield

	ActionInjectFailure
	// ActionRecover clears the most-recently-injected failure. Tests
	// MUST pair recover with inject so the system observes both
	// failure-onset and recovery — DST stress lives in the recover
	// path, not the inject path.
	ActionRecover
)

func (a Action) String() string {
	switch a {
	case ActionSleep:
		return "sleep"
	case ActionYield:
		return "yield"
	case ActionInjectFailure:
		return "inject"
	case ActionRecover:
		return "recover"
	default:
		return "unknown"
	}
}

// Mix declares the action-frequency contract for a DST run. Weights
// MUST be non-negative; the harness normalises them to a discrete CDF
// before each step. MaxSleep caps the synctest fake-clock advance per
// ActionSleep so even unlucky seeds keep wall-clock bounded for CI.
//
// DefaultMix returns a balanced contract used by the bench tests +
// the harness self-test; production scenarios layer their own Mix
// reflecting the system under test's expected fault cadence (e.g.,
// audit-chain chaos has a higher Inject weight than a happy-path
// soak).
type Mix struct {
	Sleep    int
	Yield    int
	Inject   int
	Recover  int
	MaxSleep time.Duration
}

func (m Mix) Validate() error {
	if m.Sleep < 0 || m.Yield < 0 || m.Inject < 0 || m.Recover < 0 {
		return fmt.Errorf("dst.Mix: weights must be non-negative; got %+v", m)
	}
	if m.Sleep+m.Yield+m.Inject+m.Recover == 0 {
		return fmt.Errorf("dst.Mix: total weight must be > 0; got %+v", m)
	}
	if m.MaxSleep < 0 {
		return fmt.Errorf("dst.Mix: MaxSleep must be ≥ 0; got %s", m.MaxSleep)
	}
	return nil
}

func DefaultMix() Mix {
	return Mix{
		Sleep:    1,
		Yield:    1,
		Inject:   1,
		Recover:  1,
		MaxSleep: 100 * time.Millisecond,
	}
}

type Scheduler struct {
	seed Seed
	mix  Mix
	rng  *rand.Rand
	cdf  []int
}

func NewScheduler(seed Seed, mix Mix) *Scheduler {
	if err := mix.Validate(); err != nil {
		panic("dst.NewScheduler: " + err.Error())
	}
	src := rand.NewPCG(uint64(seed), uint64(seed)^0x9E3779B97F4A7C15)
	r := rand.New(src)
	cdf := []int{
		mix.Sleep,
		mix.Sleep + mix.Yield,
		mix.Sleep + mix.Yield + mix.Inject,
		mix.Sleep + mix.Yield + mix.Inject + mix.Recover,
	}
	return &Scheduler{seed: seed, mix: mix, rng: r, cdf: cdf}
}

func (s *Scheduler) Seed() Seed { return s.seed }

func (s *Scheduler) Next() Action {
	total := s.cdf[3]
	pick := s.rng.IntN(total)
	switch {
	case pick < s.cdf[0]:
		return ActionSleep
	case pick < s.cdf[1]:
		return ActionYield
	case pick < s.cdf[2]:
		return ActionInjectFailure
	default:
		return ActionRecover
	}
}

func (s *Scheduler) SleepFor() time.Duration {
	if s.mix.MaxSleep == 0 {
		return 0
	}
	return time.Duration(s.rng.Int64N(int64(s.mix.MaxSleep)))
}

func (s *Scheduler) Stream(n int) []Action {
	out := make([]Action, n)
	for i := 0; i < n; i++ {
		out[i] = s.Next()
	}
	return out
}

func DeriveSeed(parent Seed, label string) Seed {

	h := uint64(1469598103934665603)
	const prime = uint64(1099511628211)
	for _, b := range []byte(label) {
		h ^= uint64(b)
		h *= prime
	}
	return Seed(uint64(parent) ^ h)
}

type ActionHistogram map[Action]int

func AllActions() []Action {
	return []Action{ActionSleep, ActionYield, ActionInjectFailure, ActionRecover}
}

func Histogram(stream []Action) ActionHistogram {
	h := make(ActionHistogram, len(AllActions()))
	for _, a := range stream {
		h[a]++
	}
	return h
}

func FormatStream(stream []Action) string {
	parts := make([]string, len(stream))
	for i, a := range stream {
		parts[i] = a.String()
	}
	return strings.Join(parts, ",")
}

func ParseStream(s string) ([]Action, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]Action, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		switch p {
		case "sleep":
			out[i] = ActionSleep
		case "yield":
			out[i] = ActionYield
		case "inject":
			out[i] = ActionInjectFailure
		case "recover":
			out[i] = ActionRecover
		default:
			return nil, fmt.Errorf("ParseStream: unknown action %q at index %d", p, i)
		}
	}
	return out, nil
}

type FailureInjector struct {
	mu       sync.Mutex
	active   bool
	tag      string
	onActive func()
	onClear  func()
}

func NewFailureInjector(tag string, onActive, onClear func()) *FailureInjector {
	return &FailureInjector{tag: tag, onActive: onActive, onClear: onClear}
}

func (f *FailureInjector) Activate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active {
		return
	}
	f.active = true
	if f.onActive != nil {
		f.onActive()
	}
}

func (f *FailureInjector) Recover() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active {
		return
	}
	f.active = false
	if f.onClear != nil {
		f.onClear()
	}
}

func (f *FailureInjector) Active() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

func (f *FailureInjector) Tag() string { return f.tag }

func SeedSummary(seed Seed, mix Mix) string {
	return fmt.Sprintf("seed=%d mix={sleep:%d yield:%d inject:%d recover:%d max_sleep=%s}",
		seed, mix.Sleep, mix.Yield, mix.Inject, mix.Recover, mix.MaxSleep)
}

func SortedSeeds(seeds []Seed) []Seed {
	out := append([]Seed(nil), seeds...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
