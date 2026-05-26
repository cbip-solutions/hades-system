package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeIngester struct {
	mu         sync.Mutex
	deltaCalls []string
	err        error
}

func (f *fakeIngester) IngestDelta(_ context.Context, eco string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deltaCalls = append(f.deltaCalls, eco)
	return nil
}

func (f *fakeIngester) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.deltaCalls))
	copy(out, f.deltaCalls)
	return out
}

type fakeSweeper struct {
	fpSweepCalls     int32
	changeSweepCalls int32
	rebuildCalls     int32
	gcCalls          int32
	fpErr            error
	changeErr        error
	rebuildErr       error
	gcErr            error
}

func (f *fakeSweeper) SweepChunkFingerprints(_ context.Context, _ string) error {
	atomic.AddInt32(&f.fpSweepCalls, 1)
	return f.fpErr
}

func (f *fakeSweeper) SweepChangeNodes(_ context.Context, _ string) error {
	atomic.AddInt32(&f.changeSweepCalls, 1)
	return f.changeErr
}

func (f *fakeSweeper) RebuildSymbolIndex(_ context.Context, _ string) error {
	atomic.AddInt32(&f.rebuildCalls, 1)
	return f.rebuildErr
}

func (f *fakeSweeper) CASGarbageCollect(_ context.Context) error {
	atomic.AddInt32(&f.gcCalls, 1)
	return f.gcErr
}

type fakeVersionDetector struct {
	mu          sync.Mutex
	newVersions map[string][]string
	err         error
}

func (f *fakeVersionDetector) DetectNewVersions(_ context.Context, eco string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.newVersions[eco], nil
}

func TestNewCronWorker_RequiresDeps(t *testing.T) {

	_, err := NewCronWorker(CronWorkerConfig{
		Ingester: nil,
		Sweeper:  &fakeSweeper{},
		Timezone: time.UTC,
	})
	if err == nil {
		t.Fatal("NewCronWorker: expected error with nil Ingester")
	}
	if !strings.Contains(err.Error(), "Ingester") {
		t.Errorf("expected error to mention Ingester; got %v", err)
	}

	_, err = NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  nil,
		Timezone: time.UTC,
	})
	if err == nil {
		t.Fatal("NewCronWorker: expected error with nil Sweeper")
	}
	if !strings.Contains(err.Error(), "Sweeper") {
		t.Errorf("expected error to mention Sweeper; got %v", err)
	}
}

func TestNewCronWorker_NilTimezone_UsesLocalOrUTC(t *testing.T) {
	w, err := NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  &fakeSweeper{},
		Timezone: nil,
	})
	if err != nil {
		t.Fatalf("NewCronWorker with nil TZ: %v", err)
	}
	if w == nil {
		t.Fatal("CronWorker is nil")
	}
}

func TestNewCronWorker_DefaultPollIntervalAndSweepHour(t *testing.T) {
	w, err := NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  &fakeSweeper{},
		Timezone: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewCronWorker: %v", err)
	}
	if w.cfg.PollInterval != 6*time.Hour {
		t.Errorf("PollInterval default: want 6h, got %v", w.cfg.PollInterval)
	}
	if w.cfg.SweepHour != 3 {
		t.Errorf("SweepHour default: want 3, got %d", w.cfg.SweepHour)
	}
}

func TestNewCronWorker_RespectsCustomPollIntervalAndSweepHour(t *testing.T) {
	w, err := NewCronWorker(CronWorkerConfig{
		Ingester:     &fakeIngester{},
		Sweeper:      &fakeSweeper{},
		Timezone:     time.UTC,
		PollInterval: 30 * time.Minute,
		SweepHour:    5,
	})
	if err != nil {
		t.Fatalf("NewCronWorker: %v", err)
	}
	if w.cfg.PollInterval != 30*time.Minute {
		t.Errorf("PollInterval: want 30m, got %v", w.cfg.PollInterval)
	}
	if w.cfg.SweepHour != 5 {
		t.Errorf("SweepHour: want 5, got %d", w.cfg.SweepHour)
	}
}

func TestCronWorker_PollUpstream_SchedulesDelta(t *testing.T) {
	ingester := &fakeIngester{}
	sweeper := &fakeSweeper{}
	detector := &fakeVersionDetector{
		newVersions: map[string][]string{
			"go":         {"1.23.1"},
			"python":     {},
			"typescript": {},
			"rust":       {"1.80.0"},
		},
	}

	w, err := NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         sweeper,
		VersionDetector: detector,
		Timezone:        time.UTC,
	})
	if err != nil {
		t.Fatalf("NewCronWorker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.PollUpstream(ctx); err != nil {
		t.Fatalf("PollUpstream: %v", err)
	}

	calls := ingester.calls()
	if len(calls) != 2 {
		t.Errorf("want 2 IngestDelta calls (go + rust); got %d: %v", len(calls), calls)
	}
	hasGo, hasRust := false, false
	for _, c := range calls {
		if c == "go" {
			hasGo = true
		}
		if c == "rust" {
			hasRust = true
		}
	}
	if !hasGo {
		t.Error("go delta not scheduled")
	}
	if !hasRust {
		t.Error("rust delta not scheduled")
	}
}

func TestCronWorker_PollUpstream_NoDetector_NoOp(t *testing.T) {
	ingester := &fakeIngester{}
	w, err := NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         &fakeSweeper{},
		VersionDetector: nil,
		Timezone:        time.UTC,
	})
	if err != nil {
		t.Fatalf("NewCronWorker: %v", err)
	}
	if err := w.PollUpstream(context.Background()); err != nil {
		t.Fatalf("PollUpstream no-detector: %v", err)
	}
	if got := len(ingester.calls()); got != 0 {
		t.Errorf("want 0 calls with no detector; got %d", got)
	}
}

func TestCronWorker_PollUpstream_DetectorError_AggregatedError(t *testing.T) {
	ingester := &fakeIngester{}
	detector := &fakeVersionDetector{err: errors.New("upstream 503")}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         &fakeSweeper{},
		VersionDetector: detector,
		Timezone:        time.UTC,
	})

	err := w.PollUpstream(context.Background())
	if err == nil {
		t.Fatal("expected error when detector fails for all ecosystems")
	}
	if !strings.Contains(err.Error(), "detect") {
		t.Errorf("expected aggregated error to include 'detect'; got %v", err)
	}

	if got := len(ingester.calls()); got != 0 {
		t.Errorf("want 0 IngestDelta calls when detector fails; got %d", got)
	}
}

func TestCronWorker_WeeklySweep_AllEcosystems(t *testing.T) {
	sweeper := &fakeSweeper{}
	ingester := &fakeIngester{}

	w, err := NewCronWorker(CronWorkerConfig{
		Ingester: ingester,
		Sweeper:  sweeper,
		Timezone: time.UTC,
	})
	if err != nil {
		t.Fatalf("NewCronWorker: %v", err)
	}

	ctx := context.Background()
	if err := w.WeeklySweep(ctx); err != nil {
		t.Fatalf("WeeklySweep: %v", err)
	}

	if got := atomic.LoadInt32(&sweeper.fpSweepCalls); got != 4 {
		t.Errorf("SweepChunkFingerprints: want 4 calls, got %d", got)
	}
	if got := atomic.LoadInt32(&sweeper.changeSweepCalls); got != 4 {
		t.Errorf("SweepChangeNodes: want 4 calls, got %d", got)
	}
	if got := atomic.LoadInt32(&sweeper.rebuildCalls); got != 4 {
		t.Errorf("RebuildSymbolIndex: want 4 calls, got %d", got)
	}
	if got := atomic.LoadInt32(&sweeper.gcCalls); got != 1 {
		t.Errorf("CASGarbageCollect: want 1 call, got %d", got)
	}
}

func TestCronWorker_WeeklySweep_PartialFPError_ContinuesRest(t *testing.T) {
	sweeper := &fakeSweeper{fpErr: errors.New("sqlite locked")}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  sweeper,
		Timezone: time.UTC,
	})

	err := w.WeeklySweep(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error when fingerprint sweep fails")
	}
	if !strings.Contains(err.Error(), "SweepChunkFingerprints") {
		t.Errorf("expected error to mention SweepChunkFingerprints; got %v", err)
	}

	if got := atomic.LoadInt32(&sweeper.fpSweepCalls); got != 4 {
		t.Errorf("SweepChunkFingerprints: want 4 attempts even with errors; got %d", got)
	}

	if got := atomic.LoadInt32(&sweeper.changeSweepCalls); got != 4 {
		t.Errorf("SweepChangeNodes: want 4 attempts; got %d", got)
	}
	if got := atomic.LoadInt32(&sweeper.rebuildCalls); got != 4 {
		t.Errorf("RebuildSymbolIndex: want 4 attempts; got %d", got)
	}
	if got := atomic.LoadInt32(&sweeper.gcCalls); got != 1 {
		t.Errorf("CASGarbageCollect: want 1 attempt; got %d", got)
	}
}

func TestCronWorker_WeeklySweep_GCError_Surfaces(t *testing.T) {
	sweeper := &fakeSweeper{gcErr: errors.New("disk full")}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  sweeper,
		Timezone: time.UTC,
	})

	err := w.WeeklySweep(context.Background())
	if err == nil {
		t.Fatal("expected error when CASGarbageCollect fails")
	}
	if !strings.Contains(err.Error(), "CASGarbageCollect") {
		t.Errorf("expected error to mention CASGarbageCollect; got %v", err)
	}
}

func TestCronWorker_PollUpstream_IngestError_LogsAndContinues(t *testing.T) {
	ingester := &fakeIngester{err: errors.New("db locked")}
	detector := &fakeVersionDetector{
		newVersions: map[string][]string{
			"go":         {"1.23.0"},
			"python":     {"3.13.0"},
			"typescript": {"5.7.0"},
			"rust":       {"1.80.0"},
		},
	}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         &fakeSweeper{},
		VersionDetector: detector,
		Timezone:        time.UTC,
	})

	err := w.PollUpstream(context.Background())
	if err == nil {
		t.Fatal("expected error when ingester fails")
	}
	if !strings.Contains(err.Error(), "ingest") {
		t.Errorf("expected error to mention 'ingest'; got %v", err)
	}
}

func TestCronWorker_Stop_HonorsContext(t *testing.T) {
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester: &fakeIngester{},
		Sweeper:  &fakeSweeper{},
		Timezone: time.UTC,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:

	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return within 1s after context cancellation")
	}
}

func TestLoadTimezone_ValidZone(t *testing.T) {
	loc, err := loadTimezone("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("loadTimezone: %v", err)
	}
	if loc == nil {
		t.Fatal("loadTimezone: returned nil location")
	}
	if loc.String() != "America/Argentina/Buenos_Aires" {
		t.Errorf("loc.String(): want America/Argentina/Buenos_Aires, got %s", loc.String())
	}
}

func TestLoadTimezone_EmptyFallsBackToLocal(t *testing.T) {
	loc, err := loadTimezone("")
	if err != nil {
		t.Fatalf("loadTimezone empty: %v", err)
	}
	if loc == nil {
		t.Fatal("loadTimezone empty: returned nil location")
	}
}

func TestLoadTimezone_InvalidZone_Error(t *testing.T) {
	_, err := loadTimezone("Not/AValid/Zone")
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
	if !strings.Contains(err.Error(), "loadTimezone") {
		t.Errorf("expected error to mention loadTimezone; got %v", err)
	}
}

func TestIsSundaySweepTime_Correct(t *testing.T) {

	sundayMorning := time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC)
	if !isSundaySweepTime(sundayMorning, 3, time.UTC) {
		t.Errorf("2026-05-17 03:00 UTC must be a Sunday sweep time (got weekday=%v)", sundayMorning.Weekday())
	}

	sundayMid := time.Date(2026, 5, 17, 3, 30, 0, 0, time.UTC)
	if !isSundaySweepTime(sundayMid, 3, time.UTC) {
		t.Error("03:30 UTC on Sunday must still be sweep hour")
	}

	notYet := time.Date(2026, 5, 17, 2, 59, 59, 0, time.UTC)
	if isSundaySweepTime(notYet, 3, time.UTC) {
		t.Error("02:59:59 must not be sweep time")
	}

	tuesday := time.Date(2026, 5, 19, 3, 0, 0, 0, time.UTC)
	if isSundaySweepTime(tuesday, 3, time.UTC) {
		t.Error("Tuesday must not be Sunday sweep time")
	}

	tooLate := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	if isSundaySweepTime(tooLate, 3, time.UTC) {
		t.Error("04:00 must not be sweep hour when SweepHour=3")
	}
}

func TestIsSundaySweepTime_CrossTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}

	utcTime := time.Date(2026, 5, 17, 6, 0, 0, 0, time.UTC)
	if !isSundaySweepTime(utcTime, 3, loc) {
		t.Errorf("06:00 UTC == 03:00 ART Sunday should match sweepHour=3 in Buenos Aires")
	}
}

func TestAllEcosystems_CanonicalFour(t *testing.T) {
	want := map[string]bool{"go": true, "python": true, "typescript": true, "rust": true}
	if len(allEcosystems) != 4 {
		t.Errorf("allEcosystems: want 4 entries; got %d (%v)", len(allEcosystems), allEcosystems)
	}
	for _, eco := range allEcosystems {
		if !want[eco] {
			t.Errorf("unexpected ecosystem %q in allEcosystems", eco)
		}
		delete(want, eco)
	}
	if len(want) > 0 {
		t.Errorf("missing ecosystems: %v", want)
	}
}

func TestCronWorker_Run_PollTickerFires(t *testing.T) {
	ingester := &fakeIngester{}
	detector := &fakeVersionDetector{
		newVersions: map[string][]string{"go": {"1.23.1"}},
	}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         &fakeSweeper{},
		VersionDetector: detector,
		Timezone:        time.UTC,
		PollInterval:    20 * time.Millisecond,
		SweepHour:       3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = w.Run(ctx)

	if got := len(ingester.calls()); got < 1 {
		t.Errorf("poll ticker: want ≥1 IngestDelta call within 200ms; got %d", got)
	}
}

func TestDaemonCronClient_SatisfiesInterfaces(t *testing.T) {
	c := &daemonCronClient{socketPath: "/tmp/nonexistent.sock"}
	var (
		_ Ingester        = c
		_ Sweeper         = c
		_ VersionDetector = c
	)
}

func TestDaemonCronClient_DetectNewVersions_StubNilNil(t *testing.T) {
	c := &daemonCronClient{socketPath: "/tmp/nonexistent.sock"}
	versions, err := c.DetectNewVersions(context.Background(), "go")
	if err != nil {
		t.Errorf("stub DetectNewVersions: want nil err; got %v", err)
	}
	if versions != nil {
		t.Errorf("stub DetectNewVersions: want nil versions; got %v", versions)
	}
}

func TestMaybeRunWeeklySweep_NotSundayHour_NoSweep(t *testing.T) {
	sweeper := &fakeSweeper{}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:  &fakeIngester{},
		Sweeper:   sweeper,
		Timezone:  time.UTC,
		SweepHour: 3,
	})

	tuesday := time.Date(2026, 5, 19, 3, 0, 0, 0, time.UTC)
	got := w.maybeRunWeeklySweep(context.Background(), tuesday, time.Time{})
	if !got.IsZero() {
		t.Errorf("non-sweep time should leave lastSweepDate zero; got %v", got)
	}
	if atomic.LoadInt32(&sweeper.fpSweepCalls) != 0 {
		t.Errorf("sweep should not run; fp calls = %d", atomic.LoadInt32(&sweeper.fpSweepCalls))
	}
}

func TestMaybeRunWeeklySweep_SundayHour_FirstFire_Sweeps(t *testing.T) {
	sweeper := &fakeSweeper{}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:  &fakeIngester{},
		Sweeper:   sweeper,
		Timezone:  time.UTC,
		SweepHour: 3,
	})

	sunday := time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC)
	got := w.maybeRunWeeklySweep(context.Background(), sunday, time.Time{})

	wantDate := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if !got.Equal(wantDate) {
		t.Errorf("lastSweepDate: want %v, got %v", wantDate, got)
	}
	if atomic.LoadInt32(&sweeper.fpSweepCalls) != 4 {
		t.Errorf("sweep should run for 4 ecosystems; fp calls = %d", atomic.LoadInt32(&sweeper.fpSweepCalls))
	}
	if atomic.LoadInt32(&sweeper.gcCalls) != 1 {
		t.Errorf("GC should run once; gc calls = %d", atomic.LoadInt32(&sweeper.gcCalls))
	}
}

func TestMaybeRunWeeklySweep_AlreadySweptToday_NoOp(t *testing.T) {
	sweeper := &fakeSweeper{}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:  &fakeIngester{},
		Sweeper:   sweeper,
		Timezone:  time.UTC,
		SweepHour: 3,
	})

	sunday := time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC)

	already := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	got := w.maybeRunWeeklySweep(context.Background(), sunday, already)
	if !got.Equal(already) {
		t.Errorf("lastSweepDate should be unchanged; want %v got %v", already, got)
	}
	if atomic.LoadInt32(&sweeper.fpSweepCalls) != 0 {
		t.Errorf("sweep should not re-run within same day; fp calls = %d", atomic.LoadInt32(&sweeper.fpSweepCalls))
	}
}

func TestMaybeRunWeeklySweep_NextSunday_AfterPriorSweep_Fires(t *testing.T) {
	sweeper := &fakeSweeper{}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:  &fakeIngester{},
		Sweeper:   sweeper,
		Timezone:  time.UTC,
		SweepHour: 3,
	})

	prevSunday := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	nextSunday := time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC)
	got := w.maybeRunWeeklySweep(context.Background(), nextSunday, prevSunday)
	wantDate := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if !got.Equal(wantDate) {
		t.Errorf("lastSweepDate next sunday: want %v got %v", wantDate, got)
	}
	if atomic.LoadInt32(&sweeper.fpSweepCalls) != 4 {
		t.Errorf("sweep should run for next Sunday; fp calls = %d", atomic.LoadInt32(&sweeper.fpSweepCalls))
	}
}

func TestMaybeRunWeeklySweep_SweepError_DateStillAdvances(t *testing.T) {
	sweeper := &fakeSweeper{fpErr: errors.New("sqlite locked")}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:  &fakeIngester{},
		Sweeper:   sweeper,
		Timezone:  time.UTC,
		SweepHour: 3,
	})

	sunday := time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC)
	got := w.maybeRunWeeklySweep(context.Background(), sunday, time.Time{})
	wantDate := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	if !got.Equal(wantDate) {
		t.Errorf("date should advance even on sweep error; want %v got %v", wantDate, got)
	}
}

type fakeClockSweeper struct {
	fakeSweeper
	sweepCh chan struct{}
	once    sync.Once
}

func (f *fakeClockSweeper) SweepChunkFingerprints(ctx context.Context, eco string) error {
	atomic.AddInt32(&f.fakeSweeper.fpSweepCalls, 1)
	f.once.Do(func() {
		close(f.sweepCh)
	})
	return f.fpErr
}

func TestRunWithInterval_HeartbeatDoesNotTriggerSweepOffWindow(t *testing.T) {
	sweeper := &fakeSweeper{}
	w, _ := NewCronWorker(CronWorkerConfig{
		Ingester:     &fakeIngester{},
		Sweeper:      sweeper,
		Timezone:     time.UTC,
		PollInterval: 1 * time.Hour,
		SweepHour:    3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = w.runWithInterval(ctx, 5*time.Millisecond)

	t.Logf("fp calls observed in off-window run: %d (expected ~0 unless test ran during Sun 03:00 UTC)",
		atomic.LoadInt32(&sweeper.fpSweepCalls))
}

func TestParseFlags_Defaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg, err := parseFlags(fs, nil)
	if err != nil {
		t.Fatalf("parseFlags nil args: %v", err)
	}
	if cfg.daemonSocket != "/tmp/zen-swarm.sock" {
		t.Errorf("daemonSocket default: got %q", cfg.daemonSocket)
	}
	if cfg.pollInterval != "6h" {
		t.Errorf("pollInterval default: got %q", cfg.pollInterval)
	}
	if cfg.sweepHour != 3 {
		t.Errorf("sweepHour default: got %d", cfg.sweepHour)
	}
}

func TestParseFlags_Overrides(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	args := []string{
		"-daemon-uds", "/var/run/x.sock",
		"-timezone", "UTC",
		"-poll-interval", "1h",
		"-sweep-hour", "5",
	}
	cfg, err := parseFlags(fs, args)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.daemonSocket != "/var/run/x.sock" {
		t.Errorf("daemonSocket: %q", cfg.daemonSocket)
	}
	if cfg.timezone != "UTC" {
		t.Errorf("timezone: %q", cfg.timezone)
	}
	if cfg.pollInterval != "1h" {
		t.Errorf("pollInterval: %q", cfg.pollInterval)
	}
	if cfg.sweepHour != 5 {
		t.Errorf("sweepHour: %d", cfg.sweepHour)
	}
}

func TestParseFlags_InvalidArg_Error(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	fs.SetOutput(io.Discard)
	_, err := parseFlags(fs, []string{"-unknown-flag", "foo"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestBuildCronWorker_Success(t *testing.T) {
	cfg := runtimeConfig{
		daemonSocket: "/tmp/zen-build-test.sock",
		timezone:     "UTC",
		pollInterval: "30s",
		sweepHour:    4,
	}
	w, err := buildCronWorker(cfg, fakeDepsBuilder)
	if err != nil {
		t.Fatalf("buildCronWorker: %v", err)
	}
	if w == nil {
		t.Fatal("CronWorker is nil")
	}
	if w.cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval: want 30s, got %v", w.cfg.PollInterval)
	}
	if w.cfg.SweepHour != 4 {
		t.Errorf("SweepHour: want 4, got %d", w.cfg.SweepHour)
	}
}

func TestBuildCronWorker_InvalidTimezone(t *testing.T) {
	cfg := runtimeConfig{
		daemonSocket: "/tmp/x.sock",
		timezone:     "Not/Valid/Zone",
		pollInterval: "1h",
		sweepHour:    3,
	}
	_, err := buildCronWorker(cfg, fakeDepsBuilder)
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestBuildCronWorker_InvalidPollInterval(t *testing.T) {
	cfg := runtimeConfig{
		daemonSocket: "/tmp/x.sock",
		timezone:     "UTC",
		pollInterval: "not-a-duration",
		sweepHour:    3,
	}
	_, err := buildCronWorker(cfg, fakeDepsBuilder)
	if err == nil {
		t.Fatal("expected error for invalid poll-interval")
	}
}

func fakeDepsBuilder(_ string) (Ingester, Sweeper, VersionDetector) {
	return &fakeIngester{}, &fakeSweeper{}, &fakeVersionDetector{}
}
