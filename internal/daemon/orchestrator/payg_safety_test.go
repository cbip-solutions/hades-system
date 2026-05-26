package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCounters struct {
	mu            sync.Mutex
	sessionTotals map[string]float64

	projectTotals map[string]float64
}

func newFakeCounters() *fakeCounters {
	return &fakeCounters{
		sessionTotals: map[string]float64{},
		projectTotals: map[string]float64{},
	}
}

func (f *fakeCounters) SessionTotal(sessionID string) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionTotals[sessionID]
}

func (f *fakeCounters) ProjectProfileTierTotal(project, profile, tier string, window time.Duration) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var winName string
	switch window {
	case 24 * time.Hour:
		winName = "24h"
	case 30 * 24 * time.Hour:
		winName = "30d"
	default:
		return 0
	}
	return f.projectTotals[project+"|"+profile+"|"+tier+"|"+winName]
}

func (f *fakeCounters) setSession(sessionID string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionTotals[sessionID] = v
}

func (f *fakeCounters) setProject(project, profile, tier, winName string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projectTotals[project+"|"+profile+"|"+tier+"|"+winName] = v
}

type fakeNotifier struct {
	mu       sync.Mutex
	info     []string
	warn     []string
	critical []string
}

func (n *fakeNotifier) NotifyINFO(title, body, source string) {
	n.mu.Lock()
	n.info = append(n.info, title+"|"+body+"|"+source)
	n.mu.Unlock()
}

func (n *fakeNotifier) NotifyWARN(title, body, source string) {
	n.mu.Lock()
	n.warn = append(n.warn, title+"|"+body+"|"+source)
	n.mu.Unlock()
}

func (n *fakeNotifier) NotifyCRITICAL(title, body, source string) {
	n.mu.Lock()
	n.critical = append(n.critical, title+"|"+body+"|"+source)
	n.mu.Unlock()
}

func (n *fakeNotifier) infoCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.info)
}

func (n *fakeNotifier) warnCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.warn)
}

func (n *fakeNotifier) criticalCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.critical)
}

func newSafety(t *testing.T) (*PaygSafety, *fakeCounters, *fakeNotifier) {
	t.Helper()
	c := newFakeCounters()
	n := &fakeNotifier{}
	return NewPaygSafety(PaygSafetyOptions{Counters: c, Notifier: n}), c, n
}

var defaultEffective = ProfileEffective{
	PerSessionUSD: 0,
	PerDayUSD:     0,
	PerMonthUSD:   100.0,
	OnCapReached:  ModePauseDescriptive,
}

func TestCheckCapUnderCapNoError(t *testing.T) {
	s, c, _ := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 10.0)
	if err := s.CheckCap("p", "pr", "t", "sess1", 5.0, defaultEffective); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestCheckCapExceedsReturnsSentinel(t *testing.T) {
	s, c, _ := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 99.0)
	err := s.CheckCap("p", "pr", "t", "sess1", 5.0, defaultEffective)
	if !errors.Is(err, ErrCapWillExceed) {
		t.Errorf("err=%v, want ErrCapWillExceed", err)
	}
}

func TestCheckCapZeroCapMeansUnenforced(t *testing.T) {
	s, c, _ := newSafety(t)
	eff := ProfileEffective{
		PerSessionUSD: 0,
		PerDayUSD:     0,
		PerMonthUSD:   0,
		OnCapReached:  ModePauseDescriptive,
	}
	c.setProject("p", "pr", "t", "30d", 9_999.0)
	if err := s.CheckCap("p", "pr", "t", "sess1", 9_999.0, eff); err != nil {
		t.Errorf("unconfigured caps must not block, got %v", err)
	}
}

func TestCheckCapFiresThresholdAt50(t *testing.T) {
	s, c, n := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 30.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	if got := n.infoCount(); got != 1 {
		t.Fatalf("info notifications=%d, want 1", got)
	}
	if !strings.Contains(n.info[0], "50%") {
		t.Errorf("expected 50%% in INFO body, got %q", n.info[0])
	}
}

func TestCheckCapFiresThresholdAt80(t *testing.T) {
	s, c, n := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 70.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 15.0, defaultEffective)
	if got := n.warnCount(); got != 1 {
		t.Fatalf("warn notifications=%d, want 1", got)
	}
}

func TestCheckCapFiresThresholdAt100(t *testing.T) {
	s, c, n := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 95.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 10.0, defaultEffective)
	if got := n.criticalCount(); got < 1 {
		t.Fatalf("critical notifications=%d, want >=1", got)
	}
}

func TestCheckCapThresholdsAreIdempotent(t *testing.T) {
	s, c, n := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 30.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	if got := n.infoCount(); got != 1 {
		t.Errorf("info notifications=%d, want 1 (idempotent)", got)
	}
}

func TestCheckCapMultipleWindows(t *testing.T) {
	s, c, n := newSafety(t)
	eff := ProfileEffective{
		PerSessionUSD: 5,
		PerDayUSD:     50,
		PerMonthUSD:   500,
		OnCapReached:  ModePauseDescriptive,
	}
	c.setSession("sess1", 0)
	c.setProject("p", "pr", "t", "24h", 0)
	c.setProject("p", "pr", "t", "30d", 0)

	err := s.CheckCap("p", "pr", "t", "sess1", 6.0, eff)
	if !errors.Is(err, ErrCapWillExceed) {
		t.Errorf("err=%v, want ErrCapWillExceed", err)
	}
	if got := n.criticalCount(); got < 1 {
		t.Errorf("expected at least one CRITICAL (session 100%%); got %d", got)
	}
}

func TestHandleCapReachedPauseHard(t *testing.T) {
	s, _, n := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", ModePause)
	if !errors.Is(err, ErrTierPausedHard) {
		t.Errorf("err=%v, want ErrTierPausedHard", err)
	}
	if got := n.criticalCount(); got != 1 {
		t.Errorf("critical=%d, want 1", got)
	}
}

func TestHandleCapReachedPauseDescriptive(t *testing.T) {
	s, _, n := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", ModePauseDescriptive)
	if !errors.Is(err, ErrTierPausedDescriptive) {
		t.Errorf("err=%v, want ErrTierPausedDescriptive", err)
	}
	if got := n.criticalCount(); got != 1 {
		t.Fatalf("critical=%d, want 1", got)
	}
	if !strings.Contains(n.critical[0], "zen budget") {
		t.Errorf("descriptive body should reference operator commands; got %q", n.critical[0])
	}
}

func TestHandleCapReachedCascadeDown(t *testing.T) {
	s, _, n := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", ModeCascadeDown)
	if !errors.Is(err, ErrCascadeDown) {
		t.Errorf("err=%v, want ErrCascadeDown", err)
	}
	if got := n.warnCount(); got != 1 {
		t.Errorf("warn=%d, want 1", got)
	}
	if got := n.criticalCount(); got != 0 {
		t.Errorf("critical=%d, want 0 for cascade", got)
	}
}

func TestHandleCapReachedNotifyOnly(t *testing.T) {
	s, _, n := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", ModeNotifyOnly)
	if err != nil {
		t.Errorf("err=%v, want nil for notify_only", err)
	}
	if got := n.criticalCount(); got != 1 {
		t.Errorf("critical=%d, want 1", got)
	}
}

func TestHandleCapReachedEmptyDefaultsToDescriptive(t *testing.T) {
	s, _, _ := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", "")
	if !errors.Is(err, ErrTierPausedDescriptive) {
		t.Errorf("err=%v, want ErrTierPausedDescriptive (default)", err)
	}
}

func TestHandleCapReachedUnknownFallsBackToDescriptive(t *testing.T) {
	s, _, _ := newSafety(t)
	err := s.HandleCapReached("p", "pr", "t", "bogus_mode")
	if !errors.Is(err, ErrTierPausedDescriptive) {
		t.Errorf("err=%v, want ErrTierPausedDescriptive (fallback)", err)
	}
}

func TestNewPaygSafetyPanicsOnNilCounters(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil Counters")
		}
	}()
	_ = NewPaygSafety(PaygSafetyOptions{Counters: nil})
}

func TestPaygSafetyNilNotifierSilent(t *testing.T) {
	c := newFakeCounters()
	c.setProject("p", "pr", "t", "30d", 95.0)
	s := NewPaygSafety(PaygSafetyOptions{Counters: c, Notifier: nil})

	_ = s.CheckCap("p", "pr", "t", "sess1", 10.0, defaultEffective)
	_ = s.HandleCapReached("p", "pr", "t", ModePauseDescriptive)
}

func TestWindowResetSchedulerExitsOnCancel(t *testing.T) {
	s, _, _ := newSafety(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := s.WindowResetScheduler(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("WindowResetScheduler did not exit on cancel")
	}
}

func TestCapOverridesPinSentinel(t *testing.T) {
	if !capOverridesPin() {
		t.Fatal("capOverridesPin must return true (inv-zen-063 anchor)")
	}
}

func TestCheckCapNotifierNilDoesNotPanic(t *testing.T) {
	c := newFakeCounters()
	c.setProject("p", "pr", "t", "30d", 30.0)
	s := NewPaygSafety(PaygSafetyOptions{Counters: c, Notifier: nil})
	if err := s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective); err != nil {
		t.Errorf("err=%v, want nil with nil notifier", err)
	}
}

func TestCheckCapNilReceiverReturnsError(t *testing.T) {
	var p *PaygSafety
	err := p.CheckCap("p", "pr", "t", "sess1", 1.0, defaultEffective)
	if err == nil {
		t.Fatal("expected error from nil receiver, got nil")
	}
}

func TestHandleCapReachedNilReceiverReturnsError(t *testing.T) {
	var p *PaygSafety
	err := p.HandleCapReached("p", "pr", "t", ModePause)
	if err == nil {
		t.Fatal("expected error from nil receiver, got nil")
	}
}

func TestCheckCapNotifyAtPercentsCustom(t *testing.T) {
	s, c, n := newSafety(t)
	eff := ProfileEffective{
		PerMonthUSD:      100.0,
		NotifyAtPercents: []int{25, 75},
		OnCapReached:     ModePauseDescriptive,
	}
	c.setProject("p", "pr", "t", "30d", 10.0)

	_ = s.CheckCap("p", "pr", "t", "sess1", 20.0, eff)
	if got := n.infoCount(); got != 1 {
		t.Errorf("info=%d, want 1 (25%% crossing)", got)
	}

	_ = s.CheckCap("p", "pr", "t", "sess1", 30.0, eff)
	if got := n.infoCount(); got != 1 {
		t.Errorf("info=%d, want 1 (50%% NOT in custom thresholds)", got)
	}

	c.setProject("p", "pr", "t", "30d", 60.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 20.0, eff)
	if got := n.infoCount(); got != 2 {
		t.Errorf("info=%d, want 2 (75%% crossing)", got)
	}
}

func TestCheckCapAtExactBoundaryStrict(t *testing.T) {
	s, c, _ := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 99.0)
	err := s.CheckCap("p", "pr", "t", "sess1", 1.0, defaultEffective)
	if err != nil {
		t.Errorf("at-cap (projected=cap) must not exceed (strict >); got %v", err)
	}

	err = s.CheckCap("p", "pr", "t", "sess1", 1.01, defaultEffective)
	if !errors.Is(err, ErrCapWillExceed) {
		t.Errorf("over-cap by 0.01 must exceed; got %v", err)
	}
}

func TestCheckCapNegativeProjectedAdd(t *testing.T) {
	s, c, _ := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 50.0)
	err := s.CheckCap("p", "pr", "t", "sess1", -10.0, defaultEffective)
	if err != nil {
		t.Errorf("negative projectedAdd (refund) must not exceed; got %v", err)
	}
}

func TestCapOverridesPinAnchorPresent(t *testing.T) {

	if !capOverridesPin() {
		t.Fatal("capOverridesPin must return true")
	}
}

func TestCheckCapConcurrentSafety(t *testing.T) {
	s, c, _ := newSafety(t)
	c.setProject("p", "pr", "t", "30d", 30.0)
	var wg sync.WaitGroup
	var calls atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.CheckCap("p", "pr", "t", "sess1", 5.0, defaultEffective)
				calls.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 800 {
		t.Fatalf("calls=%d, want 800", calls.Load())
	}
}

func TestHandleCapReachedNilNotifierCascade(t *testing.T) {
	c := newFakeCounters()
	s := NewPaygSafety(PaygSafetyOptions{Counters: c, Notifier: nil})
	if err := s.HandleCapReached("p", "pr", "t", ModeCascadeDown); !errors.Is(err, ErrCascadeDown) {
		t.Errorf("err=%v, want ErrCascadeDown", err)
	}
}

func TestPercentageHelperClamps(t *testing.T) {
	if got := percentage(50, 0); got != 0 {
		t.Errorf("capValue=0: got %d, want 0", got)
	}
	if got := percentage(-10, 100); got != 0 {
		t.Errorf("negative value: got %d, want 0 (clamped)", got)
	}
	if got := percentage(1_000_000, 100); got != 999 {
		t.Errorf("huge value: got %d, want 999 (clamped)", got)
	}
	if got := percentage(50, 100); got != 50 {
		t.Errorf("normal: got %d, want 50", got)
	}
}

var _ CapCounters = (*CostCounters)(nil)

func TestWindowResetSchedulerClearsThresholds(t *testing.T) {
	c := newFakeCounters()
	n := &fakeNotifier{}
	s := NewPaygSafety(PaygSafetyOptions{Counters: c, Notifier: n})
	s.tickInterval = 1 * time.Millisecond

	c.setProject("p", "pr", "t", "30d", 30.0)
	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	if got := n.infoCount(); got != 1 {
		t.Fatalf("pre-reset info=%d, want 1", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := s.WindowResetScheduler(ctx)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		empty := len(s.thresholdsSent) == 0
		s.mu.Unlock()
		if empty {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	_ = s.CheckCap("p", "pr", "t", "sess1", 25.0, defaultEffective)
	cancel()
	<-done

	if got := n.infoCount(); got < 2 {
		t.Errorf("post-reset info=%d, want >=2 (threshold re-fired)", got)
	}
}
