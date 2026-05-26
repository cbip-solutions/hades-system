package orchestrator_test

import (
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

func TestTierHealthEmptyWindowReturnsZeroRate(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	if r := th.ErrorRate(); r != 0 {
		t.Errorf("ErrorRate empty = %v, want 0", r)
	}
}

func TestTierHealthAllSuccessReturnsZeroRate(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	for i := 0; i < 10; i++ {
		th.RecordSuccess()
	}
	if r := th.ErrorRate(); r != 0 {
		t.Errorf("ErrorRate all-success = %v, want 0", r)
	}
}

func TestTierHealthAllFailureReturnsOneRate(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	for i := 0; i < 10; i++ {
		th.RecordFailure()
	}
	if r := th.ErrorRate(); r != 1.0 {
		t.Errorf("ErrorRate all-fail = %v, want 1.0", r)
	}
}

func TestTierHealthMixedReturnsProportion(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	th.RecordSuccess()
	th.RecordSuccess()
	th.RecordFailure()
	th.RecordFailure()
	if r := th.ErrorRate(); r < 0.49 || r > 0.51 {
		t.Errorf("ErrorRate 2/4 = %v, want ~0.5", r)
	}
}

func TestTierHealthEvictsExpiredEntries(t *testing.T) {
	th := orchestrator.NewTierHealth(50 * time.Millisecond)
	th.RecordFailure()
	th.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	th.RecordSuccess()
	if r := th.ErrorRate(); r > 0 {
		t.Errorf("ErrorRate after eviction = %v, want 0", r)
	}
}

func TestTierHealthConsecutiveFailures(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	th.RecordFailure()
	th.RecordFailure()
	th.RecordFailure()
	if cf := th.ConsecutiveFailures(); cf != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", cf)
	}
	th.RecordSuccess()
	if cf := th.ConsecutiveFailures(); cf != 0 {
		t.Errorf("ConsecutiveFailures after success = %d, want 0", cf)
	}
}

func TestTierHealthNewRejectsZeroOrNegativeWindow(t *testing.T) {
	tests := []struct {
		name   string
		window time.Duration
	}{
		{"zero", 0},
		{"negative", -1 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			var th *orchestrator.TierHealth
			require := func() {
				th = orchestrator.NewTierHealth(tc.window)
			}
			panicked := func() (panicked bool) {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
					}
				}()
				require()
				return false
			}()
			if panicked {
				t.Fatalf("NewTierHealth(%v) panicked; want fallback to 5min default", tc.window)
			}

			th.RecordFailure()
			if r := th.ErrorRate(); r != 1.0 {
				t.Errorf("ErrorRate after one failure = %v, want 1.0", r)
			}
		})
	}
}

func TestTierHealthErrorRateAfterEvictionResetsToZero(t *testing.T) {
	th := orchestrator.NewTierHealth(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		th.RecordFailure()
	}

	if r := th.ErrorRate(); r != 1.0 {
		t.Fatalf("ErrorRate before eviction = %v, want 1.0", r)
	}

	time.Sleep(100 * time.Millisecond)

	if r := th.ErrorRate(); r != 0 {
		t.Errorf("ErrorRate after full eviction = %v, want 0", r)
	}
}

func TestTierHealthConcurrentRecordRaceClean(t *testing.T) {
	const goroutines = 8
	const callsPerGoroutine = 100

	th := orchestrator.NewTierHealth(5 * time.Minute)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				if (i+id)%2 == 0 {
					th.RecordSuccess()
				} else {
					th.RecordFailure()
				}
			}
		}(g)
	}
	wg.Wait()

	rate := th.ErrorRate()
	if rate < 0 || rate > 1 {
		t.Errorf("ErrorRate after concurrent writes = %v, want [0, 1]", rate)
	}

	cf := th.ConsecutiveFailures()
	if cf < 0 {
		t.Errorf("ConsecutiveFailures = %d, want >= 0", cf)
	}
}

func TestTierHealthConsecutiveFailuresPersistsAcrossEviction(t *testing.T) {
	th := orchestrator.NewTierHealth(50 * time.Millisecond)
	th.RecordFailure()
	th.RecordFailure()
	th.RecordFailure()

	if cf := th.ConsecutiveFailures(); cf != 3 {
		t.Fatalf("ConsecutiveFailures before eviction = %d, want 3", cf)
	}

	time.Sleep(100 * time.Millisecond)

	if r := th.ErrorRate(); r != 0 {
		t.Errorf("ErrorRate after eviction = %v, want 0 (outcomes expired)", r)
	}
	if cf := th.ConsecutiveFailures(); cf != 3 {
		t.Errorf("ConsecutiveFailures after eviction = %d, want 3 (persists by design)", cf)
	}
}

func TestTierHealthMultipleSuccessesAfterFailures(t *testing.T) {
	th := orchestrator.NewTierHealth(5 * time.Minute)
	th.RecordFailure()
	th.RecordFailure()
	th.RecordFailure()
	if cf := th.ConsecutiveFailures(); cf != 3 {
		t.Fatalf("ConsecutiveFailures = %d, want 3", cf)
	}

	th.RecordSuccess()
	if cf := th.ConsecutiveFailures(); cf != 0 {
		t.Errorf("ConsecutiveFailures after 1st success = %d, want 0", cf)
	}

	th.RecordSuccess()
	if cf := th.ConsecutiveFailures(); cf != 0 {
		t.Errorf("ConsecutiveFailures after 2nd success = %d, want 0", cf)
	}

	r := th.ErrorRate()
	if r < 0.59 || r > 0.61 {
		t.Errorf("ErrorRate after 3 failures + 2 successes = %v, want ~0.6", r)
	}
}
