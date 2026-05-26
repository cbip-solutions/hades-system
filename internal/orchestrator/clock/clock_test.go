package clock_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

func TestRealNowIsMonotonic(t *testing.T) {
	c := clock.Real{}
	a := c.Now()
	time.Sleep(2 * time.Millisecond)
	b := c.Now()
	if !b.After(a) {
		t.Fatalf("Real.Now did not advance: a=%v b=%v", a, b)
	}
}

func TestRealSinceMatchesNow(t *testing.T) {
	c := clock.Real{}
	start := c.Now()
	time.Sleep(2 * time.Millisecond)
	d := c.Since(start)
	if d <= 0 {
		t.Fatalf("Real.Since returned non-positive: %v", d)
	}
}

func TestFakeAdvance(t *testing.T) {
	base := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(base)
	if !f.Now().Equal(base) {
		t.Fatalf("Fake.Now != base: got %v", f.Now())
	}
	f.Advance(5 * time.Second)
	want := base.Add(5 * time.Second)
	if !f.Now().Equal(want) {
		t.Fatalf("after Advance: got %v want %v", f.Now(), want)
	}
}

func TestFakeNewTimerFiresOnAdvance(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tm := f.NewTimer(100 * time.Millisecond)
	select {
	case <-tm.C():
		t.Fatalf("timer fired before Advance")
	default:
	}
	f.Advance(99 * time.Millisecond)
	select {
	case <-tm.C():
		t.Fatalf("timer fired below threshold")
	default:
	}
	f.Advance(2 * time.Millisecond)
	select {
	case <-tm.C():

	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timer did not fire after Advance past deadline")
	}
}

func TestFakeNewTickerRepeats(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tk := f.NewTicker(50 * time.Millisecond)
	defer tk.Stop()
	var fired atomic.Int32
	go func() {
		for range tk.C() {
			fired.Add(1)
			if fired.Load() >= 3 {
				return
			}
		}
	}()

	f.Advance(150 * time.Millisecond)
	ok := f.BlockUntilCondition(func() bool { return fired.Load() >= 3 }, time.Second)
	if !ok {
		t.Fatalf("ticker did not fire 3 times within 1s (fired=%d)", fired.Load())
	}
}

func TestFakeTimerStop(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tm := f.NewTimer(time.Second)
	if !tm.Stop() {
		t.Fatalf("Stop returned false on unexpired timer")
	}
	f.Advance(2 * time.Second)
	select {
	case <-tm.C():
		t.Fatalf("stopped timer fired")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRealNewTimerFires(t *testing.T) {
	c := clock.Real{}
	tm := c.NewTimer(5 * time.Millisecond)
	defer tm.Stop()
	select {
	case <-tm.C():

	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Real.NewTimer did not fire within budget")
	}
}

func TestRealNewTimerStop(t *testing.T) {
	c := clock.Real{}
	tm := c.NewTimer(time.Hour)
	if !tm.Stop() {
		t.Fatalf("Real timer Stop returned false on unexpired timer")
	}
	select {
	case <-tm.C():
		t.Fatalf("stopped Real timer fired")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRealNewTickerFires(t *testing.T) {
	c := clock.Real{}
	tk := c.NewTicker(5 * time.Millisecond)
	got := 0
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case <-tk.C():
			got++
			if got >= 2 {
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	tk.Stop()
	if got < 2 {
		t.Fatalf("Real.NewTicker fired %d times, want >=2", got)
	}
}

func TestFakeSince(t *testing.T) {
	base := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(base)
	earlier := base.Add(-3 * time.Second)
	if got := f.Since(earlier); got != 3*time.Second {
		t.Fatalf("Fake.Since: got %v want 3s", got)
	}
	f.Advance(2 * time.Second)
	if got := f.Since(earlier); got != 5*time.Second {
		t.Fatalf("Fake.Since after Advance: got %v want 5s", got)
	}
}

func TestFakeNewTickerPanicOnNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -1 * time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			f := clock.NewFake(time.Unix(0, 0))
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("NewTicker(%v) did not panic", d)
				}
			}()
			_ = f.NewTicker(d)
		})
	}
}

func TestFakeAdvancePanicOnNegative(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Advance(-1s) did not panic")
		}
	}()
	f.Advance(-time.Second)
}

func TestFakeBlockUntilNSuccess(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tk := f.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	go func() {

		for range tk.C() {
		}
	}()
	f.Advance(30 * time.Millisecond)
	f.BlockUntilN(3, time.Second)

}

func TestFakeBlockUntilNTimeout(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	start := time.Now()
	f.BlockUntilN(5, 10*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Fatalf("BlockUntilN returned in %v, expected >=10ms", elapsed)
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("BlockUntilN took %v, expected <=500ms", elapsed)
	}
}

func TestFakeTickerStopDuringRun(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tk := f.NewTicker(50 * time.Millisecond)
	tk.Stop()
	f.Advance(500 * time.Millisecond)
	select {
	case <-tk.C():
		t.Fatalf("stopped ticker fired")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestFakeStopDuringAdvanceMultiFire(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)

	f := clock.NewFake(time.Unix(0, 0))
	const n = 10
	timers := make([]clock.Timer, n)
	for i := 0; i < n; i++ {
		timers[i] = f.NewTimer(time.Duration(10*(i+1)) * time.Millisecond)
	}
	var firedAfter atomic.Int32

	stopped := make(chan struct{})
	go func() {
		<-timers[0].C()
		for i := 1; i < n; i++ {
			timers[i].Stop()
		}
		close(stopped)
	}()

	for i := 1; i < n; i++ {
		go func(tm clock.Timer) {
			select {
			case <-tm.C():
				firedAfter.Add(1)
			case <-time.After(200 * time.Millisecond):
			}
		}(timers[i])
	}

	f.Advance(time.Duration(10*(n+1)) * time.Millisecond)

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatalf("timers[0] consumer never ran")
	}

	_ = f.BlockUntilCondition(func() bool { return firedAfter.Load() > 0 }, 100*time.Millisecond)
	if got := firedAfter.Load(); got != 0 {
		t.Fatalf("phantom fire(s) after Stop: %d (IMPORTANT-4 fix missing or insufficient)", got)
	}
}

func TestFakeAdvanceFiresMultipleInOrder(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tA := f.NewTimer(30 * time.Millisecond)
	tB := f.NewTimer(10 * time.Millisecond)
	tC := f.NewTimer(20 * time.Millisecond)
	f.Advance(50 * time.Millisecond)
	read := func(tm clock.Timer, label string) time.Time {
		select {
		case v := <-tm.C():
			return v
		case <-time.After(50 * time.Millisecond):
			t.Fatalf("timer %s did not fire", label)
		}
		return time.Time{}
	}
	gotA := read(tA, "A")
	gotB := read(tB, "B")
	gotC := read(tC, "C")

	if !gotB.Before(gotC) || !gotC.Before(gotA) {
		t.Fatalf("ordering wrong: A=%v B=%v C=%v (want B<C<A)", gotA, gotB, gotC)
	}
}

func TestRealSleep(t *testing.T) {
	c := clock.Real{}
	start := time.Now()
	c.Sleep(2 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 2*time.Millisecond {
		t.Fatalf("Real.Sleep returned in %v, expected >=2ms", elapsed)
	}
}

func TestRealAfterFunc(t *testing.T) {
	c := clock.Real{}
	var fired atomic.Int32
	tm := c.AfterFunc(2*time.Millisecond, func() { fired.Add(1) })
	defer tm.Stop()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fired.Load() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Real.AfterFunc fn did not fire within 200ms")
}

func TestRealAfterFuncStop(t *testing.T) {
	c := clock.Real{}
	var fired atomic.Int32
	tm := c.AfterFunc(time.Hour, func() { fired.Add(1) })
	if !tm.Stop() {
		t.Fatalf("Real.AfterFunc Stop returned false on unfired timer")
	}
	time.Sleep(10 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("Real.AfterFunc fn ran after Stop")
	}
}

func TestFakeSleep(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	done := make(chan struct{})
	go func() {
		f.Sleep(50 * time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("Fake.Sleep returned without Advance")
	case <-time.After(20 * time.Millisecond):
	}
	f.Advance(50 * time.Millisecond)
	select {
	case <-done:

	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Fake.Sleep did not unblock after Advance past deadline")
	}
}

func TestFakeSleepZero(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	for _, d := range []time.Duration{0, -time.Second} {
		t.Run(d.String(), func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				f.Sleep(d)
				close(done)
			}()
			select {
			case <-done:

			case <-time.After(50 * time.Millisecond):
				t.Fatalf("Fake.Sleep(%v) did not return immediately", d)
			}
		})
	}
}

func TestFakeAfterFunc(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	var fired atomic.Int32
	tm := f.AfterFunc(50*time.Millisecond, func() { fired.Add(1) })
	defer tm.Stop()

	if fired.Load() != 0 {
		t.Fatalf("Fake.AfterFunc fired before Advance")
	}
	f.Advance(60 * time.Millisecond)
	ok := f.BlockUntilCondition(func() bool { return fired.Load() == 1 }, time.Second)
	if !ok {
		t.Fatalf("Fake.AfterFunc fn did not fire after Advance (fired=%d)", fired.Load())
	}
}

func TestFakeAfterFuncStop(t *testing.T) {
	gBefore := runtime.NumGoroutine()
	f := clock.NewFake(time.Unix(0, 0))
	var fired atomic.Int32
	tm := f.AfterFunc(time.Hour, func() { fired.Add(1) })
	if !tm.Stop() {
		t.Fatalf("Fake.AfterFunc Stop returned false on unfired timer")
	}

	f.Advance(2 * time.Hour)

	_ = f.BlockUntilCondition(func() bool { return fired.Load() > 0 }, 50*time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("Fake.AfterFunc fn ran after Stop")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= gBefore+1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > gBefore+1 {
		t.Fatalf("goroutine leak: had %d, now %d (AfterFunc watcher did not exit on Stop)", gBefore, got)
	}
}

func TestFakeNewTimerZeroFiresImmediately(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second, -time.Nanosecond} {
		t.Run(d.String(), func(t *testing.T) {
			f := clock.NewFake(time.Unix(0, 0))
			tm := f.NewTimer(d)
			select {
			case <-tm.C():

			case <-time.After(50 * time.Millisecond):
				t.Fatalf("NewTimer(%v) did not fire immediately (Real/Fake parity violation)", d)
			}
		})
	}
}

func TestFakeBlockUntilCondition(t *testing.T) {
	t.Run("already_true", func(t *testing.T) {
		f := clock.NewFake(time.Unix(0, 0))
		ok := f.BlockUntilCondition(func() bool { return true }, 100*time.Millisecond)
		if !ok {
			t.Fatalf("BlockUntilCondition with always-true predicate returned false")
		}
	})

	t.Run("becomes_true", func(t *testing.T) {
		f := clock.NewFake(time.Unix(0, 0))
		var flag atomic.Int32
		go func() {
			time.Sleep(5 * time.Millisecond)
			flag.Store(1)
		}()
		ok := f.BlockUntilCondition(func() bool { return flag.Load() == 1 }, time.Second)
		if !ok {
			t.Fatalf("BlockUntilCondition did not observe condition becoming true")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		f := clock.NewFake(time.Unix(0, 0))
		start := time.Now()
		ok := f.BlockUntilCondition(func() bool { return false }, 20*time.Millisecond)
		elapsed := time.Since(start)
		if ok {
			t.Fatalf("BlockUntilCondition with always-false predicate returned true")
		}
		if elapsed < 20*time.Millisecond {
			t.Fatalf("BlockUntilCondition returned in %v, expected >=20ms", elapsed)
		}
	})
}

func TestFakeTimerDoubleStop(t *testing.T) {
	f := clock.NewFake(time.Unix(0, 0))
	tm := f.NewTimer(time.Second)
	if !tm.Stop() {
		t.Fatalf("first Stop returned false")
	}
	if tm.Stop() {
		t.Fatalf("second Stop returned true; expected false (already stopped)")
	}
}
