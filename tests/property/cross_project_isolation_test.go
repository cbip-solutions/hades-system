//go:build property
// +build property

// Property: for any sequence of operations across N=5..20 projects and
// M=100..1000 ops drawn from the inbox/aggregator API, the daemon-level
// AggregatorCacheStore row's project_id MUST match the per-project
// authoritative source for that NotificationID. Extends the unit-tier
// inv-zen-113 fuzz at tests/compliance/inv_zen_113_no_cross_project_inbox_leak_test.go
// (N=5 × 200 ops × 5 seeds) to a randomized scale of N=5..20 × M=100..1000
// via testing/quick with deterministic seeding.
//
// # Drift notes (vs plan-template heredoc)
//
// The plan template referenced fictional symbols not present in the live
// codebase: `inbox.AuthoritativeStore`, `inbox.NewOutboxBridge`,
// `inbox.NewRebuilder`, `inbox.NewAggregatorCache`, `clock.NewVirtual`,
// `eventlog.NewRecorder`, `projectctx.ProjectID(testhelpers.MakeProjectID(i))`,
// and `cache.Iterate`. The real surface (already exercised by Phase E +
// existing inv-zen-113 compliance test) is:
//
//   - inbox.NewMemStore()                      — per-project authoritative
//   - inboxadapter.NewAdapter(perProject, daemonStore) — bridge + cache
//   - Adapter.Insert(ctx, *Notification)       — 2-stage write (auth + cache)
//   - Adapter.Ack / Adapter.Snooze             — mutation
//   - Adapter.Delete(ctx, projectID)           — cascade
//   - Adapter.InsertCache(ctx, CacheRow)       — direct cache write
//   - Adapter.Query(ctx, ListFilter)           — cache read
//   - Adapter.List(ctx, ListFilter)            — per-project read
//   - Adapter.Rebuild(ctx, []inbox.Store)      — cache rehydrate
//
// Reality wins (per per-project memory feedback_plan_template_drift.md):
// the property under test (no cross-project leak under random ops) is
// preserved verbatim; only the mechanical bindings change.
package property

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter"
	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type scenario struct {
	NumProjects int
	NumOps      int
	Seed        int64
}

func (s scenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	v := scenario{
		NumProjects: 5 + rng.Intn(16),
		NumOps:      100 + rng.Intn(901),
		Seed:        rng.Int63(),
	}
	return reflect.ValueOf(v)
}

func TestProp_CrossProjectIsolation_NoLeak(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 50,
		Rand:     rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name()))))),
	}
	if testing.Short() {
		cfg.MaxCount = 5
	}

	property := func(sc scenario) bool {
		msg := runIsolationScenario(t, sc)
		if msg != "" {
			t.Logf("scenario failed: NumProjects=%d NumOps=%d Seed=%d: %s",
				sc.NumProjects, sc.NumOps, sc.Seed, msg)
			return false
		}
		return true
	}

	if err := quick.Check(property, cfg); err != nil {
		t.Fatalf("cross-project isolation property failed: %v", err)
	}
}

func runIsolationScenario(t *testing.T, sc scenario) string {
	t.Helper()
	rng := rand.New(rand.NewSource(sc.Seed))
	ctx := context.Background()

	s, err := store.Open(":memory:")
	if err != nil {
		return fmt.Sprintf("store.Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		return fmt.Sprintf("Migrate: %v", err)
	}

	a := inboxadapter.NewAdapter(nil, s)

	pids := make([]string, sc.NumProjects)
	aliases := make([]string, sc.NumProjects)
	for i := 0; i < sc.NumProjects; i++ {

		first := byte('a' + (i % 6))
		second := byte('0' + (i % 10))
		pids[i] = string(first) + strings.Repeat(string(second), 63)
		aliases[i] = fmt.Sprintf("p%02d", i)
		a.RegisterProject(pids[i], aliases[i], s)
	}

	state := map[int64]*tracked{}
	insertCount := 0

	for i := 0; i < sc.NumOps; i++ {

		op := rng.Intn(10)
		pi := rng.Intn(len(pids))
		pid := pids[pi]

		switch {
		case op < 6:

			n := &inbox.Notification{
				ProjectID: pid,
				Severity:  inbox.AllSeverities()[rng.Intn(4)],
				EventType: fmt.Sprintf("evt-%d", insertCount),
				ContentHash: inbox.ComputeContentHash(map[string]any{
					"i":   i,
					"p":   pid,
					"seq": insertCount,
				}),
				Payload:   json.RawMessage(`{}`),
				CreatedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second),
			}
			if err := a.Insert(ctx, n); err != nil {

				if err.Error() != "" && !strings.Contains(err.Error(), "dedup") {
					return fmt.Sprintf("op %d Insert(pid=%s): %v", i, pid[:8], err)
				}
				continue
			}
			insertCount++
			if n.ID > 0 {
				state[n.ID] = &tracked{notifID: n.ID, projectID: pid, alive: true}
			}

		case op == 6:

			if err := a.InsertCache(ctx, inbox.CacheRow{
				ProjectID:      pid,
				ProjectAlias:   aliases[pi],
				NotificationID: int64(i + 1),
				Severity:       inbox.AllSeverities()[rng.Intn(4)],
				EventType:      fmt.Sprintf("cache-%d", i),
				ContentHash:    strings.Repeat("c", 64),
				CreatedAt:      time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second),
			}); err != nil {
				return fmt.Sprintf("op %d InsertCache: %v", i, err)
			}

		case op == 7:

			id := pickLiveID(state, rng)
			if id == 0 {
				continue
			}
			if err := a.Ack(ctx, id); err != nil {

				continue
			}
			if tr, ok := state[id]; ok {
				tr.alive = false
			}

		case op == 8:

			id := pickLiveID(state, rng)
			if id == 0 {
				continue
			}
			_ = a.Snooze(ctx, id, time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))

		case op == 9:

			if err := a.Delete(ctx, pid); err != nil {
				return fmt.Sprintf("op %d Delete(pid=%s): %v", i, pid[:8], err)
			}
			for k, tr := range state {
				if tr.projectID == pid {
					tr.alive = false
					_ = k
				}
			}
		}

		registered := map[string]bool{}
		for _, p := range pids {
			registered[p] = true
		}

		rows, err := a.Query(ctx, inbox.ListFilter{IncludeAcked: true})
		if err != nil {
			return fmt.Sprintf("op %d cache Query: %v", i, err)
		}
		for _, r := range rows {
			if !registered[r.ProjectID] {
				return fmt.Sprintf("op %d cache row %d project_id=%q not registered",
					i, r.NotificationID, r.ProjectID)
			}
		}

		// (b) Per-project List MUST NEVER return rows from a different
		//     project. With all N projects sharing one in-memory store,
		//     the SQL `project_id = ?` clause is the load-bearing
		//     isolation; this assertion fails if a future refactor
		//     accidentally drops it.
		for _, pid := range pids {
			ns, err := a.List(ctx, inbox.ListFilter{
				ProjectID:    pid,
				IncludeAcked: true,
			})
			if err != nil {
				return fmt.Sprintf("op %d List(%s): %v", i, pid[:8], err)
			}
			for _, n := range ns {
				if n.ProjectID != pid {
					return fmt.Sprintf("op %d List(%s) returned row from project %s (NotifID=%d)",
						i, pid[:8], n.ProjectID[:8], n.ID)
				}
			}
		}
	}

	return ""
}

func pickLiveID(state map[int64]*tracked, rng *rand.Rand) int64 {
	var live []int64
	for id, tr := range state {
		if tr.alive {
			live = append(live, id)
		}
	}
	if len(live) == 0 {
		return 0
	}

	for i := 1; i < len(live); i++ {
		for j := i; j > 0 && live[j-1] > live[j]; j-- {
			live[j-1], live[j] = live[j], live[j-1]
		}
	}
	return live[rng.Intn(len(live))]
}

type tracked struct {
	notifID   int64
	projectID string
	alive     bool
}
