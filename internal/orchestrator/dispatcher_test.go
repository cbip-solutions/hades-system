package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type fakeWorkforce struct {
	mu sync.Mutex

	spawnCalls atomic.Int32
	abortCalls atomic.Int32

	results      []orchestrator.WorkerResult
	spawnErr     error
	blockResults bool
	emitDelay    time.Duration
	lastSpawnReq orchestrator.SpawnRequest
}

func (f *fakeWorkforce) SpawnWorkers(ctx context.Context, req orchestrator.SpawnRequest) (<-chan orchestrator.WorkerResult, error) {
	f.spawnCalls.Add(1)
	f.mu.Lock()
	f.lastSpawnReq = req
	spawnErr := f.spawnErr
	results := f.results
	block := f.blockResults
	delay := f.emitDelay
	f.mu.Unlock()

	if spawnErr != nil {
		return nil, spawnErr
	}
	out := make(chan orchestrator.WorkerResult, len(results))
	go func() {
		defer close(out)
		if block {
			<-ctx.Done()
			return
		}
		for _, r := range results {
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- r:
			}
		}
	}()
	return out, nil
}

func (f *fakeWorkforce) AbortAll(_ context.Context) error {
	f.abortCalls.Add(1)
	return nil
}

func countByType(records []eventlog.Record, et eventlog.EventType) int {
	n := 0
	for _, r := range records {
		if r.EventType == et {
			n++
		}
	}
	return n
}

func newDispatcherForTest(t *testing.T, wf *fakeWorkforce) (orchestrator.Dispatcher, *eventlog.Log, *fakePool) {
	t.Helper()
	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	pool := &fakePool{}
	disp, err := orchestrator.NewDispatcher(orchestrator.DispatcherConfig{
		Clock:     clk,
		EventLog:  memLog,
		Pool:      pool,
		Workforce: wf,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return disp, memLog, pool
}

func dispatchRequestFor(width, depth int) orchestrator.DispatchRequest {
	return orchestrator.DispatchRequest{
		SessionID: testSessionID,
		ProjectID: testProjectID,
		Doctrine:  "max-scope",
		Width:     width,
		Depth:     depth,
		Spec:      defaultSpec(),
	}
}

func queryRecords(t *testing.T, l *eventlog.Log, sessionID string) []eventlog.Record {
	t.Helper()
	recs, err := l.Query(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("eventlog.Query: %v", err)
	}
	return recs
}

func TestDispatcherDispatchHappyPath(t *testing.T) {
	wf := &fakeWorkforce{
		results: []orchestrator.WorkerResult{
			{WorkerID: "w0", Status: "ok"},
			{WorkerID: "w1", Status: "ok"},
			{WorkerID: "w2", Status: "ok"},
		},
	}
	disp, memLog, pool := newDispatcherForTest(t, wf)

	res, err := disp.Dispatch(context.Background(), dispatchRequestFor(3, 2))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.WorkersSpawned != 3 {
		t.Fatalf("WorkersSpawned=%d, want 3", res.WorkersSpawned)
	}
	if res.Completed != 3 {
		t.Fatalf("Completed=%d, want 3", res.Completed)
	}
	if res.Errors != 0 || res.Aborted != 0 {
		t.Fatalf("unexpected non-zero error/aborted counters: %+v", res)
	}
	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked: %d still held", pool.Leased())
	}
	if wf.spawnCalls.Load() != 1 {
		t.Fatalf("spawn calls = %d, want 1", wf.spawnCalls.Load())
	}
	if wf.abortCalls.Load() != 0 {
		t.Fatalf("AbortAll must not run on happy path, got %d", wf.abortCalls.Load())
	}

	recs := queryRecords(t, memLog, testSessionID)
	if got := countByType(recs, eventlog.EvtWorkerDispatched); got != 3 {
		t.Fatalf("EvtWorkerDispatched count = %d, want 3", got)
	}
	if got := countByType(recs, eventlog.EvtWorkerCheckpoint); got != 3 {
		t.Fatalf("EvtWorkerCheckpoint count = %d, want 3", got)
	}
}

func TestDispatcherWorktreePoolExhausted(t *testing.T) {
	wf := &fakeWorkforce{}
	disp, _, pool := newDispatcherForTest(t, wf)
	pool.leaseErr = worktreepool.ErrPoolExhausted

	_, err := disp.Dispatch(context.Background(), dispatchRequestFor(2, 1))
	if !errors.Is(err, worktreepool.ErrPoolExhausted) {
		t.Fatalf("Dispatch: want ErrPoolExhausted, got %v", err)
	}
	if wf.spawnCalls.Load() != 0 {
		t.Fatalf("workforce spawned despite pool exhaustion: %d", wf.spawnCalls.Load())
	}
	if pool.Leased() != 0 {
		t.Fatalf("partial leases leaked: %d", pool.Leased())
	}
}

func TestDispatcherCancelTriggersAbortAll(t *testing.T) {
	wf := &fakeWorkforce{
		blockResults: true,
	}
	disp, _, pool := newDispatcherForTest(t, wf)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {

		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := disp.Dispatch(ctx, dispatchRequestFor(2, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch: want context.Canceled, got %v", err)
	}
	if got := wf.abortCalls.Load(); got != 1 {
		t.Fatalf("AbortAll calls = %d, want exactly 1", got)
	}
	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked on cancel: %d", pool.Leased())
	}
}

func TestDispatcherWorkforceSpawnError(t *testing.T) {
	spawnErr := errors.New("workforce spawn boom")
	wf := &fakeWorkforce{spawnErr: spawnErr}
	disp, _, pool := newDispatcherForTest(t, wf)

	_, err := disp.Dispatch(context.Background(), dispatchRequestFor(2, 1))
	if err == nil {
		t.Fatalf("Dispatch: want error, got nil")
	}
	if !errors.Is(err, spawnErr) {
		t.Fatalf("Dispatch: spawn error chain not preserved: %v", err)
	}

	if got, want := err.Error(), "workforce spawn:"; !strings.Contains(got, want) {
		t.Fatalf("Dispatch error = %q, want prefix %q", got, want)
	}
	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked after spawn error: %d", pool.Leased())
	}
}

func TestDispatcherInvalidWidth(t *testing.T) {
	wf := &fakeWorkforce{}
	disp, _, pool := newDispatcherForTest(t, wf)

	for _, width := range []int{0, -1, -42} {
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			_, err := disp.Dispatch(context.Background(), dispatchRequestFor(width, 1))
			if !errors.Is(err, orchestrator.ErrInvalidBuildRequest) {
				t.Fatalf("Dispatch: want ErrInvalidBuildRequest wrap, got %v", err)
			}
			if pool.Leased() != 0 {
				t.Fatalf("partial lease on validation failure: %d", pool.Leased())
			}
			if wf.spawnCalls.Load() != 0 {
				t.Fatalf("workforce spawned on validation failure: %d", wf.spawnCalls.Load())
			}
		})
	}
}

func TestDispatcherMixedResults(t *testing.T) {
	wf := &fakeWorkforce{
		results: []orchestrator.WorkerResult{
			{WorkerID: "w0", Status: "ok"},
			{WorkerID: "w1", Status: "ok"},
			{WorkerID: "w2", Status: "error", Err: errors.New("boom")},
			{WorkerID: "w3", Status: "aborted"},
		},
	}
	disp, memLog, pool := newDispatcherForTest(t, wf)

	res, err := disp.Dispatch(context.Background(), dispatchRequestFor(4, 2))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.WorkersSpawned != 4 {
		t.Fatalf("WorkersSpawned=%d, want 4", res.WorkersSpawned)
	}
	if res.Completed != 2 {
		t.Fatalf("Completed=%d, want 2", res.Completed)
	}
	if res.Errors != 1 {
		t.Fatalf("Errors=%d, want 1", res.Errors)
	}
	if res.Aborted != 1 {
		t.Fatalf("Aborted=%d, want 1", res.Aborted)
	}
	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked: %d", pool.Leased())
	}

	recs := queryRecords(t, memLog, testSessionID)
	if got := countByType(recs, eventlog.EvtWorkerDispatched); got != 4 {
		t.Fatalf("EvtWorkerDispatched count = %d, want 4", got)
	}
	if got := countByType(recs, eventlog.EvtWorkerCheckpoint); got != 4 {
		t.Fatalf("EvtWorkerCheckpoint count = %d, want 4", got)
	}
}

func TestDispatcherShutdownIdempotent(t *testing.T) {
	wf := &fakeWorkforce{}
	disp, _, _ := newDispatcherForTest(t, wf)

	if err := disp.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := disp.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if got := wf.abortCalls.Load(); got != 1 {
		t.Fatalf("AbortAll calls across two Shutdowns = %d, want exactly 1", got)
	}
}

func TestDispatcherNewDispatcherValidation(t *testing.T) {
	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	pool := &fakePool{}
	wf := &fakeWorkforce{}

	cases := map[string]orchestrator.DispatcherConfig{
		"nil clock": {
			EventLog: memLog, Pool: pool, Workforce: wf,
		},
		"nil eventlog": {
			Clock: clk, Pool: pool, Workforce: wf,
		},
		"nil pool": {
			Clock: clk, EventLog: memLog, Workforce: wf,
		},
		"nil workforce": {
			Clock: clk, EventLog: memLog, Pool: pool,
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := orchestrator.NewDispatcher(cfg)
			if !errors.Is(err, orchestrator.ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig wrap, got %v", err)
			}
			if got != nil {
				t.Fatalf("expected nil dispatcher on error, got %#v", got)
			}
		})
	}

	if _, err := orchestrator.NewDispatcher(orchestrator.DispatcherConfig{
		Clock: clk, EventLog: memLog, Pool: pool, Workforce: wf,
	}); err != nil {
		t.Fatalf("happy NewDispatcher: %v", err)
	}
}

func TestDispatcherShutdownReleasesLeasedFromInFlightDispatch(t *testing.T) {
	wf := &fakeWorkforce{blockResults: true}
	disp, _, pool := newDispatcherForTest(t, wf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dispatchStarted := make(chan struct{})
	dispatchReturned := make(chan error, 1)
	go func() {

		close(dispatchStarted)
		_, err := disp.Dispatch(ctx, dispatchRequestFor(2, 1))
		dispatchReturned <- err
	}()

	<-dispatchStarted

	deadline := time.Now().Add(2 * time.Second)
	for {
		if pool.Leased() == 2 && wf.spawnCalls.Load() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Dispatch did not reach drain loop: leased=%d spawnCalls=%d",
				pool.Leased(), wf.spawnCalls.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}

	if err := disp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if got := wf.abortCalls.Load(); got != 1 {
		t.Fatalf("AbortAll calls = %d, want 1", got)
	}

	if pool.Leased() != 0 {
		t.Fatalf("worktrees not released by Shutdown: %d still held", pool.Leased())
	}

	cancel()
	select {
	case err := <-dispatchReturned:

		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Dispatch returned unexpected err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Dispatch did not return after cancel + Shutdown")
	}
}

func TestDispatcherPreCancelledCtx(t *testing.T) {
	wf := &fakeWorkforce{}
	disp, _, pool := newDispatcherForTest(t, wf)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := disp.Dispatch(ctx, dispatchRequestFor(2, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch with pre-cancelled ctx: want context.Canceled, got %v", err)
	}
	if pool.Leased() != 0 {
		t.Fatalf("Lease must not run with cancelled ctx: leased=%d", pool.Leased())
	}
	if wf.spawnCalls.Load() != 0 {
		t.Fatalf("spawn must not run with cancelled ctx: %d", wf.spawnCalls.Load())
	}
}

func TestDispatcherShutdownReleasesNothingWhenNoLeases(t *testing.T) {
	wf := &fakeWorkforce{
		results: []orchestrator.WorkerResult{
			{WorkerID: "w0", Status: "ok"},
			{WorkerID: "w1", Status: "ok"},
		},
	}
	disp, _, pool := newDispatcherForTest(t, wf)

	if _, err := disp.Dispatch(context.Background(), dispatchRequestFor(2, 1)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if pool.Leased() != 0 {
		t.Fatalf("post-Dispatch leased=%d, want 0", pool.Leased())
	}

	if err := disp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := wf.abortCalls.Load(); got != 1 {
		t.Fatalf("AbortAll = %d, want 1", got)
	}
}

func TestDispatcherRemovePointerSetPartial(t *testing.T) {

	customWF := &routingWorkforce{
		firstCallTarget: &fakeWorkforce{blockResults: true},
		restCallsTarget: &fakeWorkforce{
			results: []orchestrator.WorkerResult{{WorkerID: "fast0", Status: "ok"}},
		},
	}
	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	pool2 := &fakePool{}
	disp2, err := orchestrator.NewDispatcher(orchestrator.DispatcherConfig{
		Clock: clk, EventLog: memLog, Pool: pool2, Workforce: customWF,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	slowCtx, cancelSlow := context.WithCancel(context.Background())
	defer cancelSlow()

	slowDone := make(chan error, 1)
	go func() {
		_, err := disp2.Dispatch(slowCtx, orchestrator.DispatchRequest{
			SessionID: testSessionID, ProjectID: testProjectID,
			Doctrine: "max-scope", Width: 1, Depth: 1, Spec: defaultSpec(),
		})
		slowDone <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if pool2.Leased() == 1 && customWF.firstCallTarget.spawnCalls.Load() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow Dispatch did not park: leased=%d slow.spawn=%d",
				pool2.Leased(), customWF.firstCallTarget.spawnCalls.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}

	res, err := disp2.Dispatch(context.Background(), orchestrator.DispatchRequest{
		SessionID: testSessionID, ProjectID: testProjectID,
		Doctrine: "max-scope", Width: 1, Depth: 1, Spec: defaultSpec(),
	})
	if err != nil {
		t.Fatalf("fast Dispatch: %v", err)
	}
	if res.Completed != 1 {
		t.Fatalf("fast Completed=%d, want 1", res.Completed)
	}

	if pool2.Leased() != 1 {
		t.Fatalf("post-fast leased=%d, want 1 (slow still parked)", pool2.Leased())
	}

	cancelSlow()
	select {
	case err := <-slowDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("slow Dispatch returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("slow Dispatch did not unwind on cancel")
	}
	if pool2.Leased() != 0 {
		t.Fatalf("post-cleanup leased=%d, want 0", pool2.Leased())
	}
}

type neverCloseWorkforce struct {
	abortCalls atomic.Int32
	done       chan struct{}
	stopOnce   sync.Once
}

func newNeverCloseWorkforce() *neverCloseWorkforce {
	return &neverCloseWorkforce{done: make(chan struct{})}
}

func (n *neverCloseWorkforce) Stop() {
	n.stopOnce.Do(func() { close(n.done) })
}

func (n *neverCloseWorkforce) SpawnWorkers(_ context.Context, _ orchestrator.SpawnRequest) (<-chan orchestrator.WorkerResult, error) {

	ch := make(chan orchestrator.WorkerResult)
	go func() {
		<-n.done

		close(ch)
	}()
	return ch, nil
}

func (n *neverCloseWorkforce) AbortAll(_ context.Context) error {
	n.abortCalls.Add(1)
	return nil
}

type postAbortWorkforce struct {
	results    []orchestrator.WorkerResult
	abortCalls atomic.Int32
}

func (p *postAbortWorkforce) SpawnWorkers(ctx context.Context, _ orchestrator.SpawnRequest) (<-chan orchestrator.WorkerResult, error) {
	ch := make(chan orchestrator.WorkerResult, len(p.results))
	go func() {
		defer close(ch)

		<-ctx.Done()
		for _, r := range p.results {
			ch <- r
		}
	}()
	return ch, nil
}

func (p *postAbortWorkforce) AbortAll(_ context.Context) error {
	p.abortCalls.Add(1)
	return nil
}

func TestDispatcherDrainDeadlineExceeded(t *testing.T) {
	nwf := newNeverCloseWorkforce()

	t.Cleanup(nwf.Stop)

	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	pool := &fakePool{}
	disp, err := orchestrator.NewDispatcher(orchestrator.DispatcherConfig{
		Clock:         clk,
		EventLog:      memLog,
		Pool:          pool,
		Workforce:     nwf,
		DrainDeadline: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, dispErr := disp.Dispatch(ctx, dispatchRequestFor(1, 1))
	if !errors.Is(dispErr, orchestrator.ErrDrainDeadlineExceeded) {
		t.Fatalf("Dispatch: want ErrDrainDeadlineExceeded in chain, got %v", dispErr)
	}
	if !errors.Is(dispErr, context.Canceled) {
		t.Fatalf("Dispatch: want context.Canceled in chain, got %v", dispErr)
	}
	if got := nwf.abortCalls.Load(); got != 1 {
		t.Fatalf("AbortAll calls = %d, want 1", got)
	}

	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked after drain-deadline bail: %d", pool.Leased())
	}
}

func TestDispatcherPostAbortCheckpointsEmitted(t *testing.T) {
	pawf := &postAbortWorkforce{
		results: []orchestrator.WorkerResult{
			{WorkerID: "w0", Status: "aborted"},
			{WorkerID: "w1", Status: "aborted"},
			{WorkerID: "w2", Status: "aborted"},
		},
	}

	clk := newTestClock()
	memLog := eventlog.NewMemory(clk)
	pool := &fakePool{}
	disp, err := orchestrator.NewDispatcher(orchestrator.DispatcherConfig{
		Clock:     clk,
		EventLog:  memLog,
		Pool:      pool,
		Workforce: pawf,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, dispErr := disp.Dispatch(ctx, dispatchRequestFor(3, 1))

	if !errors.Is(dispErr, context.Canceled) {
		t.Fatalf("Dispatch: want context.Canceled, got %v", dispErr)
	}

	recs, qErr := memLog.Query(context.Background(), testSessionID, 0)
	if qErr != nil {
		t.Fatalf("eventlog.Query: %v", qErr)
	}
	if got := countByType(recs, eventlog.EvtWorkerCheckpoint); got != 3 {
		t.Fatalf("EvtWorkerCheckpoint count = %d, want 3 (post-cancel audit trail must survive)", got)
	}

	if pool.Leased() != 0 {
		t.Fatalf("worktrees leaked: %d", pool.Leased())
	}
}

type routingWorkforce struct {
	firstCallTarget *fakeWorkforce
	restCallsTarget *fakeWorkforce
	calls           atomic.Int32
}

func (r *routingWorkforce) SpawnWorkers(ctx context.Context, req orchestrator.SpawnRequest) (<-chan orchestrator.WorkerResult, error) {
	n := r.calls.Add(1)
	if n == 1 {
		return r.firstCallTarget.SpawnWorkers(ctx, req)
	}
	return r.restCallsTarget.SpawnWorkers(ctx, req)
}

func (r *routingWorkforce) AbortAll(ctx context.Context) error {
	_ = r.firstCallTarget.AbortAll(ctx)
	return r.restCallsTarget.AbortAll(ctx)
}
