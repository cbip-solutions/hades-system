// go:build integration && cgo

package ecosystem_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

var cronEcosystems = []string{"go", "python", "typescript", "rust"}

func init() {
	canonical := make([]string, len(ecosystem.AllEcosystems))
	for i, e := range ecosystem.AllEcosystems {
		canonical[i] = string(e)
	}
	if strings.Join(canonical, ",") != strings.Join(cronEcosystems, ",") {
		panic("cron_failure_test: cronEcosystems is out of sync with ecosystem.AllEcosystems — update both to match")
	}
}

type cronIngester interface {
	IngestDelta(ctx context.Context, eco string, version string) error
}

type cronVersionDetector interface {
	DetectNewVersions(ctx context.Context, eco string) ([]string, error)
}

type cronEcoSweeper interface {
	SweepChunkFingerprints(ctx context.Context, eco string) error
	SweepChangeNodes(ctx context.Context, eco string) error
	RebuildSymbolIndex(ctx context.Context, eco string) error
	CASGarbageCollect(ctx context.Context) error
}

type cronWorkerFacade struct {
	ingester cronIngester
	detector cronVersionDetector
	sweeper  cronEcoSweeper
}

func (c *cronWorkerFacade) pollUpstream(ctx context.Context) error {
	var errs []error
	for _, eco := range cronEcosystems {

		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		versions, err := c.detector.DetectNewVersions(ctx, eco)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, v := range versions {
			if err := c.ingester.IngestDelta(ctx, eco, v); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *cronWorkerFacade) weeklySweep(ctx context.Context) error {
	var errs []error
	for _, eco := range cronEcosystems {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		if err := c.sweeper.SweepChunkFingerprints(ctx, eco); err != nil {
			errs = append(errs, err)
		}
		if err := c.sweeper.SweepChangeNodes(ctx, eco); err != nil {
			errs = append(errs, err)
		}
		if err := c.sweeper.RebuildSymbolIndex(ctx, eco); err != nil {
			errs = append(errs, err)
		}
	}
	if err := c.sweeper.CASGarbageCollect(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

type noopIngester struct {
	callCount int32
}

func (n *noopIngester) IngestDelta(_ context.Context, _ string, _ string) error {
	atomic.AddInt32(&n.callCount, 1)
	return nil
}

type partialFailIngester struct {
	mu       sync.Mutex
	callsPer map[string]int
	failEco  string
}

func newPartialFailIngester(failEco string) *partialFailIngester {
	return &partialFailIngester{
		callsPer: make(map[string]int),
		failEco:  failEco,
	}
}

func (p *partialFailIngester) IngestDelta(_ context.Context, eco string, _ string) error {
	p.mu.Lock()
	p.callsPer[eco]++
	p.mu.Unlock()
	if eco == p.failEco {
		return errors.New("simulated ingest failure for " + eco)
	}
	return nil
}

func (p *partialFailIngester) snapshotCalls() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.callsPer))
	for k, v := range p.callsPer {
		out[k] = v
	}
	return out
}

type allVersionsDetector struct {
	mu       sync.Mutex
	callsPer map[string]int
}

func newAllVersionsDetector() *allVersionsDetector {
	return &allVersionsDetector{callsPer: make(map[string]int)}
}

func (a *allVersionsDetector) DetectNewVersions(_ context.Context, eco string) ([]string, error) {
	a.mu.Lock()
	a.callsPer[eco]++
	a.mu.Unlock()

	return []string{"v99.0.0"}, nil
}

func (a *allVersionsDetector) snapshotCalls() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]int, len(a.callsPer))
	for k, v := range a.callsPer {
		out[k] = v
	}
	return out
}

type partialFailDetector struct {
	mu       sync.Mutex
	failEco  string
	callsPer map[string]int
}

func newPartialFailDetector(failEco string) *partialFailDetector {
	return &partialFailDetector{failEco: failEco, callsPer: make(map[string]int)}
}

func (p *partialFailDetector) DetectNewVersions(_ context.Context, eco string) ([]string, error) {
	p.mu.Lock()
	p.callsPer[eco]++
	p.mu.Unlock()
	if eco == p.failEco {
		return nil, errors.New("simulated upstream-detector failure for " + eco)
	}
	return []string{"v1.0.0"}, nil
}

func (p *partialFailDetector) snapshotCalls() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.callsPer))
	for k, v := range p.callsPer {
		out[k] = v
	}
	return out
}

type noopCronSweeper struct{}

func (noopCronSweeper) SweepChunkFingerprints(context.Context, string) error { return nil }
func (noopCronSweeper) SweepChangeNodes(context.Context, string) error       { return nil }
func (noopCronSweeper) RebuildSymbolIndex(context.Context, string) error     { return nil }
func (noopCronSweeper) CASGarbageCollect(context.Context) error              { return nil }

type failingEcoSweeper struct {
	mu              sync.Mutex
	failEco         string
	failPhase       string
	fpCalls         int32
	cnCalls         int32
	siCalls         int32
	gcCalls         int32
	perEcoPhaseHits map[string]int
}

func newFailingEcoSweeper(failEco, failPhase string) *failingEcoSweeper {
	return &failingEcoSweeper{
		failEco:         failEco,
		failPhase:       failPhase,
		perEcoPhaseHits: make(map[string]int),
	}
}

func (f *failingEcoSweeper) hit(label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perEcoPhaseHits[label]++
}

func (f *failingEcoSweeper) SweepChunkFingerprints(_ context.Context, eco string) error {
	atomic.AddInt32(&f.fpCalls, 1)
	f.hit(eco + ":fingerprint")
	if eco == f.failEco && f.failPhase == "fingerprint" {
		return errors.New("fingerprint sweep failed for " + eco)
	}
	return nil
}

func (f *failingEcoSweeper) SweepChangeNodes(_ context.Context, eco string) error {
	atomic.AddInt32(&f.cnCalls, 1)
	f.hit(eco + ":changenodes")
	if eco == f.failEco && f.failPhase == "changenodes" {
		return errors.New("changenodes sweep failed for " + eco)
	}
	return nil
}

func (f *failingEcoSweeper) RebuildSymbolIndex(_ context.Context, eco string) error {
	atomic.AddInt32(&f.siCalls, 1)
	f.hit(eco + ":symbols")
	if eco == f.failEco && f.failPhase == "symbols" {
		return errors.New("symbols rebuild failed for " + eco)
	}
	return nil
}

func (f *failingEcoSweeper) CASGarbageCollect(_ context.Context) error {
	atomic.AddInt32(&f.gcCalls, 1)
	return nil
}

type slowCancelSweeper struct {
	cancel context.CancelFunc
	called int32
}

func (s *slowCancelSweeper) SweepChunkFingerprints(ctx context.Context, _ string) error {
	if atomic.AddInt32(&s.called, 1) == 1 {

		s.cancel()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}
func (s *slowCancelSweeper) SweepChangeNodes(ctx context.Context, _ string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
func (s *slowCancelSweeper) RebuildSymbolIndex(ctx context.Context, _ string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
func (s *slowCancelSweeper) CASGarbageCollect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type fixedSizer struct{ bytes int64 }

func (f *fixedSizer) TotalBytes(_ context.Context) (int64, error) { return f.bytes, nil }

func TestCronWorker_PollUpstream_PartialIngestFailure_OthersComplete(t *testing.T) {
	ingester := newPartialFailIngester("python")
	detector := newAllVersionsDetector()
	worker := &cronWorkerFacade{
		ingester: ingester,
		detector: detector,
		sweeper:  noopCronSweeper{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := worker.pollUpstream(ctx)
	if err == nil {
		t.Fatal("expected aggregated error when one ecosystem ingest fails; got nil")
	}

	if !strings.Contains(err.Error(), "python") {
		t.Errorf("aggregated error must surface the failing ecosystem name; got %q", err.Error())
	}

	detectorCalls := detector.snapshotCalls()
	for _, eco := range cronEcosystems {
		if detectorCalls[eco] != 1 {
			t.Errorf("DetectNewVersions(%s): got %d calls, want 1", eco, detectorCalls[eco])
		}
	}
	ingestCalls := ingester.snapshotCalls()
	for _, eco := range cronEcosystems {
		if ingestCalls[eco] != 1 {
			t.Errorf("IngestDelta(%s): got %d calls, want 1 (partial-failure must NOT short-circuit)",
				eco, ingestCalls[eco])
		}
	}
}

func TestCronWorker_PollUpstream_PartialDetectorFailure_OthersComplete(t *testing.T) {
	ingester := &noopIngester{}
	detector := newPartialFailDetector("typescript")
	worker := &cronWorkerFacade{
		ingester: ingester,
		detector: detector,
		sweeper:  noopCronSweeper{},
	}

	err := worker.pollUpstream(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error when one ecosystem detector fails")
	}
	if !strings.Contains(err.Error(), "typescript") {
		t.Errorf("aggregated error must surface failing ecosystem; got %q", err.Error())
	}

	for _, eco := range cronEcosystems {
		if detector.snapshotCalls()[eco] != 1 {
			t.Errorf("DetectNewVersions(%s): got %d, want 1", eco, detector.snapshotCalls()[eco])
		}
	}

	if got, want := atomic.LoadInt32(&ingester.callCount), int32(3); got != want {
		t.Errorf("IngestDelta call count: got %d, want %d (failed detector must skip ingest)",
			got, want)
	}
}

func TestCronWorker_WeeklySweep_OneEcoFingerprintFails_OthersContinue(t *testing.T) {
	sweeper := newFailingEcoSweeper("typescript", "fingerprint")
	worker := &cronWorkerFacade{
		ingester: &noopIngester{},
		detector: newAllVersionsDetector(),
		sweeper:  sweeper,
	}

	err := worker.weeklySweep(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error when one ecosystem sweep fails")
	}
	if !strings.Contains(err.Error(), "typescript") {
		t.Errorf("aggregated error must surface failing ecosystem; got %q", err.Error())
	}

	if got := atomic.LoadInt32(&sweeper.fpCalls); got != int32(len(cronEcosystems)) {
		t.Errorf("SweepChunkFingerprints calls: got %d, want %d", got, len(cronEcosystems))
	}

	if got := atomic.LoadInt32(&sweeper.cnCalls); got != int32(len(cronEcosystems)) {
		t.Errorf("SweepChangeNodes calls: got %d, want %d (later phases must still run)",
			got, len(cronEcosystems))
	}
	if got := atomic.LoadInt32(&sweeper.siCalls); got != int32(len(cronEcosystems)) {
		t.Errorf("RebuildSymbolIndex calls: got %d, want %d", got, len(cronEcosystems))
	}

	if got := atomic.LoadInt32(&sweeper.gcCalls); got != 1 {
		t.Errorf("CASGarbageCollect calls: got %d, want 1", got)
	}
}

func TestCronWorker_WeeklySweep_OneEcoChangeNodesFails_PipelineContinues(t *testing.T) {
	sweeper := newFailingEcoSweeper("go", "changenodes")
	worker := &cronWorkerFacade{
		ingester: &noopIngester{},
		detector: newAllVersionsDetector(),
		sweeper:  sweeper,
	}

	err := worker.weeklySweep(context.Background())
	if err == nil {
		t.Fatal("expected aggregated error from change-nodes failure")
	}
	if !strings.Contains(err.Error(), "changenodes") {
		t.Errorf("aggregated error should mention failing phase; got %q", err.Error())
	}

	if got := atomic.LoadInt32(&sweeper.siCalls); got != int32(len(cronEcosystems)) {
		t.Errorf("RebuildSymbolIndex calls: got %d want %d (change-node failure must not block symbol rebuild)",
			got, len(cronEcosystems))
	}
}

func TestCronWorker_ContextCancel_MidSweep_ExitsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slow := &slowCancelSweeper{cancel: cancel}
	worker := &cronWorkerFacade{
		ingester: &noopIngester{},
		detector: newAllVersionsDetector(),
		sweeper:  slow,
	}

	type result struct {
		err     error
		elapsed time.Duration
	}
	resCh := make(chan result, 1)
	go func() {
		start := time.Now()
		err := worker.weeklySweep(ctx)
		resCh <- result{err: err, elapsed: time.Since(start)}
	}()

	select {
	case res := <-resCh:

		if res.elapsed >= 2*time.Second {
			t.Errorf("weeklySweep took %v after cancellation; expected <2s", res.elapsed)
		}

		if res.err == nil {
			t.Error("weeklySweep returned nil after cancellation; expected context.Canceled")
		}
		if res.err != nil && !errors.Is(res.err, context.Canceled) {
			t.Errorf("weeklySweep err: %v; expected to wrap context.Canceled", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("weeklySweep did not exit within 5s after context cancellation")
	}
}

func TestBudgetMonitor_OverflowBlocksIngest(t *testing.T) {
	const targetGB = 40.0
	const ceilingGB = 60.0

	bigSizer := &fixedSizer{bytes: int64(65 * (1 << 30))}
	monitor := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  targetGB,
		CeilingGB: ceilingGB,
		Sizer:     bigSizer,
	})

	status, err := monitor.Check(context.Background())
	if err != nil {
		t.Fatalf("BudgetMonitor.Check: %v", err)
	}
	if status.State != ecosystem.BudgetOverflow {
		t.Errorf("inv-zen-199: 65 GB must classify as BudgetOverflow; got %s", status.State.String())
	}
	if !status.BlockAllWrites {
		t.Errorf("inv-zen-199 VIOLATED: 65 GB must BlockAllWrites=true; got false")
	}
	if !status.BlockNewIngest {
		t.Errorf("inv-zen-199 VIOLATED: 65 GB must BlockNewIngest=true; got false")
	}
}

func TestBudgetMonitor_RedBlocksNewIngestOnly(t *testing.T) {
	const targetGB = 40.0
	const ceilingGB = 60.0

	monitor := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  targetGB,
		CeilingGB: ceilingGB,
		Sizer:     &fixedSizer{bytes: int64(50 * (1 << 30))},
	})

	status, err := monitor.Check(context.Background())
	if err != nil {
		t.Fatalf("BudgetMonitor.Check: %v", err)
	}
	if status.State != ecosystem.BudgetRed {
		t.Errorf("50 GB must classify as BudgetRed; got %s", status.State.String())
	}
	if !status.BlockNewIngest {
		t.Error("BudgetRed must BlockNewIngest=true")
	}
	if status.BlockAllWrites {
		t.Error("BudgetRed must NOT BlockAllWrites — existing-version updates remain OK")
	}
}

func TestBudgetMonitor_GreenAllowsAllWrites(t *testing.T) {
	const targetGB = 40.0
	const ceilingGB = 60.0
	monitor := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
		TargetGB:  targetGB,
		CeilingGB: ceilingGB,
		Sizer:     &fixedSizer{bytes: int64(10 * (1 << 30))},
	})

	status, err := monitor.Check(context.Background())
	if err != nil {
		t.Fatalf("BudgetMonitor.Check: %v", err)
	}
	if status.State != ecosystem.BudgetGreen {
		t.Errorf("10 GB must classify as BudgetGreen; got %s", status.State.String())
	}
	if status.BlockNewIngest || status.BlockAllWrites {
		t.Errorf("BudgetGreen must permit all writes; got BlockNewIngest=%v BlockAllWrites=%v",
			status.BlockNewIngest, status.BlockAllWrites)
	}
}

func TestCronWorker_PollThenSweep_PartialFailureDoesNotAbortNextStage(t *testing.T) {
	ingester := newPartialFailIngester("rust")
	detector := newAllVersionsDetector()
	sweeper := newFailingEcoSweeper("", "")
	worker := &cronWorkerFacade{
		ingester: ingester,
		detector: detector,
		sweeper:  sweeper,
	}

	pollErr := worker.pollUpstream(context.Background())
	if pollErr == nil {
		t.Fatal("expected poll error from rust ingester failure")
	}

	if sweepErr := worker.weeklySweep(context.Background()); sweepErr != nil {
		t.Errorf("sweep must not be aborted by prior poll error; got %v", sweepErr)
	}

	if got := atomic.LoadInt32(&sweeper.fpCalls); got != int32(len(cronEcosystems)) {
		t.Errorf("sweep visited %d ecosystems; want %d", got, len(cronEcosystems))
	}
}
