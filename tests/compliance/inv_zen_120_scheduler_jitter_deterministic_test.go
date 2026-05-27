// Package compliance — invariant: scheduler jitter is deterministic.
//
// Spec §1 Q9 C / §7.2 invariant wording:
//
// "Scheduler jitter offset MUST be deterministic — hash(routine_id) %
// (10% × period), capped at 15min recurring / 90s one-shot."
//
// This test is the cross-package, boundary-side witness. The in-package
// test surface in internal/scheduler/jitter_test.go locks
// per-implementation behaviour; this file re-asserts the contract from
// outside the package so any future refactor (e.g. swapping the hash
// primitive, lifting the bucket math, splitting the cap branches into
// a doctrine-driven matrix) gets caught at the public surface.
//
// Coverage matrix:
//
// (a) Determinism across 10000 invocations on the same input — locks
// the "no map iteration / no random source / no init-time RNG"
// contract that makes daemon.db replay reproducible.
// (b) Concurrency-safety: 100 goroutines × 100 calls each on the same
// input return the same value. Defends against accidental
// package-level mutable state introduced by a future caching layer.
// (c) Order-independence: interleaving foreign IDs in between calls
// on the witness ID does not change the witness ID's offset.
// (d) Recurring cap (≥1h period): 15min ceiling holds across 10000
// distinct IDs. Saturation tests prove the cap branch is reached.
// (e) One-shot cap (<1h period): 90s ceiling holds across 10000
// distinct IDs.
// (f) Cross-process determinism: a child process (started via
// `go run` against a tiny generator) produces byte-identical
// output. Catches accidental use of process-local randomness
// (e.g. crypto/rand instead of crypto/sha256, time-based seed,
// map iteration). The generator source is written to a temp dir
// so the test is self-contained and survives `go test -race`.
//
// Boundary: this test imports only internal/scheduler +
// stdlib + the in-tests testharness. It does NOT touch internal/store,
// internal/providers, or private-tier1-module.
//
// Inv-zen-120 contract.
package compliance

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

const witnessRoutineID = "01HZ7K8M9P2Q3R4S5T6V7W8X9Y"

func TestInvZen120JitterDeterministicAcrossInvocations(t *testing.T) {
	t.Parallel()
	period := time.Hour
	want := scheduler.ComputeJitter(witnessRoutineID, period)
	for i := 0; i < 10000; i++ {
		got := scheduler.ComputeJitter(witnessRoutineID, period)
		if got != want {
			t.Fatalf("inv-zen-120 violated at i=%d: got %v, want %v", i, got, want)
		}
	}
}

func TestInvZen120JitterDeterministicAcrossGoroutines(t *testing.T) {
	t.Parallel()
	period := time.Hour
	want := scheduler.ComputeJitter(witnessRoutineID, period)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got := scheduler.ComputeJitter(witnessRoutineID, period)
				if got != want {
					t.Errorf("inv-zen-120 violated in goroutine: got %v, want %v", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestInvZen120JitterOrderIndependent(t *testing.T) {
	t.Parallel()
	period := time.Hour
	want := scheduler.ComputeJitter(witnessRoutineID, period)
	for i := 0; i < 1000; i++ {

		_ = scheduler.ComputeJitter(fmt.Sprintf("foreign-%d", i), period)
		got := scheduler.ComputeJitter(witnessRoutineID, period)
		if got != want {
			t.Fatalf("inv-zen-120 violated after foreign-%d: got %v, want %v", i, got, want)
		}
	}
}

func TestInvZen120RecurringCap15Min(t *testing.T) {
	t.Parallel()
	period := 24 * time.Hour
	cap15min := 15 * time.Minute
	saturatedAtCap := 0
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("recurring-routine-%d", i)
		got := scheduler.ComputeJitter(id, period)
		if got > cap15min {
			t.Fatalf("inv-zen-120 cap violated: id=%q jitter=%v > 15min", id, got)
		}
		if got == cap15min {
			saturatedAtCap++
		}
		if got < 0 {
			t.Fatalf("inv-zen-120 negative jitter: id=%q jitter=%v", id, got)
		}
	}

	if saturatedAtCap == 0 {
		t.Errorf("inv-zen-120 cap branch never reached across 10000 IDs at 24h period; cap may be dead code")
	}
}

func TestInvZen120OneShotCap90s(t *testing.T) {
	t.Parallel()
	period := 30 * time.Minute
	cap90s := 90 * time.Second
	saturatedAtCap := 0
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("oneshot-routine-%d", i)
		got := scheduler.ComputeJitter(id, period)
		if got > cap90s {
			t.Fatalf("inv-zen-120 cap violated: id=%q jitter=%v > 90s", id, got)
		}
		if got == cap90s {
			saturatedAtCap++
		}
		if got < 0 {
			t.Fatalf("inv-zen-120 negative jitter: id=%q jitter=%v", id, got)
		}
	}
	if saturatedAtCap == 0 {
		t.Errorf("inv-zen-120 cap branch never reached across 10000 IDs at 30min period; cap may be dead code")
	}
}

func TestInvZen120BoundedBy10Percent(t *testing.T) {
	t.Parallel()
	period := time.Hour
	bucket := period / 10
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("bounded-routine-%d", i)
		got := scheduler.ComputeJitter(id, period)
		if got >= bucket {
			t.Fatalf("inv-zen-120 10%% bound violated: id=%q jitter=%v >= bucket=%v",
				id, got, bucket)
		}
		if got < 0 {
			t.Fatalf("inv-zen-120 negative jitter: id=%q jitter=%v", id, got)
		}
	}
}

func TestInvZen120ZeroAndNegativePeriodReturnZero(t *testing.T) {
	t.Parallel()
	for _, period := range []time.Duration{0, -time.Second, -time.Hour} {
		got := scheduler.ComputeJitter(witnessRoutineID, period)
		if got != 0 {
			t.Errorf("inv-zen-120 degenerate-input contract violated: period=%v got=%v want=0",
				period, got)
		}
	}
}

func TestInvZen120CrossProcessDeterminism(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain unavailable: %v", err)
	}
	if testing.Short() {
		t.Skip("skipping cross-process spawn in -short mode")
	}
	root := repoRoot(t)
	tmp, err := os.MkdirTemp(filepath.Join(root, "tests", "compliance"), ".jitter-probe-tmp-")
	if err != nil {
		t.Fatalf("mkdir module-internal tmp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmp); err != nil {
			t.Logf("cleanup tmp %s: %v", tmp, err)
		}
	})
	src := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(src, []byte(jitterGeneratorSource), 0o644); err != nil {
		t.Fatalf("write generator source: %v", err)
	}

	out1, err := runGenerator(t, src)
	if err != nil {
		t.Fatalf("first generator run: %v", err)
	}
	out2, err := runGenerator(t, src)
	if err != nil {
		t.Fatalf("second generator run: %v", err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatalf("inv-zen-120 cross-process determinism violated:\nfirst:  %s\nsecond: %s",
			string(out1), string(out2))
	}

	want := scheduler.ComputeJitter(witnessRoutineID, time.Hour)
	expectedLine := fmt.Sprintf("%s\t%d", witnessRoutineID, int64(want))
	if !bytes.Contains(out1, []byte(expectedLine)) {
		t.Errorf("inv-zen-120 cross-process / in-process drift: generator output does not contain witness line %q\noutput:\n%s",
			expectedLine, string(out1))
	}
}

func runGenerator(t *testing.T, src string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("go", "run", src)

	cmd.Env = os.Environ()
	cmd.Dir = repoRoot(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Logf("generator stderr: %s", stderr.String())
		return nil, fmt.Errorf("go run: %w", err)
	}
	return bytes.TrimRight(stdout.Bytes(), "\n"), nil
}

const jitterGeneratorSource = `package main

import (
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func main() {
	ids := []string{
		"01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		"alpha", "beta", "gamma", "delta", "epsilon",
	}
	periods := []time.Duration{time.Hour, 24 * time.Hour, 30 * time.Minute}
	for _, p := range periods {
		for _, id := range ids {
			d := scheduler.ComputeJitter(id, p)
			fmt.Printf("%s\t%d\n", id, int64(d))
		}
	}
}
`

func TestInvZen120GeneratorSourceImportPath(t *testing.T) {
	t.Parallel()
	if !strings.Contains(jitterGeneratorSource,
		`"github.com/cbip-solutions/hades-system/internal/scheduler"`) {
		t.Errorf("inv-zen-120 generator source out of sync with module path; update jitterGeneratorSource constant")
	}

	if strings.Contains(jitterGeneratorSource, "runtime.") {
		t.Errorf("inv-zen-120 generator source must not import runtime; OS-specific behaviour breaks cross-process determinism")
	}

	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "netbsd", "openbsd":

	default:
		t.Logf("inv-zen-120 cross-process spawn untested on GOOS=%s", runtime.GOOS)
	}
}
