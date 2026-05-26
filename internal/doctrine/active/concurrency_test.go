package active_test

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func versionedSchema(version int) *v1.Schema {
	marker := fmt.Sprintf("v%d", version)
	return &v1.Schema{
		SchemaVersion:   marker,
		DoctrineVersion: marker,
	}
}

func expectedInvariants(versions []int) map[[2]string]bool {
	set := make(map[[2]string]bool, len(versions))
	for _, v := range versions {
		marker := fmt.Sprintf("v%d", v)
		set[[2]string{marker, marker}] = true
	}
	return set
}

func TestConcurrency_Active_NoTornReads_ManyReaders_FewWriters(t *testing.T) {

	a := active.NewAccessor()
	versions := []int{1, 2, 3, 4, 5}
	reg := make(map[string]*v1.Schema, len(versions))
	for _, v := range versions {
		reg[fmt.Sprintf("v%d", v)] = versionedSchema(v)
	}
	a.SetRegistry(reg)
	if err := a.SetUserDefault("v1"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}

	expected := expectedInvariants(versions)

	var (
		readsPerformed atomic.Uint64
		tornReads      atomic.Uint64
		stop           atomic.Bool
	)

	numReaders := runtime.GOMAXPROCS(0) * 4
	numWriters := 4
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				got := a.Active()
				if got == nil {
					tornReads.Add(1)
					continue
				}
				inv := [2]string{got.SchemaVersion, got.DoctrineVersion}
				if !expected[inv] {
					tornReads.Add(1)
					t.Errorf("torn read: invariant %v not in expected set %v", inv, expected)
				}
				readsPerformed.Add(1)
			}
		}()
	}

	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; !stop.Load(); i++ {
				v := versions[(i+seed)%len(versions)]
				_ = a.SetUserDefault(fmt.Sprintf("v%d", v))
			}
		}(w)
	}

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if reads := readsPerformed.Load(); reads < 10_000 {
		t.Logf("warning: only %d reads performed in 200ms; consider increasing duration", reads)
	}
	if torn := tornReads.Load(); torn > 0 {
		t.Errorf("torn reads detected: %d (out of %d total reads)", torn, readsPerformed.Load())
	}
}

func TestConcurrency_For_NoTornReads_PerProjectChurn(t *testing.T) {
	// Per-project schemas churning via SetForProject + ClearForProject.
	// Reader concurrent with Set→Clear→Set cycles MUST see consistent
	// invariants (either the per-project schema's tuple or the
	// max-scope fallback's tuple — never a mix).

	a := active.NewAccessor()
	versions := []int{10, 20, 30}
	reg := make(map[string]*v1.Schema, len(versions)+1)
	for _, v := range versions {
		reg[fmt.Sprintf("v%d", v)] = versionedSchema(v)
	}
	reg["max-scope"] = versionedSchema(0)
	a.SetRegistry(reg)

	expected := expectedInvariants(append([]int{0}, versions...))
	projectID := "churn-project"

	var (
		readsPerformed atomic.Uint64
		tornReads      atomic.Uint64
		stop           atomic.Bool
	)
	var wg sync.WaitGroup
	numReaders := runtime.GOMAXPROCS(0) * 4

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				got := a.For(projectID)
				if got == nil {
					tornReads.Add(1)
					continue
				}
				inv := [2]string{got.SchemaVersion, got.DoctrineVersion}
				if !expected[inv] {
					tornReads.Add(1)
					t.Errorf("torn read on For(): invariant %v not in expected set %v", inv, expected)
				}
				readsPerformed.Add(1)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			v := versions[i%len(versions)]
			schema := versionedSchema(v)
			a.SetForProject(projectID, schema)
			if i%3 == 0 {
				a.ClearForProject(projectID)
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if reads := readsPerformed.Load(); reads < 10_000 {
		t.Logf("warning: only %d reads performed in 200ms", reads)
	}
	if torn := tornReads.Load(); torn > 0 {
		t.Errorf("torn reads on For() under churn: %d (out of %d total)", torn, readsPerformed.Load())
	}
}

func TestConcurrency_StableSnapshot_InFlightWorkerSeesPriorSchema(t *testing.T) {

	a := active.NewAccessor()
	a.SetRegistry(map[string]*v1.Schema{
		"max-scope": versionedSchema(0),
	})
	initialSchema := versionedSchema(100)
	a.SetForProject("worker-project", initialSchema)

	workerInvariantSeen := make(chan [2]string, 1)
	workerStarted := make(chan struct{})

	go func() {
		snapshot := a.For("worker-project")
		close(workerStarted)

		time.Sleep(80 * time.Millisecond)

		workerInvariantSeen <- [2]string{snapshot.SchemaVersion, snapshot.DoctrineVersion}
	}()

	<-workerStarted

	differentSchema := versionedSchema(200)
	a.SetForProject("worker-project", differentSchema)

	if got := a.For("worker-project"); got.DoctrineVersion != "v200" {
		t.Fatalf("post-Store For(worker-project).DoctrineVersion = %q; want v200 (Store didn't take effect)",
			got.DoctrineVersion)
	}

	inv := <-workerInvariantSeen
	expected := [2]string{"v100", "v100"}
	if inv != expected {
		t.Errorf("inv-zen-092 violation: worker snapshot invariant = %v; want %v "+
			"(in-flight worker must see prior pointer's data even after concurrent Store)",
			inv, expected)
	}
}

func TestConcurrency_SetRegistry_AtomicReplace_NoTornReads(t *testing.T) {
	// SetRegistry replaces the entire registry atomically. Concurrent
	// Active() calls during the replace MUST see either the old
	// registry's fallback OR the new registry's fallback, never a
	// mid-replace nil/half-state.

	a := active.NewAccessor()

	versions := []int{1, 2, 3, 4, 5}
	expected := expectedInvariants(versions)

	var (
		readsPerformed atomic.Uint64
		tornReads      atomic.Uint64
		stop           atomic.Bool
	)
	var wg sync.WaitGroup

	a.SetRegistry(map[string]*v1.Schema{"max-scope": versionedSchema(1)})

	numReaders := runtime.GOMAXPROCS(0) * 4
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				got := a.Active()
				if got == nil {
					tornReads.Add(1)
					continue
				}
				inv := [2]string{got.SchemaVersion, got.DoctrineVersion}
				if !expected[inv] {
					tornReads.Add(1)
					t.Errorf("torn read during SetRegistry churn: %v not in %v", inv, expected)
				}
				readsPerformed.Add(1)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			v := versions[i%len(versions)]
			reg := map[string]*v1.Schema{"max-scope": versionedSchema(v)}
			a.SetRegistry(reg)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if torn := tornReads.Load(); torn > 0 {
		t.Errorf("torn reads during SetRegistry churn: %d", torn)
	}
}

func TestConcurrency_MixedAllOperations(t *testing.T) {

	a := active.NewAccessor()
	versions := []int{1, 2, 3, 4, 5, 10, 20, 30, 40, 50}
	reg := make(map[string]*v1.Schema, len(versions)+1)
	for _, v := range versions {
		reg[fmt.Sprintf("v%d", v)] = versionedSchema(v)
	}
	reg["max-scope"] = versionedSchema(0)
	a.SetRegistry(reg)
	if err := a.SetUserDefault("v1"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}

	expected := expectedInvariants(append([]int{0}, versions...))

	var (
		reads     atomic.Uint64
		tornReads atomic.Uint64
		stop      atomic.Bool
	)
	var wg sync.WaitGroup

	numReaders := runtime.GOMAXPROCS(0) * 8
	projectIDs := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; !stop.Load(); j++ {
				var got *v1.Schema
				if j%2 == 0 {
					got = a.Active()
				} else {
					got = a.For(projectIDs[(j+seed)%len(projectIDs)])
				}
				if got == nil {
					tornReads.Add(1)
					continue
				}
				inv := [2]string{got.SchemaVersion, got.DoctrineVersion}
				if !expected[inv] {
					tornReads.Add(1)
					t.Errorf("torn mixed-read: %v not in %v", inv, expected)
				}
				reads.Add(1)
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = a.SetUserDefault(fmt.Sprintf("v%d", versions[i%len(versions)]))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			pid := projectIDs[i%len(projectIDs)]
			v := versions[i%len(versions)]
			if i%4 == 0 {
				a.ClearForProject(pid)
			} else {
				a.SetForProject(pid, versionedSchema(v))
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			time.Sleep(time.Millisecond)
			newReg := make(map[string]*v1.Schema, len(versions)+1)
			for _, v := range versions {
				newReg[fmt.Sprintf("v%d", v)] = versionedSchema(v)
			}
			newReg["max-scope"] = versionedSchema(0)
			a.SetRegistry(newReg)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if reads.Load() < 10_000 {
		t.Logf("warning: only %d reads in 300ms mixed test", reads.Load())
	}
	if torn := tornReads.Load(); torn > 0 {
		t.Errorf("torn reads in mixed-operation stress: %d", torn)
	}
}
