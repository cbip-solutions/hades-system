package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthSampler_SamplesInBackgroundAndServesCache(t *testing.T) {
	var calls atomic.Int64
	compute := func(ctx context.Context) HealthSnapshot {
		calls.Add(1)
		return HealthSnapshot{SampledAt: time.Now(), Deps: map[string]DepHealth{"x": {Up: true}}}
	}
	s := NewHealthSampler(compute, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if d, ok := s.Current().Get("x"); ok && d.Up {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sampler never populated the snapshot")
		}
		time.Sleep(time.Millisecond)
	}

	before := calls.Load()
	for i := 0; i < 1000; i++ {
		_ = s.Current()
	}
	if calls.Load() != before {
		t.Fatalf("Current() triggered compute: before=%d after=%d", before, calls.Load())
	}
	cancel()
	<-done
}

func TestHealthSnapshot_GetReportsPerDependency(t *testing.T) {
	snap := HealthSnapshot{
		SampledAt: time.Unix(1000, 0),
		Deps: map[string]DepHealth{
			"research_mcp_up": {Up: true},
			"gitnexus_up":     {Up: false, Detail: "not on PATH"},
		},
	}
	if d, ok := snap.Get("research_mcp_up"); !ok || !d.Up {
		t.Fatalf("research_mcp_up: want up+ok, got %+v ok=%v", d, ok)
	}
	if d, ok := snap.Get("gitnexus_up"); !ok || d.Up || d.Detail != "not on PATH" {
		t.Fatalf("gitnexus_up: want down+detail, got %+v", d)
	}
	if _, ok := snap.Get("unknown"); ok {
		t.Fatal("unknown dependency must report ok=false")
	}
}
