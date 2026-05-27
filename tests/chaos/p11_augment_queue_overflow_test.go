// go:build chaos
package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestQueueOverflow_AggregatorErrorsPropagate(t *testing.T) {
	fake := testharness.NewAggregatorFake()
	fake.InjectError(testharness.AggregatorOpFTS, errors.New("chaos: knowledge index unavailable"))
	fake.InjectError(testharness.AggregatorOpVec, errors.New("chaos: vec down"))
	fake.InjectError(testharness.AggregatorOpGraph, errors.New("chaos: graph down"))

	c := augment.NewAggregatorConsumer(fake, fake)

	const n = 200
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := c.Lane2FTS(ctx, "x", 10)
			if err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()

	if int(failures.Load()) != n {
		t.Errorf("failures = %d, want %d (every call must surface error)", failures.Load(), n)
	}

	ftsCalls := 0
	for _, c := range fake.Calls() {
		if c.Op == testharness.AggregatorOpFTS {
			ftsCalls++
		}
	}
	if ftsCalls != n {
		t.Errorf("FTS call count = %d, want %d (call log corruption under storm)", ftsCalls, n)
	}
}

func TestQueueOverflow_SlowAggregatorRespectsContext(t *testing.T) {
	slow := &slowAggregator{delay: 100 * time.Millisecond}
	c := augment.NewAggregatorConsumer(slow, slow)

	const n = 50
	var wg sync.WaitGroup
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, _ = c.Lane2FTS(ctx, "x", 10)
		}()
	}
	wg.Wait()

	if time.Now().After(deadline) {
		t.Fatalf("queue overflow caused goroutines to outlive the test deadline")
	}
}

type slowAggregator struct {
	delay time.Duration
}

func (s *slowAggregator) QueryFTS(ctx context.Context, queryText string, limit int) ([]augment.QueryResult, error) {
	select {
	case <-time.After(s.delay):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowAggregator) QueryVec(ctx context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	select {
	case <-time.After(s.delay):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowAggregator) QueryGraph(ctx context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	select {
	case <-time.After(s.delay):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowAggregator) Embed(ctx context.Context, _ string) ([]float32, error) {
	select {
	case <-time.After(s.delay):
		return []float32{0}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
