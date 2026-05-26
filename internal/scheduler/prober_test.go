package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func okQueueDepth(total int, by map[string]int) QueueDepthFn {
	return func(ctx context.Context, now time.Time) (int, map[string]int, error) {
		return total, by, nil
	}
}

func errQueueDepth(err error) QueueDepthFn {
	return func(ctx context.Context, now time.Time) (int, map[string]int, error) {
		return 0, nil, err
	}
}

func okMissedFires(total int, by map[string]int) MissedFiresFn {
	return func(ctx context.Context, since time.Time) (int, map[string]int, error) {
		return total, by, nil
	}
}

func okWfqStatus(maxPct int, alias string) WfqStatusFn {
	return func(ctx context.Context) (int, string, error) { return maxPct, alias, nil }
}

func okDispatcherPing(err error) DispatcherPingFn {
	return func(ctx context.Context) error { return err }
}

func TestProberQueueDepthOK(t *testing.T) {
	p := NewProber(
		okQueueDepth(5, map[string]int{"internal-platform-x": 3, "zen-swarm": 2}),
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
	total, by, err := p.QueueDepth(context.Background())
	if err != nil {
		t.Fatalf("QueueDepth: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if by["internal-platform-x"] != 3 {
		t.Errorf("internal-platform-x = %d, want 3", by["internal-platform-x"])
	}
}

func TestProberQueueDepthError(t *testing.T) {
	p := NewProber(
		errQueueDepth(errors.New("db locked")),
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
	_, _, err := p.QueueDepth(context.Background())
	if err == nil {
		t.Error("expected error")
	}
}

func TestProberQueueDepthPassesNow(t *testing.T) {

	var passed time.Time
	p := NewProber(
		func(ctx context.Context, now time.Time) (int, map[string]int, error) {
			passed = now
			return 0, nil, nil
		},
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
	_, _, _ = p.QueueDepth(context.Background())
	if passed.IsZero() {
		t.Error("queueDepth received zero time")
	}
	if time.Since(passed) > 5*time.Second {
		t.Errorf("queueDepth time stale: %v ago", time.Since(passed))
	}
}

func TestProberMissedFires24hWindow(t *testing.T) {

	var passed time.Time
	p := NewProber(
		okQueueDepth(0, nil),
		func(ctx context.Context, since time.Time) (int, map[string]int, error) {
			passed = since
			return 3, map[string]int{"internal-platform-x": 3}, nil
		},
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
	total, by, err := p.MissedFires24h(context.Background())
	if err != nil {
		t.Fatalf("MissedFires24h: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if by["internal-platform-x"] != 3 {
		t.Errorf("internal-platform-x = %d, want 3", by["internal-platform-x"])
	}
	expected := time.Now().Add(-24 * time.Hour)
	delta := passed.Sub(expected)
	if delta < -time.Second || delta > time.Second {
		t.Errorf("since=%v expected≈%v (delta=%v)", passed, expected, delta)
	}
}

func TestProberWfqSaturation(t *testing.T) {
	p := NewProber(
		okQueueDepth(0, nil),
		okMissedFires(0, nil),
		okWfqStatus(90, "internal-platform-x"),
		okDispatcherPing(nil),
	)
	pct, alias, err := p.WfqSaturation(context.Background())
	if err != nil {
		t.Fatalf("WfqSaturation: %v", err)
	}
	if pct != 90 || alias != "internal-platform-x" {
		t.Errorf("got pct=%d alias=%q, want 90/internal-platform-x", pct, alias)
	}
}

func TestProberDispatcherBoundOK(t *testing.T) {
	p := NewProber(
		okQueueDepth(0, nil),
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
	if err := p.DispatcherBound(context.Background()); err != nil {
		t.Errorf("DispatcherBound: %v", err)
	}
}

func TestProberDispatcherBoundFail(t *testing.T) {
	innerErr := errors.New("dispatcher down")
	p := NewProber(
		okQueueDepth(0, nil),
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(innerErr),
	)
	err := p.DispatcherBound(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, innerErr) {
		t.Errorf("errors.Is(err, innerErr) = false; want true")
	}
}

func TestProberNewPanicsOnNilQueueDepth(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(nil, ...) should panic")
		}
	}()
	_ = NewProber(nil,
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
}

func TestProberNewPanicsOnNilMissedFires(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, nil, ...) should panic")
		}
	}()
	_ = NewProber(
		okQueueDepth(0, nil), nil,
		okWfqStatus(0, ""),
		okDispatcherPing(nil),
	)
}

func TestProberNewPanicsOnNilWfqStatus(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, *, nil, *) should panic")
		}
	}()
	_ = NewProber(
		okQueueDepth(0, nil),
		okMissedFires(0, nil),
		nil,
		okDispatcherPing(nil),
	)
}

func TestProberNewPanicsOnNilDispatcherPing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, *, *, nil) should panic")
		}
	}()
	_ = NewProber(
		okQueueDepth(0, nil),
		okMissedFires(0, nil),
		okWfqStatus(0, ""),
		nil,
	)
}
