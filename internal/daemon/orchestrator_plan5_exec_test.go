package daemon

import (
	"context"
	"testing"
	"time"
)

func TestOsExecer_ConcurrencyCapped(t *testing.T) {

	for i := 0; i < execMaxConcurrent; i++ {
		if !execSem.TryAcquire(1) {
			t.Fatalf("slot %d should be free", i)
		}
	}
	if execSem.TryAcquire(1) {
		t.Fatal("the (N+1)th acquire must fail — concurrency not capped")
	}
	for i := 0; i < execMaxConcurrent; i++ {
		execSem.Release(1)
	}
}

func TestOsExecer_TimeoutKillsProcessGroup(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, _ = osExecer{}.Run(ctx, "sh", "-c", "sleep 30 & wait")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Run hung past WaitDelay: %v (process group not killed)", elapsed)
	}
}
