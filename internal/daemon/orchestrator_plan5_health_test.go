package daemon

import (
	"context"
	"database/sql"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func staticSampler(snap orchestrator.HealthSnapshot) *orchestrator.HealthSampler {
	s := orchestrator.NewHealthSampler(func(_ context.Context) orchestrator.HealthSnapshot {
		return snap
	}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-s.Run(ctx)
	return s
}

func newTestPlan5ServiceWithStore(t *testing.T) (*Plan5OrchestratorService, *store.Store) {
	t.Helper()
	st := newTestStore(t)
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := NewPlan5OrchestratorService(Plan5OrchestratorServiceConfig{
		Adapter: a,
	})
	if err != nil {
		t.Fatalf("NewPlan5OrchestratorService: %v", err)
	}
	return svc, st
}

func countAuditEventsRaw(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	row := st.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_events_raw`,
	)
	if err := row.Scan(&n); err != nil && err != sql.ErrNoRows {
		t.Fatalf("countAuditEventsRaw: %v", err)
	}
	return n
}

func TestHealthEventLogWritable_NoPerCallAuditWrite(t *testing.T) {
	svc, st := newTestPlan5ServiceWithStore(t)
	svc.SetHealthSampler(staticSampler(orchestrator.HealthSnapshot{
		Deps: map[string]orchestrator.DepHealth{"event_log_writable": {Up: true}},
	}))
	before := countAuditEventsRaw(t, st)
	for i := 0; i < 50; i++ {
		_, _, _ = svc.HealthEventLogWritable()
	}
	if got := countAuditEventsRaw(t, st); got != before {
		t.Fatalf("health poll wrote audit rows: before=%d after=%d", before, got)
	}
}

func TestHealthMethods_ReadSnapshotNotCheckEngine(t *testing.T) {
	snap := orchestrator.HealthSnapshot{Deps: map[string]orchestrator.DepHealth{
		"research_mcp_up": {Up: true},
	}}
	svc := newTestPlan5Service(t)
	svc.SetHealthSampler(staticSampler(snap))

	up, err := svc.HealthResearchMCPUp()
	if err != nil || !up {
		t.Fatalf("research_mcp_up: want up,nil; got %v,%v", up, err)
	}
}
