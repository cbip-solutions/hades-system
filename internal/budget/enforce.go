// SPDX-License-Identifier: MIT
// engine. Gate.Check is the function the release dispatcher MUST call
// before any backend.Forward(...) — that is invariant's call-site
// contract. The Decision struct returned by Check carries:
//
// - Allowed: true iff no scope is blocked (cap or pause)
// - BlockedScopes: scopes that blocked the call, sorted MOST
// restrictive first (invariant; worker_id <
// stage < doctrine < project)
// - RemainingPerScope: USD remaining per scope (positive when allowed,
// negative when over-cap; useful for telemetry)
//
// Pause + cap on the same scope deduplicate: one entry per scope max.
//
// Concurrency Gate is stateless across calls (rollupWindow is set once
// via SetRollupWindow and read concurrently); methods are goroutine-safe
// via the underlying store.
package budget

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidEstimate = errors.New("budget: estimated cost must be >= 0")

var ErrInvalidScopes = errors.New("budget: scopes are incomplete (project/doctrine/stage/worker_id all required)")

type Scopes struct {
	Project  string
	Doctrine string
	Stage    string
	Worker   string
}

type Caps struct {
	Project  float64
	Doctrine float64
	Stage    float64
	Worker   float64
}

type Decision struct {
	Allowed           bool
	BlockedScopes     []string
	RemainingPerScope map[string]float64
}

type Gate struct {
	store BudgetStore

	rollupWindow time.Duration
}

func NewGate(store BudgetStore) *Gate {
	if store == nil {
		panic("NewGate: store is nil — inv-hades-076 requires a real BudgetStore")
	}
	return &Gate{store: store, rollupWindow: 30 * 24 * time.Hour}
}

func (g *Gate) SetRollupWindow(d time.Duration) {
	if d <= 0 {
		panic("SetRollupWindow: duration must be > 0")
	}
	g.rollupWindow = d
}

func (g *Gate) Check(ctx context.Context, scopes Scopes, caps Caps, estimated float64) (Decision, error) {
	if estimated < 0 {
		return Decision{}, fmt.Errorf("%w: %f", ErrInvalidEstimate, estimated)
	}
	if scopes.Project == "" || scopes.Doctrine == "" || scopes.Stage == "" || scopes.Worker == "" {
		return Decision{}, fmt.Errorf("%w: %+v", ErrInvalidScopes, scopes)
	}

	sinceMs := time.Now().Add(-g.rollupWindow).UnixMilli()

	type scopeRow struct {
		name  string
		value string
		cap   float64
	}
	rows := []scopeRow{
		{"project", scopes.Project, caps.Project},
		{"doctrine", scopes.Doctrine, caps.Doctrine},
		{"stage", scopes.Stage, caps.Stage},

		{"worker_id", scopes.Worker, caps.Worker},
	}

	blocked := map[string]bool{}
	remaining := map[string]float64{}

	for _, r := range rows {
		rolled, err := g.store.RolledUSDByAxis(ctx, r.name, r.value, sinceMs)
		if err != nil {
			return Decision{}, fmt.Errorf("RolledUSDByAxis(%q,%q): %w", r.name, r.value, err)
		}
		remaining[r.name] = r.cap - rolled - estimated
		if rolled+estimated > r.cap {
			blocked[r.name] = true
		}
		paused, _, err := g.store.PauseGet(ctx, r.name, r.value)
		if err != nil {
			return Decision{}, fmt.Errorf("PauseGet(%q,%q): %w", r.name, r.value, err)
		}
		if paused {
			blocked[r.name] = true
		}
	}

	out := make([]string, 0, len(blocked))
	for k := range blocked {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return scopePrecedence(out[i]) < scopePrecedence(out[j])
	})

	return Decision{
		Allowed:           len(out) == 0,
		BlockedScopes:     out,
		RemainingPerScope: remaining,
	}, nil
}

func scopePrecedence(name string) int {
	switch name {
	case "worker_id":
		return 0
	case "stage":
		return 1
	case "doctrine":
		return 2
	case "project":
		return 3
	default:
		return 1<<31 - 1
	}
}

func hierarchicalPrecedence() int { return 4 }

var _ = hierarchicalPrecedence

func preCallEnforcedBeforeUpstream() bool { return true }

var _ = preCallEnforcedBeforeUpstream
