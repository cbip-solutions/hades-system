// Package compliance — invariant: per-project inbox isolation,
// no cross-project leak in the daemon-level aggregator cache.
//
// Spec §5.5 + §7.2 invariant wording:
//
// "Cross-project inbox isolation: every cache row's project_id MUST
// match its originating per-project source DB; List(projectID=X)
// MUST NEVER return rows from any project Y != X."
//
// This test is the cross-package, boundary-side property witness for
// the no-cross-project-leak invariant. The in-package coverage in
// internal/inbox/outbox_test.go locks the bridge construction site
// (CacheWrite → CacheRow ProjectID copy); this file re-asserts the
// contract from outside the package as a randomized property test
// across N=5 projects × 200 mixed Insert / InsertCache / Ack
// operations so any future refactor of the routing layer (per-project
// store fanout, alias denormalization, fanout queue semantics) gets
// caught at the public surface before it ships.
//
// Property under test:
//
// (a) Every cache row reachable via Adapter.Query has a project_id
// that names one of the registered projects.
// (b) Per-project Adapter.List(ProjectID=pid) NEVER returns a row
// whose ProjectID != pid — even when all per-project sources
// share a single backing store (test-mode aggregation pattern).
//
// Boundary: this test imports internal/daemon/
// inboxadapter (the only crossing point) + internal/inbox + internal/
// store. internal/inbox itself does NOT import internal/store; the
// adapter is the bridge.
//
// Inv-zen-113 contract.
package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter"
	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/store"
)

var _ = noCrossProjectInboxLeakAnchorReference()

func noCrossProjectInboxLeakAnchorReference() error {
	return inbox.ErrCrossProjectInboxLeakAnchor
}

func TestInvZen113NoCrossProjectInboxLeakPropertyN5(t *testing.T) {
	const numProjects = 5
	const numOps = 200
	const numIterations = 5

	seeds := []int64{2026, 31337, 42, 9001, 0xFEEDFACE}
	if len(seeds) != numIterations {
		t.Fatalf("test misconfigured: %d seeds for %d iterations", len(seeds), numIterations)
	}

	type op int
	const (
		opInsert op = iota
		opCacheInsert
		opAck
	)

	for iter, seed := range seeds {
		iter := iter
		seed := seed
		t.Run(fmt.Sprintf("iter=%d/seed=%d", iter, seed), func(t *testing.T) {
			s, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			if err := s.Migrate(); err != nil {
				t.Fatalf("Migrate: %v", err)
			}

			a := inboxadapter.NewAdapter(nil, s)
			pids := make([]string, numProjects)
			for i := 0; i < numProjects; i++ {

				pids[i] = string(rune('a'+i)) + strings.Repeat(string(rune('0'+i)), 63)
				a.RegisterProject(pids[i], string(rune('a'+i)), s)
			}

			ctx := context.Background()
			r := rand.New(rand.NewSource(seed))

			for i := 0; i < numOps; i++ {
				pid := pids[r.Intn(numProjects)]
				switch op(r.Intn(3)) {
				case opInsert:
					n := &inbox.Notification{
						ProjectID: pid,
						Severity:  inbox.AllSeverities()[r.Intn(4)],
						EventType: "evt",
						ContentHash: inbox.ComputeContentHash(map[string]any{
							"i": i, "p": pid, "iter": iter,
						}),
						Payload:   json.RawMessage(`{}`),
						CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
					}
					_ = a.Insert(ctx, n)
				case opCacheInsert:
					id := int64(r.Int63n(int64(numOps)) + 1)
					_ = a.InsertCache(ctx, inbox.CacheRow{
						ProjectID:      pid,
						ProjectAlias:   pid[:5],
						NotificationID: id,
						Severity:       inbox.AllSeverities()[r.Intn(4)],
						EventType:      "evt-cache",
						ContentHash:    strings.Repeat("c", 64),
						CreatedAt: time.Now().UTC().Add(
							time.Duration(i) * time.Second,
						),
					})
				case opAck:
					id := int64(r.Int63n(int64(numOps)) + 1)
					_ = a.Ack(ctx, id)
				}
			}

			rows, err := a.Query(ctx, inbox.ListFilter{IncludeAcked: true})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			registered := map[string]bool{}
			for _, pid := range pids {
				registered[pid] = true
			}
			for _, cr := range rows {
				if !registered[cr.ProjectID] {
					t.Errorf("inv-zen-113 violation (iter=%d seed=%d): "+
						"cache row.project_id=%q not in registered set",
						iter, seed, cr.ProjectID)
				}
			}

			// Property (b): per-project List MUST NEVER return rows
			// from a different project. With all 5 projects sharing one
			// in-memory store, the SQL WHERE project_id = ? clause is
			// the load-bearing isolation; this assertion fails if a
			// future refactor accidentally drops it.
			for _, pid := range pids {
				ns, err := a.List(ctx, inbox.ListFilter{
					ProjectID:    pid,
					IncludeAcked: true,
				})
				if err != nil {
					t.Fatalf("List(%s): %v", pid, err)
				}
				for _, n := range ns {
					if n.ProjectID != pid {
						t.Errorf("inv-zen-113 violation (iter=%d seed=%d): "+
							"List(%q) returned row from project %q",
							iter, seed, pid, n.ProjectID)
					}
				}
			}
		})
	}
}
