//go:build property && cgo

package ecosystem_property_test

import (
	"sort"
	"strconv"
	"testing"
	"testing/quick"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestSymbolIndex_Property_ContainsLatencyP99(t *testing.T) {
	idx := ecosystem.NewSymbolIndex()
	const N = 10000
	for i := 0; i < N; i++ {
		eco := []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		}[i%4]
		idx.Register(eco, "pkg"+strconv.Itoa(i%2000)+".Sym"+strconv.Itoa(i), "1."+strconv.Itoa(i%50))
	}

	latencies := make([]time.Duration, 0, 1000)
	config := &quick.Config{MaxCount: 1000}
	gen := func(seed int) bool {
		eco := []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		}[(seed%4+4)%4]
		path := "pkg" + strconv.Itoa((seed%4000+4000)%4000) + ".Sym" + strconv.Itoa((seed%10000+10000)%10000)
		version := "1." + strconv.Itoa((seed%100+100)%100)
		start := time.Now()
		_ = idx.Contains(ecosystem.SymbolRef{Ecosystem: eco, SymbolPath: path, Version: version})
		latencies = append(latencies, time.Since(start))
		return true
	}
	if err := quick.Check(gen, config); err != nil {
		t.Fatalf("quick.Check: %v", err)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p99 := latencies[int(float64(len(latencies))*0.99)]
	t.Logf("inv-zen-195 p99 over 1000 random queries: %v", p99)
	if p99 > 1*time.Millisecond {
		t.Errorf("inv-zen-195 violation: p99=%v > 1ms", p99)
	}
}

func TestSymbolIndex_Property_AnyVersion_AlwaysHitsAnyVersion(t *testing.T) {
	idx := ecosystem.NewSymbolIndex()
	gen := func(eco int, pathSeed int, versionSeed int) bool {
		ecoVal := []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		}[(eco%4+4)%4]
		path := "p" + strconv.Itoa(pathSeed)
		version := "v" + strconv.Itoa(versionSeed)
		idx.Register(ecoVal, path, version)
		return idx.Contains(ecosystem.SymbolRef{Ecosystem: ecoVal, SymbolPath: path, Version: ""})
	}
	if err := quick.Check(gen, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("any-version property violated: %v", err)
	}
}

func TestSymbolIndex_Property_VersionedOnlyHitsMatchingVersion(t *testing.T) {
	idx := ecosystem.NewSymbolIndex()
	gen := func(seedA, seedB int) bool {
		eco := ecosystem.EcoGo
		path := "pkg.Sym" + strconv.Itoa(seedA)
		vA := "1." + strconv.Itoa(seedA)
		vB := "2." + strconv.Itoa(seedB)
		if vA == vB {
			return true
		}
		idx.Register(eco, path, vA)

		return idx.Contains(ecosystem.SymbolRef{Ecosystem: eco, SymbolPath: path, Version: vA}) &&
			!idx.Contains(ecosystem.SymbolRef{Ecosystem: eco, SymbolPath: path, Version: vB})
	}
	if err := quick.Check(gen, &quick.Config{MaxCount: 500}); err != nil {
		t.Errorf("versioned-only property violated: %v", err)
	}
}
