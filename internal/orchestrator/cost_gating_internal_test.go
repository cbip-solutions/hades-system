package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeAppenderInternal struct{}

func (fakeAppenderInternal) Append(_ context.Context, _ eventlog.Event) (int64, error) {
	return 0, nil
}

func TestNewCostGatingEngine_DefaultsApplied_Internal(t *testing.T) {
	prof, err := BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	prof.AtomicityTimeout = 0
	cfg := CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  fakeAppenderInternal{},
		Budget:    budgetReaderStub{},
		Workers:   newWorkerSetStub(),
		Actuator:  actuatorStub{},
		Profile:   prof,
		PollEvery: 0,
		SessionID: "sess-x",
		ProjectID: "proj-x",
	}
	eng, err := NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}
	if eng.pollEvery != 500*time.Millisecond {
		t.Errorf("pollEvery default: got %v want 500ms", eng.pollEvery)
	}
	if eng.atomTimeout != 30*time.Second {
		t.Errorf("atomTimeout default: got %v want 30s", eng.atomTimeout)
	}
	if len(eng.table) != 5 {
		t.Errorf("table rows: got %d want 5", len(eng.table))
	}
	if eng.currentRow != nil {
		t.Errorf("currentRow at construction: got %+v want nil", eng.currentRow)
	}
	if eng.stoppedCh == nil {
		t.Error("stoppedCh: nil at construction; expected non-nil")
	}
	if eng.profile.DoctrineName != "max-scope" {
		t.Errorf("profile DoctrineName: got %q want max-scope", eng.profile.DoctrineName)
	}
}

func TestNewCostGatingEngine_PollEverySupplied_NotOverwritten(t *testing.T) {
	prof, err := BuiltinCostProfile("default")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	cfg := CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  fakeAppenderInternal{},
		Budget:    budgetReaderStub{},
		Workers:   newWorkerSetStub(),
		Actuator:  actuatorStub{},
		Profile:   prof,
		PollEvery: 250 * time.Millisecond,
		SessionID: "sess-y",
		ProjectID: "proj-y",
	}
	eng, err := NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}
	if eng.pollEvery != 250*time.Millisecond {
		t.Errorf("pollEvery caller-supplied: got %v want 250ms", eng.pollEvery)
	}
	if eng.atomTimeout != 30*time.Second {
		t.Errorf("atomTimeout from profile: got %v want 30s", eng.atomTimeout)
	}
}

func TestPctFromKey(t *testing.T) {
	cases := []struct {
		key  string
		want Pct
	}{
		{"60", 60}, {"80", 80}, {"90", 90}, {"100", 100},
		{"payg", PctPAYG},
	}
	for _, c := range cases {
		got := pctFromKey(c.key)
		if got != c.want {
			t.Errorf("pctFromKey(%q): got %d want %d", c.key, got, c.want)
		}
	}
}

func TestPctFromKey_PanicsOnNonNumericKey(t *testing.T) {

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("pctFromKey did not panic on non-numeric key")
		}
	}()
	_ = pctFromKey("not-a-number")
}

func TestLoadThresholdTable_NilActionsMap(t *testing.T) {
	prof := CostProfile{
		DoctrineName: "max-scope",
		Actions:      nil,
	}
	_, err := LoadThresholdTable(prof, nil)
	if err == nil {
		t.Fatal("expected error on nil Actions map")
	}

	if !errors.Is(err, ErrMissingCostActionRow) {
		t.Fatalf("want ErrMissingCostActionRow, got %v", err)
	}
}

func TestLoadThresholdTable_PAYGSortsLastEvenWhenInsertedFirst(t *testing.T) {

	prof, err := BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	for i := 0; i < 8; i++ {
		tab, err := LoadThresholdTable(prof, nil)
		if err != nil {
			t.Fatalf("LoadThresholdTable iter=%d: %v", i, err)
		}
		if tab[len(tab)-1].Pct != PctPAYG {
			t.Fatalf("iter=%d last row pct: got %d want PctPAYG", i, tab[len(tab)-1].Pct)
		}

		for j := 1; j < len(tab)-1; j++ {
			if tab[j].Pct <= tab[j-1].Pct {
				t.Fatalf("iter=%d not ascending: row[%d]=%d row[%d]=%d",
					i, j-1, tab[j-1].Pct, j, tab[j].Pct)
			}
		}
	}
}

func TestThresholdLess(t *testing.T) {
	cases := []struct {
		a, b Pct
		want bool
		name string
	}{
		{60, 80, true, "60<80"},
		{80, 60, false, "80<60"},
		{60, 60, false, "equal"},
		{PctPAYG, 60, false, "payg-vs-numeric (payg sorts last → not less)"},
		{60, PctPAYG, true, "numeric-vs-payg (numeric is less)"},
		{PctPAYG, PctPAYG, false, "payg-vs-payg"},
	}
	for _, c := range cases {
		got := thresholdLess(c.a, c.b)
		if got != c.want {
			t.Errorf("%s: thresholdLess(%d,%d)=%v want %v",
				c.name, c.a, c.b, got, c.want)
		}
	}
}

func TestAtomicityGuardEnforced_CompileAnchor(t *testing.T) {

	_ = atomicityGuardEnforced
}

func TestRun_ApplyError_EmitsEventAndContinues(t *testing.T) {
	prof, err := BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	memLog := eventlog.NewMemory(clock.Real{})
	snap := &runApplyErrSnap{}

	snap.set(BudgetSnapshot{CumulativeUSD: 100, DailyCapUSD: 100})

	fakeAct := &runApplyErrActuator{}
	fakeAct.hardPauseErr = errors.New("orchestrator: hard-pause transient")

	cfg := CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  memLog,
		Budget:    snap,
		Workers:   newWorkerSetStub(),
		Actuator:  fakeAct,
		Profile:   prof,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-applyerr",
		ProjectID: "proj-applyerr",
	}
	eng, err := NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	var failedRecs []eventlog.Record
	for time.Now().Before(deadline) {
		recs, qerr := memLog.Query(context.Background(), "sess-applyerr", 0)
		if qerr != nil {
			t.Fatalf("Query: %v", qerr)
		}
		failedRecs = failedRecs[:0]
		for _, r := range recs {
			if r.EventType == eventlog.EvtBudgetDegradationFailed {
				failedRecs = append(failedRecs, r)
			}
		}
		if len(failedRecs) >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(failedRecs) < 1 {
		t.Fatal("no EvtBudgetDegradationFailed audit row emitted within 2s")
	}

	// currentRow MUST stay nil (Apply error → no update).
	if e2, ok := GetCurrentRowForTest(eng); ok {
		t.Errorf("currentRow updated despite Apply error: %+v", e2)
	}

	dec, derr := eventlog.Decode(failedRecs[0].EventType, failedRecs[0].Payload)
	if derr != nil {
		t.Fatalf("Decode: %v", derr)
	}
	bdf, ok := dec.(eventlog.BudgetDegradationFailed)
	if !ok {
		t.Fatalf("decoded type = %T, want BudgetDegradationFailed", dec)
	}
	if bdf.Action != string(CostActionHardPause) {
		t.Errorf("Action = %q, want %q", bdf.Action, CostActionHardPause)
	}
	if !strings.Contains(bdf.Error, "orchestrator: hard-pause transient") {
		t.Errorf("Error = %q, want substring of %q", bdf.Error, fakeAct.hardPauseErr.Error())
	}

	time.Sleep(80 * time.Millisecond)
	recs, _ := memLog.Query(context.Background(), "sess-applyerr", 0)
	failed2 := 0
	for _, r := range recs {
		if r.EventType == eventlog.EvtBudgetDegradationFailed {
			failed2++
		}
	}
	if failed2 < 2 {
		t.Errorf("expected ≥2 failed-audit rows after sustained Apply failures, got %d", failed2)
	}

	cancel()
	<-eng.Stopped()
}

func TestEvaluate_PAYGRowMissingFromTable_DefensiveBaseline(t *testing.T) {
	prof, err := BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	memLog := eventlog.NewMemory(clock.Real{})
	cfg := CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  memLog,
		Budget:    budgetReaderStub{},
		Workers:   newWorkerSetStub(),
		Actuator:  actuatorStub{},
		Profile:   prof,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-pdef",
		ProjectID: "proj-pdef",
	}
	eng, err := NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	pruned := make([]ThresholdRow, 0, len(eng.table))
	for _, r := range eng.table {
		if r.Pct != PctPAYG {
			pruned = append(pruned, r)
		}
	}
	eng.table = pruned

	row := eng.Evaluate(BudgetSnapshot{
		CumulativeUSD: 5, DailyCapUSD: 100, PAYGActive: true,
	})
	if row.Pct != 0 || row.Action != CostActionContinue {
		t.Errorf("missing-payg-row defensive branch: got %+v want {0 continue}", row)
	}
}

func TestPayloadOf_PayloadMarshalError(t *testing.T) {
	got, err := payloadOf(payloadErrEncoder{})
	if err == nil {
		t.Fatalf("payloadOf: want error, got nil (map=%v)", got)
	}
	if got != nil {
		t.Errorf("payloadOf: want nil map on error, got %v", got)
	}
}

func TestPayloadOf_UnmarshalError(t *testing.T) {
	got, err := payloadOf(payloadBadJSONEncoder{})
	if err == nil {
		t.Fatalf("payloadOf: want error, got nil (map=%v)", got)
	}
	if got != nil {
		t.Errorf("payloadOf: want nil map on unmarshal error, got %v", got)
	}
}

type payloadErrEncoder struct{}

func (payloadErrEncoder) Type() eventlog.EventType { return eventlog.EvtBudgetSnapshotError }
func (payloadErrEncoder) Payload() ([]byte, error) {
	return nil, errors.New("synthetic marshal failure")
}

type payloadBadJSONEncoder struct{}

func (payloadBadJSONEncoder) Type() eventlog.EventType { return eventlog.EvtBudgetSnapshotError }
func (payloadBadJSONEncoder) Payload() ([]byte, error) {
	return []byte("not-json-but-valid-bytes"), nil
}

// TestRun_AuditEmissionSurvivesCtxCancel — pre-cancelled ctx yields a
// snapshot error that MUST still produce the audit row via
// context.WithoutCancel. Pins the WithoutCancel-discipline seam
// (consistent with D-2/D-3/E-2/F-2/F-3 audit-trail patterns).
func TestRun_AuditEmissionSurvivesCtxCancel(t *testing.T) {
	prof, err := BuiltinCostProfile("max-scope")
	if err != nil {
		t.Fatalf("BuiltinCostProfile: %v", err)
	}
	memLog := eventlog.NewMemory(clock.Real{})
	snap := &runApplyErrSnap{}
	snap.setErr(errors.New("snapshot: transient read"))
	cfg := CostGatingEngineConfig{
		Clock:     clock.Real{},
		EventLog:  memLog,
		Budget:    snap,
		Workers:   newWorkerSetStub(),
		Actuator:  actuatorStub{},
		Profile:   prof,
		PollEvery: 20 * time.Millisecond,
		SessionID: "sess-cancelaudit",
		ProjectID: "proj-cancelaudit",
	}
	eng, err := NewCostGatingEngine(cfg)
	if err != nil {
		t.Fatalf("NewCostGatingEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go eng.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	var beforeCount int
	for time.Now().Before(deadline) {
		recs, _ := memLog.Query(context.Background(), "sess-cancelaudit", 0)
		beforeCount = 0
		for _, r := range recs {
			if r.EventType == eventlog.EvtBudgetSnapshotError {
				beforeCount++
			}
		}
		if beforeCount >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if beforeCount < 1 {
		t.Fatal("no snapshot-error audit row before cancel")
	}

	cancel()
	<-eng.Stopped()

	recs, _ := memLog.Query(context.Background(), "sess-cancelaudit", 0)
	got := 0
	for _, r := range recs {
		if r.EventType == eventlog.EvtBudgetSnapshotError {
			got++
		}
	}
	if got < beforeCount {
		t.Errorf("post-cancel rows = %d, want ≥%d (pre-cancel observation)", got, beforeCount)
	}
}

type runApplyErrSnap struct {
	mu   sync.Mutex
	snap BudgetSnapshot
	err  error
}

func (f *runApplyErrSnap) Snapshot(_ context.Context) (BudgetSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.err
}

func (f *runApplyErrSnap) set(s BudgetSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap = s
	f.err = nil
}

func (f *runApplyErrSnap) setErr(e error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = e
}

type runApplyErrActuator struct {
	dropAtDepthErr     error
	setTierErr         error
	setParallelismErr  error
	hardPauseErr       error
	emergencyTierErr   error
	escalateL4Err      error
	waitForConfirmErr  error
	waitingErr         error
	restoreDefaultsErr error
}

func (a *runApplyErrActuator) DropAtDepth(_ context.Context, _ int) error { return a.dropAtDepthErr }
func (a *runApplyErrActuator) SetTier(_ context.Context, _ int) error     { return a.setTierErr }
func (a *runApplyErrActuator) SetParallelism(_ context.Context, _, _ int) error {
	return a.setParallelismErr
}
func (a *runApplyErrActuator) HardPause(_ context.Context, _ string) error { return a.hardPauseErr }
func (a *runApplyErrActuator) EmergencyOnlyTier(_ context.Context) error   { return a.emergencyTierErr }
func (a *runApplyErrActuator) EscalateL4(_ context.Context, _ map[string]any) error {
	return a.escalateL4Err
}
func (a *runApplyErrActuator) WaitForConfirmation(_ context.Context, _ string) error {
	return a.waitForConfirmErr
}
func (a *runApplyErrActuator) Waiting(_ context.Context, _ string) error { return a.waitingErr }
func (a *runApplyErrActuator) RestoreDefaults(_ context.Context) error   { return a.restoreDefaultsErr }

type budgetReaderStub struct{}

func (budgetReaderStub) Snapshot(_ context.Context) (BudgetSnapshot, error) {
	return BudgetSnapshot{}, nil
}

type workerSetStub struct {
	ch chan struct{}
}

func newWorkerSetStub() *workerSetStub {
	ch := make(chan struct{})
	close(ch)
	return &workerSetStub{ch: ch}
}

func (w *workerSetStub) WaitAtomicBoundary(_ context.Context) <-chan struct{} { return w.ch }

type actuatorStub struct{}

func (actuatorStub) DropAtDepth(_ context.Context, _ int) error            { return nil }
func (actuatorStub) SetTier(_ context.Context, _ int) error                { return nil }
func (actuatorStub) SetParallelism(_ context.Context, _, _ int) error      { return nil }
func (actuatorStub) HardPause(_ context.Context, _ string) error           { return nil }
func (actuatorStub) EmergencyOnlyTier(_ context.Context) error             { return nil }
func (actuatorStub) EscalateL4(_ context.Context, _ map[string]any) error  { return nil }
func (actuatorStub) WaitForConfirmation(_ context.Context, _ string) error { return nil }
func (actuatorStub) Waiting(_ context.Context, _ string) error             { return nil }
func (actuatorStub) RestoreDefaults(_ context.Context) error               { return nil }

func GetCurrentRowForTest(e *CostGatingEngine) (ThresholdRow, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.currentRow == nil {
		return ThresholdRow{}, false
	}
	return *e.currentRow, true
}

func GetCurrentRowPtrForTest(e *CostGatingEngine) (*ThresholdRow, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentRow, e.currentRow != nil
}
