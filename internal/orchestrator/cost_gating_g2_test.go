package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type costFakeSnapshotReader struct {
	mu        sync.Mutex
	snap      orchestrator.BudgetSnapshot
	err       error
	callCount atomic.Int32
}

func (f *costFakeSnapshotReader) Snapshot(_ context.Context) (orchestrator.BudgetSnapshot, error) {
	f.callCount.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.err
}

func (f *costFakeSnapshotReader) set(s orchestrator.BudgetSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap = s
	f.err = nil
}

func (f *costFakeSnapshotReader) setErr(e error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = e
}

type costFakeActuator struct{}

func (costFakeActuator) DropAtDepth(_ context.Context, _ int) error            { return nil }
func (costFakeActuator) SetTier(_ context.Context, _ int) error                { return nil }
func (costFakeActuator) SetParallelism(_ context.Context, _, _ int) error      { return nil }
func (costFakeActuator) HardPause(_ context.Context, _ string) error           { return nil }
func (costFakeActuator) EmergencyOnlyTier(_ context.Context) error             { return nil }
func (costFakeActuator) EscalateL4(_ context.Context, _ map[string]any) error  { return nil }
func (costFakeActuator) WaitForConfirmation(_ context.Context, _ string) error { return nil }
func (costFakeActuator) Waiting(_ context.Context, _ string) error             { return nil }
func (costFakeActuator) RestoreDefaults(_ context.Context) error               { return nil }

type costFakeWorkerSet struct {
	mu       sync.Mutex
	boundary chan struct{}
}

func newCostFakeWorkerSet(initialReady bool) *costFakeWorkerSet {
	ws := &costFakeWorkerSet{boundary: make(chan struct{})}
	if initialReady {
		close(ws.boundary)
	}
	return ws
}

func (f *costFakeWorkerSet) WaitAtomicBoundary(_ context.Context) <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boundary
}

func (f *costFakeWorkerSet) signalAtomicBoundary() {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.boundary:

	default:
		close(f.boundary)
	}
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("waitFor: condition never satisfied within 2s")
}

func g2Cfg(t *testing.T, snap *costFakeSnapshotReader, log *eventlog.Log, clk clock.Clock, doctrine string) orchestrator.CostGatingEngineConfig {
	t.Helper()
	prof, err := orchestrator.BuiltinCostProfile(doctrine)
	if err != nil {
		t.Fatalf("BuiltinCostProfile(%q): %v", doctrine, err)
	}
	return orchestrator.CostGatingEngineConfig{
		Clock:     clk,
		EventLog:  log,
		Budget:    snap,
		Workers:   newCostFakeWorkerSet(true),
		Actuator:  costFakeActuator{},
		Profile:   prof,
		Override:  nil,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-g2",
		ProjectID: "proj-g2",
	}
}

func TestEvaluate_BelowAllThresholdsReturnsContinue(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}
	row := eng.Evaluate(orchestrator.BudgetSnapshot{
		CumulativeUSD: 10, DailyCapUSD: 100,
	})
	if row.Pct != 0 || row.Action != orchestrator.CostActionContinue {
		t.Errorf("got %+v, want {Pct:0 Action:continue}", row)
	}
}

func TestEvaluate_60Pct_MaxScope_ReturnsDropL3(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	if row.Pct != 60 || row.Action != orchestrator.CostActionDropL3Strategic {
		t.Errorf("got %+v, want {Pct:60 Action:drop_l3_strategic}", row)
	}
}

func TestEvaluate_PAYGOverridesPercentage(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := eng.Evaluate(orchestrator.BudgetSnapshot{
		CumulativeUSD: 5, DailyCapUSD: 100, PAYGActive: true,
	})
	if row.Pct != orchestrator.PctPAYG || row.Action != orchestrator.CostActionEmergencyOnlyTier {
		t.Errorf("got %+v, want {Pct:PctPAYG Action:emergency_only_tier}", row)
	}
}

func TestEvaluate_HighestThresholdWins(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 95, DailyCapUSD: 100})
	if row.Pct != 90 || row.Action != orchestrator.CostActionReduceParallelism {
		t.Errorf("got %+v, want {Pct:90 Action:reduce_parallelism}", row)
	}
}

func TestEvaluate_AtExactly100Pct(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: 100})
	if row.Pct != 100 || row.Action != orchestrator.CostActionHardPause {
		t.Errorf("got %+v, want {Pct:100 Action:hard_pause}", row)
	}
}

func TestEvaluate_AboveCap(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 150, DailyCapUSD: 100})
	if row.Pct != 100 || row.Action != orchestrator.CostActionHardPause {
		t.Errorf("got %+v, want {Pct:100 Action:hard_pause}", row)
	}
}

func TestEvaluate_ZeroDailyCap(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: 0})
	if row.Pct != 0 || row.Action != orchestrator.CostActionContinue {
		t.Errorf("got %+v, want {Pct:0 Action:continue}", row)
	}
}

func TestEvaluate_NegativeDailyCap(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: -5})
	if row.Pct != 0 || row.Action != orchestrator.CostActionContinue {
		t.Errorf("got %+v, want {Pct:0 Action:continue}", row)
	}
}

func TestEvaluate_NegativeCumulative(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)

	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: -10, DailyCapUSD: 100})
	if row.Pct != 0 || row.Action != orchestrator.CostActionContinue {
		t.Errorf("got %+v, want {Pct:0 Action:continue}", row)
	}
}

func TestEvaluate_DefaultDoctrine_60PctIsContinue(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "default")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	if row.Pct != 60 || row.Action != orchestrator.CostActionContinue {
		t.Errorf("got %+v, want {Pct:60 Action:continue}", row)
	}
}

func TestEvaluate_CapaFirewallDoctrine_60PctIsEscalateL4(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "capa-firewall")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	if row.Pct != 60 || row.Action != orchestrator.CostActionEscalateL4 {
		t.Errorf("got %+v, want {Pct:60 Action:escalate_l4}", row)
	}
}

func TestEvaluate_PAYGWithCapaFirewall_HardPause(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "capa-firewall")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	row := eng.Evaluate(orchestrator.BudgetSnapshot{
		CumulativeUSD: 5, DailyCapUSD: 100, PAYGActive: true,
	})
	if row.Pct != orchestrator.PctPAYG || row.Action != orchestrator.CostActionHardPause {
		t.Errorf("got %+v, want {Pct:PctPAYG Action:hard_pause}", row)
	}
}

func queryCostEvents(t *testing.T, log *eventlog.Log, et eventlog.EventType) []eventlog.Record {
	t.Helper()
	recs, err := log.Query(context.Background(), "sess-g2", 0)
	if err != nil {
		t.Fatalf("log.Query: %v", err)
	}
	out := make([]eventlog.Record, 0, len(recs))
	for _, r := range recs {
		if r.EventType == et {
			out = append(out, r)
		}
	}
	return out
}

func TestRun_PollsAndAppliesOnTransition(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, err := orchestrator.NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionContinue
	})

	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 65, DailyCapUSD: 100})
	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionDropL3Strategic
	})

	// Phase 3: stay at 65% — Apply MUST NOT fire again. Sample currentRow
	// before, hold for a few ticks, sample after; if Apply re-fired the
	// pointer would be replaced with a new allocation (currentRow is
	// *ThresholdRow). We assert the pointer is identical across the
	// interval.
	rowBefore, _ := orchestrator.GetCurrentRowPtrForTest(eng)
	time.Sleep(100 * time.Millisecond)
	rowAfter, _ := orchestrator.GetCurrentRowPtrForTest(eng)
	if rowBefore != rowAfter {
		t.Errorf("Apply re-fired on no-transition: row pointer changed (%p vs %p)", rowBefore, rowAfter)
	}

	cancel()
	<-eng.Stopped()
}

func TestRun_NoTransition_NoApply(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionContinue
	})
	rowBefore, _ := orchestrator.GetCurrentRowPtrForTest(eng)

	time.Sleep(200 * time.Millisecond)
	rowAfter, _ := orchestrator.GetCurrentRowPtrForTest(eng)
	if rowBefore != rowAfter {
		t.Errorf("Apply re-fired during sustained baseline: ptr changed")
	}

	if got := snap.callCount.Load(); got < 5 {
		t.Errorf("snapshot poll count = %d, want ≥5 (200ms / 20ms cadence)", got)
	}

	cancel()
	<-eng.Stopped()
}

func TestRun_PAYGTransition_ApplyOnce(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionContinue
	})

	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100, PAYGActive: true})
	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionEmergencyOnlyTier
	})

	cancel()
	<-eng.Stopped()
}

func TestRun_BackToBaseline_AppliesContinue(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 60, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionDropL3Strategic
	})

	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	waitFor(t, func() bool {
		row, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok && row.Action == orchestrator.CostActionContinue
	})

	cancel()
	<-eng.Stopped()
}

func TestRun_SnapshotError_EmitsEventAndContinues(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.setErr(errors.New("transient: budget read"))
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	waitFor(t, func() bool {
		return len(queryCostEvents(t, log, eventlog.EvtBudgetSnapshotError)) >= 1
	})

	recs := queryCostEvents(t, log, eventlog.EvtBudgetSnapshotError)
	dec, derr := eventlog.Decode(recs[0].EventType, recs[0].Payload)
	if derr != nil {
		t.Fatalf("Decode: %v", derr)
	}
	bse, ok := dec.(eventlog.BudgetSnapshotError)
	if !ok {
		t.Fatalf("decoded type = %T, want BudgetSnapshotError", dec)
	}
	if !strings.Contains(bse.Error, "transient: budget read") {
		t.Errorf("Error payload = %q, want substring %q", bse.Error, "transient: budget read")
	}

	if _, ok := orchestrator.GetCurrentRowForTest(eng); ok {
		t.Errorf("currentRow updated despite Snapshot errors")
	}

	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	waitFor(t, func() bool {
		_, ok := orchestrator.GetCurrentRowForTest(eng)
		return ok
	})

	cancel()
	<-eng.Stopped()
}

func TestRun_StoppedChannelClosedOnCtxCancel(t *testing.T) {
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go eng.Run(ctx)

	select {
	case <-eng.Stopped():
		t.Fatal("Stopped() closed before ctx-cancel")
	case <-time.After(30 * time.Millisecond):

	}

	cancel()

	// Post-cancel: Stopped MUST close within reasonable time.
	select {
	case <-eng.Stopped():

	case <-time.After(2 * time.Second):
		t.Fatal("Stopped() did not close within 2s after ctx-cancel")
	}
}

func TestRun_RaceFree(t *testing.T) {
	if testing.Short() {
		t.Skip("race test skipped under -short")
	}
	snap := &costFakeSnapshotReader{}
	snap.set(orchestrator.BudgetSnapshot{CumulativeUSD: 10, DailyCapUSD: 100})
	log := eventlog.NewMemory(clock.Real{})
	cfg := g2Cfg(t, snap, log, clock.Real{}, "max-scope")
	eng, _ := orchestrator.NewCostGatingEngine(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(seed float64) {
			defer wg.Done()
			deadline := time.Now().Add(150 * time.Millisecond)
			caps := []float64{10, 60, 80, 95, 100, 10, 60}
			j := 0
			for time.Now().Before(deadline) {
				snap.set(orchestrator.BudgetSnapshot{
					CumulativeUSD: caps[j%len(caps)] + seed,
					DailyCapUSD:   100,
				})
				j++
				time.Sleep(2 * time.Millisecond)
			}
		}(float64(i))
	}
	wg.Wait()
	cancel()
	<-eng.Stopped()
}
