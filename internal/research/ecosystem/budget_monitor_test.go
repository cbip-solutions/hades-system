package ecosystem_test

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type fakeSizer struct {
	totalBytes int64
	err        error
}

func (f *fakeSizer) TotalBytes(ctx context.Context) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.totalBytes, nil
}

func gb(n float64) int64 { return int64(n * (1 << 30)) }

func TestBudgetMonitor_StateGreen(t *testing.T) {

	sizer := &fakeSizer{totalBytes: gb(25)}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  40,
		CeilingGB: 60,
		Sizer:     sizer,
	})
	ctx := context.Background()
	status, err := bm.Check(ctx)
	if err != nil {
		t.Fatalf("Check: unexpected error: %v", err)
	}
	if status.State != ecosystem.BudgetGreen {
		t.Errorf("want BudgetGreen, got %v", status.State)
	}
	if status.TotalGB < 24.9 || status.TotalGB > 25.1 {
		t.Errorf("TotalGB: want ~25.0, got %.2f", status.TotalGB)
	}
	if status.BlockNewIngest {
		t.Error("BlockNewIngest must be false when Green")
	}
	if status.BlockAllWrites {
		t.Error("BlockAllWrites must be false when Green")
	}
}

func TestBudgetMonitor_StateYellow(t *testing.T) {

	for _, gbSize := range []float64{32.0, 35.5, 39.9} {
		sizer := &fakeSizer{totalBytes: gb(gbSize)}
		bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
			TargetGB:  40,
			CeilingGB: 60,
			Sizer:     sizer,
		})
		ctx := context.Background()
		status, err := bm.Check(ctx)
		if err != nil {
			t.Fatalf("Check(%.1f GB): unexpected error: %v", gbSize, err)
		}
		if status.State != ecosystem.BudgetYellow {
			t.Errorf("%.1f GB: want BudgetYellow, got %v", gbSize, status.State)
		}
		if status.BlockNewIngest {
			t.Errorf("%.1f GB: BlockNewIngest must be false at Yellow", gbSize)
		}
		if status.BlockAllWrites {
			t.Errorf("%.1f GB: BlockAllWrites must be false at Yellow", gbSize)
		}
	}
}

func TestBudgetMonitor_StateRed(t *testing.T) {

	for _, gbSize := range []float64{40.0, 50.0, 59.9} {
		sizer := &fakeSizer{totalBytes: gb(gbSize)}
		bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
			TargetGB:  40,
			CeilingGB: 60,
			Sizer:     sizer,
		})
		ctx := context.Background()
		status, err := bm.Check(ctx)
		if err != nil {
			t.Fatalf("Check(%.1f GB): unexpected error: %v", gbSize, err)
		}
		if status.State != ecosystem.BudgetRed {
			t.Errorf("%.1f GB: want BudgetRed, got %v", gbSize, status.State)
		}
		if !status.BlockNewIngest {
			t.Errorf("%.1f GB: BlockNewIngest must be true at Red", gbSize)
		}
		if status.BlockAllWrites {
			t.Errorf("%.1f GB: BlockAllWrites must be false at Red (updates OK)", gbSize)
		}
	}
}

func TestBudgetMonitor_StateOverflow(t *testing.T) {

	for _, gbSize := range []float64{60.0, 75.0, 100.0} {
		sizer := &fakeSizer{totalBytes: gb(gbSize)}
		bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
			TargetGB:  40,
			CeilingGB: 60,
			Sizer:     sizer,
		})
		ctx := context.Background()
		status, err := bm.Check(ctx)
		if err != nil {
			t.Fatalf("Check(%.1f GB): unexpected error: %v", gbSize, err)
		}
		if status.State != ecosystem.BudgetOverflow {
			t.Errorf("%.1f GB: want BudgetOverflow, got %v", gbSize, status.State)
		}
		if !status.BlockNewIngest {
			t.Errorf("%.1f GB: BlockNewIngest must be true at Overflow", gbSize)
		}
		if !status.BlockAllWrites {
			t.Errorf("%.1f GB: BlockAllWrites must be true at Overflow (inv-zen-199)", gbSize)
		}
	}
}

func TestBudgetMonitor_ExactBoundaries(t *testing.T) {

	sizer32 := &fakeSizer{totalBytes: gb(32)}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB: 40, CeilingGB: 60, Sizer: sizer32,
	})
	status, _ := bm.Check(context.Background())
	if status.State != ecosystem.BudgetYellow {
		t.Errorf("32 GB (exactly 80%%): want Yellow, got %v", status.State)
	}

	sizer40 := &fakeSizer{totalBytes: gb(40)}
	bm2 := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB: 40, CeilingGB: 60, Sizer: sizer40,
	})
	status2, _ := bm2.Check(context.Background())
	if status2.State != ecosystem.BudgetRed {
		t.Errorf("40 GB (exactly 100%%): want Red, got %v", status2.State)
	}

	sizer60 := &fakeSizer{totalBytes: gb(60)}
	bm3 := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB: 40, CeilingGB: 60, Sizer: sizer60,
	})
	status3, _ := bm3.Check(context.Background())
	if status3.State != ecosystem.BudgetOverflow {
		t.Errorf("60 GB (exactly ceiling): want Overflow, got %v", status3.State)
	}
}

func TestNewBudgetMonitor_PanicsOnInvalidTargetGB(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when TargetGB ≤ 0")
		}
	}()
	_ = ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  0,
		CeilingGB: 60,
	})
}

func TestNewBudgetMonitor_PanicsWhenCeilingNotGreaterThanTarget(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when CeilingGB ≤ TargetGB")
		}
	}()
	_ = ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  40,
		CeilingGB: 40,
	})
}

func TestBudgetMonitor_NilSizerReturnsError(t *testing.T) {
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  40,
		CeilingGB: 60,
		Sizer:     nil,
	})
	_, err := bm.Check(context.Background())
	if err == nil {
		t.Fatal("expected error when Sizer is nil; got nil")
	}
}

type fakeAuditEmitter struct {
	events []string
}

func (f *fakeAuditEmitter) EmitBudgetStateChange(ctx context.Context, prev, next ecosystem.BudgetState, totalGB float64) error {
	f.events = append(f.events, prev.String()+"→"+next.String())
	return nil
}

func TestBudgetMonitor_EmitsAuditEventOnStateChange(t *testing.T) {
	sizer := &fakeSizer{totalBytes: gb(25)}
	emitter := &fakeAuditEmitter{}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:     40,
		CeilingGB:    60,
		Sizer:        sizer,
		AuditEmitter: emitter,
	})
	ctx := context.Background()

	_, err := bm.Check(ctx)
	if err != nil {
		t.Fatalf("first Check: %v", err)
	}
	if len(emitter.events) != 0 {
		t.Errorf("first check should not emit event (no prior state); got %v", emitter.events)
	}

	_, err = bm.Check(ctx)
	if err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if len(emitter.events) != 0 {
		t.Errorf("same-state check should not emit event; got %v", emitter.events)
	}

	sizer.totalBytes = gb(35)
	_, err = bm.Check(ctx)
	if err != nil {
		t.Fatalf("third Check: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("state transition (Green→Yellow) must emit 1 event; got %d: %v", len(emitter.events), emitter.events)
	}
	if emitter.events[0] != "green→yellow" {
		t.Errorf("event: want green→yellow, got %q", emitter.events[0])
	}

	sizer.totalBytes = gb(65)
	_, err = bm.Check(ctx)
	if err != nil {
		t.Fatalf("fourth Check: %v", err)
	}
	if len(emitter.events) != 2 {
		t.Fatalf("second transition must emit event; got %d", len(emitter.events))
	}
	if emitter.events[1] != "yellow→overflow" {
		t.Errorf("event: want yellow→overflow, got %q", emitter.events[1])
	}
}

// fakeAuditEmitterErr returns an error on emit; used to verify that audit
// failures do NOT block status return (best-effort emission per master §3.6).
type fakeAuditEmitterErr struct {
	called int
}

func (f *fakeAuditEmitterErr) EmitBudgetStateChange(ctx context.Context, prev, next ecosystem.BudgetState, totalGB float64) error {
	f.called++
	return errors.New("simulated audit failure")
}

func TestBudgetMonitor_AuditEmitFailureDoesNotBlockStatus(t *testing.T) {
	sizer := &fakeSizer{totalBytes: gb(25)}
	emitter := &fakeAuditEmitterErr{}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:     40,
		CeilingGB:    60,
		Sizer:        sizer,
		AuditEmitter: emitter,
	})
	ctx := context.Background()

	if _, err := bm.Check(ctx); err != nil {
		t.Fatalf("priming Check: %v", err)
	}

	sizer.totalBytes = gb(35)
	status, err := bm.Check(ctx)
	if err != nil {
		t.Fatalf("Check with failing emitter must not return error; got %v", err)
	}
	if status.State != ecosystem.BudgetYellow {
		t.Errorf("state must be Yellow despite audit failure; got %v", status.State)
	}
	if emitter.called != 1 {
		t.Errorf("emitter should be called once on transition; called %d", emitter.called)
	}
}

func TestBudgetMonitor_AllowIngest_ByState(t *testing.T) {
	cases := []struct {
		gbSize         float64
		allowNewIngest bool
		allowAllWrites bool
	}{
		{25, true, true},
		{35, true, true},
		{50, false, true},
		{65, false, false},
	}
	for _, tc := range cases {
		sizer := &fakeSizer{totalBytes: gb(tc.gbSize)}
		bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
			TargetGB: 40, CeilingGB: 60, Sizer: sizer,
		})
		status, _ := bm.Check(context.Background())
		if status.BlockNewIngest != !tc.allowNewIngest {
			t.Errorf("%.0f GB: BlockNewIngest=%v want %v", tc.gbSize, status.BlockNewIngest, !tc.allowNewIngest)
		}
		if status.BlockAllWrites != !tc.allowAllWrites {
			t.Errorf("%.0f GB: BlockAllWrites=%v want %v", tc.gbSize, status.BlockAllWrites, !tc.allowAllWrites)
		}
	}
}

func TestBudgetMonitor_SizerError_ReturnsError(t *testing.T) {
	sizer := &fakeSizer{err: errors.New("stat failed")}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB: 40, CeilingGB: 60, Sizer: sizer,
	})
	_, err := bm.Check(context.Background())
	if err == nil {
		t.Fatal("expected error when sizer fails; got nil")
	}
}

func TestBudgetState_String(t *testing.T) {
	cases := []struct {
		state ecosystem.BudgetState
		want  string
	}{
		{ecosystem.BudgetGreen, "green"},
		{ecosystem.BudgetYellow, "yellow"},
		{ecosystem.BudgetRed, "red"},
		{ecosystem.BudgetOverflow, "overflow"},
		{ecosystem.BudgetState(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestDBSizer_SumsFileSizes(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.db"), make([]byte, 1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "python.db"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	sizer := ecosystem.NewDBSizer(dir)
	total, err := sizer.TotalBytes(context.Background())
	if err != nil {
		t.Fatalf("TotalBytes: %v", err)
	}
	if total != 3*1024*1024 {
		t.Errorf("TotalBytes: want 3145728, got %d", total)
	}
}

func TestDBSizer_MissingDirReturnsZero(t *testing.T) {

	sizer := ecosystem.NewDBSizer("/nonexistent/path/that/does/not/exist")
	total, err := sizer.TotalBytes(context.Background())
	if err != nil {
		t.Fatalf("TotalBytes: missing dir must not error; got %v", err)
	}
	if total != 0 {
		t.Errorf("TotalBytes: missing dir must return 0; got %d", total)
	}
}

func TestDBSizer_IncludesWALAndSHM(t *testing.T) {
	// WAL/SHM files ARE on disk and MUST count toward total.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.db"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.db-wal"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.db-shm"), make([]byte, 512), 0o644); err != nil {
		t.Fatal(err)
	}
	sizer := ecosystem.NewDBSizer(dir)
	total, err := sizer.TotalBytes(context.Background())
	if err != nil {
		t.Fatalf("TotalBytes: %v", err)
	}
	const want = 1024 + 2048 + 512
	if total != want {
		t.Errorf("TotalBytes: want %d, got %d", want, total)
	}
}

func TestDBSizer_ReadDirError(t *testing.T) {

	dir := t.TempDir()
	notADir := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(notADir, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	sizer := ecosystem.NewDBSizer(notADir)
	_, err := sizer.TotalBytes(context.Background())
	if err == nil {
		t.Fatal("expected error when ReadDir target is a file, got nil")
	}
}

func TestDBSizer_UnreadableSubdirSkipped(t *testing.T) {

	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod permissions; skipping unreadable-subdir test")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.db"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "ab")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "skipped"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(subdir, 0o000); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(subdir, 0o755) })

	sizer := ecosystem.NewDBSizer(dir)
	total, err := sizer.TotalBytes(context.Background())
	if err != nil {
		t.Fatalf("unreadable subdir should be skipped, not errored; got %v", err)
	}

	if total != 1024 {
		t.Errorf("TotalBytes: want 1024 (go.db only), got %d", total)
	}
}

func TestDBSizer_WithCASDir(t *testing.T) {

	dbDir := t.TempDir()
	casDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dbDir, "go.db"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(casDir, "ab")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "cdef1234"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "0011aabb"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	sizer := ecosystem.NewDBSizer(dbDir).WithCASDir(casDir)
	total, err := sizer.TotalBytes(context.Background())
	if err != nil {
		t.Fatalf("TotalBytes: %v", err)
	}
	const want = 1024 + 4096 + 2048
	if total != want {
		t.Errorf("TotalBytes: want %d, got %d", want, total)
	}
}

func TestBudgetMonitor_PropertyStateClassification(t *testing.T) {

	cases := []struct {
		gb    float64
		state ecosystem.BudgetState
	}{
		{0, ecosystem.BudgetGreen}, {31.9, ecosystem.BudgetGreen},
		{32.0, ecosystem.BudgetYellow}, {39.9, ecosystem.BudgetYellow},
		{40.0, ecosystem.BudgetRed}, {59.9, ecosystem.BudgetRed},
		{60.0, ecosystem.BudgetOverflow}, {999, ecosystem.BudgetOverflow},
	}
	for _, tc := range cases {
		got := ecosystem.ClassifyBudgetState(tc.gb, 40, 60)
		if got != tc.state {
			t.Errorf("%.1f GB: want %v, got %v", tc.gb, tc.state, got)
		}
	}

	const (
		targetGB  = 40.0
		ceilingGB = 60.0
		yellowPct = 0.80
	)
	rng := rand.New(rand.NewSource(0xBEEF))
	for i := 0; i < 1000; i++ {

		sample := rng.Float64() * 100.0

		var want ecosystem.BudgetState
		switch {
		case sample < targetGB*yellowPct:
			want = ecosystem.BudgetGreen
		case sample < targetGB:
			want = ecosystem.BudgetYellow
		case sample < ceilingGB:
			want = ecosystem.BudgetRed
		default:
			want = ecosystem.BudgetOverflow
		}

		got := ecosystem.ClassifyBudgetState(sample, targetGB, ceilingGB)
		if got != want {
			t.Errorf("iter=%d sample=%.4f GB: want %v, got %v", i, sample, want, got)
		}
	}
}

func TestBudgetMonitor_CachesLastCheck(t *testing.T) {
	callCount := 0
	sizer := &countingSizer{bytes: gb(25), counter: &callCount}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  40,
		CeilingGB: 60,
		Sizer:     sizer,
		CacheTTL:  5 * time.Minute,
	})
	ctx := context.Background()
	if _, err := bm.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bm.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("sizer should be called once within CacheTTL; called %d times", callCount)
	}
}

func TestBudgetMonitor_NoCacheWhenTTLZero(t *testing.T) {
	callCount := 0
	sizer := &countingSizer{bytes: gb(25), counter: &callCount}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  40,
		CeilingGB: 60,
		Sizer:     sizer,
		CacheTTL:  0,
	})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := bm.Check(ctx); err != nil {
			t.Fatalf("Check %d: %v", i, err)
		}
	}
	if callCount != 3 {
		t.Errorf("sizer should be called every time when CacheTTL=0; called %d times", callCount)
	}
}

type countingSizer struct {
	bytes   int64
	counter *int
}

func (c *countingSizer) TotalBytes(_ context.Context) (int64, error) {
	(*c.counter)++
	return c.bytes, nil
}

func TestBudgetMonitor_StateTransitionCoverage(t *testing.T) {
	sizer := &fakeSizer{totalBytes: gb(10)}
	emitter := &fakeAuditEmitter{}
	bm := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:     40,
		CeilingGB:    60,
		Sizer:        sizer,
		AuditEmitter: emitter,
		CacheTTL:     0,
	})
	ctx := context.Background()

	steps := []struct {
		size          float64
		wantState     ecosystem.BudgetState
		wantBlockNew  bool
		wantBlockAll  bool
		wantNumEvents int
	}{
		{10, ecosystem.BudgetGreen, false, false, 0},
		{35, ecosystem.BudgetYellow, false, false, 1},
		{50, ecosystem.BudgetRed, true, false, 2},
		{75, ecosystem.BudgetOverflow, true, true, 3},
		{50, ecosystem.BudgetRed, true, false, 4},
		{10, ecosystem.BudgetGreen, false, false, 5},
	}

	for i, step := range steps {
		sizer.totalBytes = gb(step.size)
		status, err := bm.Check(ctx)
		if err != nil {
			t.Fatalf("step %d (%.0f GB): %v", i, step.size, err)
		}
		if status.State != step.wantState {
			t.Errorf("step %d (%.0f GB): state=%v, want %v", i, step.size, status.State, step.wantState)
		}
		if status.BlockNewIngest != step.wantBlockNew {
			t.Errorf("step %d (%.0f GB): BlockNewIngest=%v, want %v", i, step.size, status.BlockNewIngest, step.wantBlockNew)
		}
		if status.BlockAllWrites != step.wantBlockAll {
			t.Errorf("step %d (%.0f GB): BlockAllWrites=%v, want %v", i, step.size, status.BlockAllWrites, step.wantBlockAll)
		}
		if len(emitter.events) != step.wantNumEvents {
			t.Errorf("step %d (%.0f GB): events=%d, want %d (events: %v)", i, step.size, len(emitter.events), step.wantNumEvents, emitter.events)
		}
	}

	if _, err := bm.Check(ctx); err != nil {
		t.Fatalf("final Check: %v", err)
	}

}
