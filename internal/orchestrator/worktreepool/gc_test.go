package worktreepool_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func gcPruneCallCount(exec *recordingExec) int {
	n := 0
	for _, c := range exec.callsSnapshot() {
		if strings.Contains(strings.Join(c, " "), "worktree prune") {
			n++
		}
	}
	return n
}

// TestGC_PrunesOnCadence verifies the gcLoop ticks at cfg.GCCadence and
// invokes `git worktree prune` (the B-2 wrapper) on every tick. We drive
// the cadence deterministically via *clock.Fake.Advance() so the test
// neither racey nor wall-clock-bound.
//
// Clock seam (Q14 C, IMP-2): every orchestrator-tier component
// that consumes time MUST take a Clock seam. B-7's gcLoop reads the
// ticker via p.clk.NewTicker so this test injects clock.Fake to drive
// exactly N fires without sleeping.
func TestGC_PrunesOnCadence(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
		Clock:       fake,
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fake.Advance(cfg.GCCadence)

		if fake.BlockUntilCondition(func() bool {
			return gcPruneCallCount(exec) >= 3
		}, 50*time.Millisecond) {
			break
		}
	}
	if got := gcPruneCallCount(exec); got < 3 {
		t.Fatalf("expected >=3 `git worktree prune` calls after poll-Advance; got %d (calls=%v)",
			got, exec.callsSnapshot())
	}
}

// TestGC_PruneFailureEmitsDegraded verifies a failing `git worktree prune`
// (e.g., ENOSPC writing the.git/worktrees ref-update lock) drives a
// WorktreePoolDegraded event emission so HRA Q8 cost-pressure
// downgrade can react. The event MUST carry doctrine + pool_id + reason
// for downstream filtering (mirrors leaseSlowPath + prewarm precedent).
func TestGC_PruneFailureEmitsDegraded(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
		Clock:       fake,
	}
	exec := newRecordingExec()
	exec.setScenario("worktree prune",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	saw := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !saw {
		fake.Advance(cfg.GCCadence)
		saw = fake.BlockUntilCondition(func() bool {
			for _, e := range emitter.eventTypes() {
				if e == eventlog.EvtWorktreePoolDegraded {
					return true
				}
			}
			return false
		}, 50*time.Millisecond)
	}
	if !saw {
		t.Fatalf("EvtWorktreePoolDegraded not emitted on prune failure; types=%v", emitter.eventTypes())
	}

	for _, evt := range emitter.eventsSnapshot() {
		if evt.Type != eventlog.EvtWorktreePoolDegraded {
			continue
		}
		payload := evt.Payload
		if payload == nil {
			t.Fatalf("EvtWorktreePoolDegraded payload nil: %#v", evt)
		}
		if payload["reason"] != "gc-prune-failed" {
			continue
		}
		if payload["doctrine"] != "default" {
			t.Errorf("doctrine=%v, want default", payload["doctrine"])
		}
		if payload["pool_id"] != "p" {
			t.Errorf("pool_id=%v, want p", payload["pool_id"])
		}
		if payload["error"] == nil || payload["error"] == "" {
			t.Errorf("error field empty: %#v", payload)
		}
		return
	}
	t.Errorf("no Degraded event with reason=gc-prune-failed; events=%+v", emitter.eventsSnapshot())
}

func TestGC_HonorsCtxCancel(t *testing.T) {
	fake := clock.NewFake(time.Unix(0, 0))
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
		Clock:       fake,
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, newRecordingExec())
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Close did not return — gcLoop did not honor ctx.Done")
	}
}

func TestPruneOrphans_PublicAPI_RunsBothLayers(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	sawPrune := false
	sawList := false
	for _, c := range exec.callsSnapshot() {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "worktree prune") {
			sawPrune = true
		}
		if strings.Contains(joined, "worktree list") {
			sawList = true
		}
	}
	if !sawPrune {
		t.Errorf("PruneOrphans did not invoke `git worktree prune`")
	}
	if !sawList {
		t.Errorf("PruneOrphans did not invoke `git worktree list --porcelain` (Layer B membership)")
	}
	if report.Duration <= 0 {
		t.Errorf("Duration not set: %v", report.Duration)
	}
}

func TestPruneOrphans_ClosedPool(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	report, err := p.PruneOrphans(context.Background())
	if !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("PruneOrphans on closed pool: err=%v, want ErrPoolClosed", err)
	}

	if report.GitPruned != 0 || report.FilesystemSwept != 0 || len(report.Errors) != 0 || report.Duration != 0 {
		t.Errorf("closed-pool PruneOrphans returned non-zero report: %+v", report)
	}
}

func TestPruneOrphans_LayerBSweepsLeakedDirs(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()

	exec.setScenario("worktree list", []byte(""), nil)

	leaked := filepath.Join(worktreeDir, "p-leaked-99")
	if err := os.MkdirAll(leaked, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Pre-create a foreign dir (different prefix) — Layer B MUST NOT touch.
	foreign := filepath.Join(worktreeDir, "other-pool-77")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 1 {
		t.Errorf("FilesystemSwept=%d, want 1; report=%+v", report.FilesystemSwept, report)
	}
	if _, err := os.Stat(leaked); !os.IsNotExist(err) {
		t.Errorf("leaked dir not removed: stat err=%v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("foreign dir got removed (Layer B must skip non-pool prefix): err=%v", err)
	}
}

func TestPruneOrphans_LayerBSkipsLeasedAndWarm(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  3,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()

	exec.setScenario("worktree list", []byte(""), nil)

	exec.setScenario("worktree add", nil, errors.New("exit status 1"))
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	if err := worktreepool.SeedWarmForTest(p, 1); err != nil {
		t.Fatalf("SeedWarmForTest: %v", err)
	}
	warmCount := worktreepool.WarmCountForTest(p)
	if warmCount != 1 {
		t.Fatalf("warm count=%d, want 1", warmCount)
	}
	warmPaths := worktreepool.WarmPathsForTest(p)
	if len(warmPaths) != 1 {
		t.Fatalf("WarmPathsForTest len=%d, want 1", len(warmPaths))
	}

	warmDir := warmPaths[0]
	if err := os.MkdirAll(warmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 0 {
		t.Errorf("FilesystemSwept=%d on a warm-only pool — Layer B must skip warm; report=%+v",
			report.FilesystemSwept, report)
	}
	if _, err := os.Stat(warmDir); err != nil {
		t.Errorf("warm dir got removed: stat err=%v", err)
	}
}

func TestPruneOrphans_LayerAErrorEmitsDegraded(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree prune",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans returned err — partial-success contract breached: %v", err)
	}
	if len(report.Errors) == 0 {
		t.Fatalf("report.Errors empty on Layer A failure: %+v", report)
	}
	sawPartial := false
	for _, evt := range emitter.eventsSnapshot() {
		if evt.Type != eventlog.EvtWorktreePoolDegraded {
			continue
		}
		if evt.Payload != nil && evt.Payload["reason"] == "prune-orphans-partial" {
			sawPartial = true
			break
		}
	}
	if !sawPartial {
		t.Errorf("WorktreePoolDegraded reason=prune-orphans-partial not emitted; events=%+v",
			emitter.eventsSnapshot())
	}
}

func TestPruneOrphans_LayerBListErrorAccumulates(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list",
		[]byte("fatal: not a git repository\n"),
		errors.New("exit status 128"))
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans should swallow Layer B errors: %v", err)
	}
	sawListErr := false
	for _, e := range report.Errors {
		if strings.HasPrefix(e, "layerB-list:") {
			sawListErr = true
		}
	}
	if !sawListErr {
		t.Errorf("Errors missing layerB-list prefix entry: %+v", report.Errors)
	}
}

func TestPruneOrphans_LayerBReadDirError(t *testing.T) {

	missingDir := filepath.Join(t.TempDir(), "absent-subdir")
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: missingDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list", []byte(""), nil)
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans should swallow Layer B errors: %v", err)
	}
	sawReadDir := false
	for _, e := range report.Errors {
		if strings.HasPrefix(e, "layerB-readdir:") {
			sawReadDir = true
		}
	}
	if !sawReadDir {
		t.Errorf("Errors missing layerB-readdir prefix entry: %+v", report.Errors)
	}
}

func TestPruneOrphans_LayerBSkipsFiles(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list", []byte(""), nil)

	stray := filepath.Join(worktreeDir, "p-stray-file")
	if err := os.WriteFile(stray, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 0 {
		t.Errorf("FilesystemSwept=%d on file-only candidate; want 0; report=%+v",
			report.FilesystemSwept, report)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray file got removed: stat err=%v", err)
	}
}

// TestPruneOrphans_LayerBHonorsZenPoolPrefix verifies the alternate
// `zen-pool-{poolID}-` prefix path (matches the branch naming Q1 contract:
// `zen-pool-{PoolID}-{leaseID}` is also a valid dir prefix in some
// orchestrator paths). Both prefixes MUST be respected.
func TestPruneOrphans_LayerBHonorsZenPoolPrefix(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list", []byte(""), nil)

	leaked := filepath.Join(worktreeDir, "zen-pool-p-77")
	if err := os.MkdirAll(leaked, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 1 {
		t.Errorf("FilesystemSwept=%d, want 1 (zen-pool-{poolID}- prefix); report=%+v",
			report.FilesystemSwept, report)
	}
}

func TestPruneOrphans_LayerBKnownEntrySkipped(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	known := filepath.Join(worktreeDir, "p-known-1")
	if err := os.MkdirAll(known, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	exec.setScenario("worktree list",
		[]byte("worktree "+known+"\nHEAD abc123\nbranch refs/heads/main\n\n"),
		nil)
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 0 {
		t.Errorf("FilesystemSwept=%d, want 0 (known dir); report=%+v",
			report.FilesystemSwept, report)
	}
	if _, err := os.Stat(known); err != nil {
		t.Errorf("known dir removed: %v", err)
	}
}

// TestPruneOrphans_LayerARemovesAdminEntries — exercises the GitPruned
// counter path. Pre-state has more entries than post-state by mocking
// two consecutive `worktree list` invocations: the pre-prune list returns
// 3 entries; the post-prune list returns 1. GitPruned MUST be 2.
func TestPruneOrphans_LayerARemovesAdminEntries(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}

	inner := newRecordingExec()
	var listCount atomic.Int32
	exec := &countingListExec{
		inner: inner,
		listResponses: [][]byte{
			[]byte("worktree /a\n\nworktree /b\n\nworktree /c\n\n"),
			[]byte("worktree /a\n\n"),
		},
		listCalls: &listCount,
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()
	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.GitPruned != 2 {
		t.Errorf("GitPruned=%d, want 2 (3 pre - 1 post); report=%+v", report.GitPruned, report)
	}
}

func TestPruneOrphans_AdminOnlyCleared_TracksGitPruneRemovals(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}

	inner := newRecordingExec()
	var listCount atomic.Int32
	exec := &countingListExec{
		inner: inner,
		listResponses: [][]byte{
			[]byte("worktree /a\n\nworktree /b\n\nworktree /c\n\n"),
			[]byte("worktree /a\n\n"),
		},
		listCalls: &listCount,
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()
	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.AdminOnlyCleared != 2 {
		t.Errorf("AdminOnlyCleared=%d, want 2; report=%+v", report.AdminOnlyCleared, report)
	}
	if report.AdminOnlyCleared != report.GitPruned {
		t.Errorf("AdminOnlyCleared=%d, want == GitPruned=%d (Layer A semantics: prune only clears entries with no dir)",
			report.AdminOnlyCleared, report.GitPruned)
	}
}

func TestPruneOrphans_AdminOnlyCleared_ZeroOnLayerAFailure(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree prune",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()
	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.AdminOnlyCleared != 0 {
		t.Errorf("AdminOnlyCleared=%d on Layer A failure; want 0 (no successful diff observed); report=%+v",
			report.AdminOnlyCleared, report)
	}
	if report.GitPruned != 0 {
		t.Errorf("GitPruned=%d on Layer A failure; want 0; report=%+v", report.GitPruned, report)
	}
}

func TestPruneOrphans_LayerBSkipsLeased(t *testing.T) {
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  3,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list", []byte(""), nil)
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()
	if err := worktreepool.SeedWarmForTest(p, 1); err != nil {
		t.Fatalf("SeedWarmForTest: %v", err)
	}

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if w == nil {
		t.Fatalf("Lease returned nil worktree")
	}
	leasedDir := w.Path()
	if err := os.MkdirAll(leasedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.FilesystemSwept != 0 {
		t.Errorf("FilesystemSwept=%d, want 0 (leased dir must be skipped); report=%+v",
			report.FilesystemSwept, report)
	}
	if _, statErr := os.Stat(leasedDir); statErr != nil {
		t.Errorf("leased dir got removed: stat err=%v", statErr)
	}
}

func TestPruneOrphans_LayerBRemoveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — chmod-based RemoveAll failure cannot be induced")
	}
	worktreeDir := t.TempDir()
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: worktreeDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree list", []byte(""), nil)

	leaked := filepath.Join(worktreeDir, "p-leaked-1")
	innerSub := filepath.Join(leaked, "inner")
	if err := os.MkdirAll(innerSub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	innerFile := filepath.Join(innerSub, "f")
	if err := os.WriteFile(innerFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := os.Chmod(leaked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {

		_ = os.Chmod(leaked, 0o755)
	})

	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Close(ctx)
	}()

	report, err := p.PruneOrphans(context.Background())
	if err != nil {
		t.Fatalf("PruneOrphans should swallow removal errors: %v", err)
	}
	if report.FilesystemSwept != 0 {
		t.Errorf("FilesystemSwept=%d on chmod-blocked dir; want 0; report=%+v",
			report.FilesystemSwept, report)
	}
	sawRemove := false
	for _, e := range report.Errors {
		if strings.HasPrefix(e, "layerB-remove ") {
			sawRemove = true
		}
	}
	if !sawRemove {
		t.Errorf("Errors missing layerB-remove prefix entry: %+v", report.Errors)
	}
}

type countingListExec struct {
	inner         *recordingExec
	listResponses [][]byte
	listCalls     *atomic.Int32
}

func (c *countingListExec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	rec := append([]string{name}, args...)
	if strings.Contains(strings.Join(rec, " "), "worktree list") {
		i := int(c.listCalls.Add(1)) - 1
		if i >= len(c.listResponses) {
			i = len(c.listResponses) - 1
		}

		_, _ = c.inner.Run(ctx, name, args...)
		return c.listResponses[i], nil
	}
	return c.inner.Run(ctx, name, args...)
}
