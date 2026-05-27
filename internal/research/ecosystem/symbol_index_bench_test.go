// go:build cgo
//go:build cgo
// +build cgo

package ecosystem

import (
	"strconv"
	"sync/atomic"
	"testing"
)

func benchPopulateSymbols(idx *SymbolIndex, n int) {
	for i := 0; i < n; i++ {
		idx.Register(EcoGo, "pkg.Sym"+strconv.Itoa(i), "1.0")
	}
}

func BenchmarkSymbolIndex_Contains_Cold10k(b *testing.B) {
	idx := NewSymbolIndex()
	const N = 10000
	benchPopulateSymbols(idx, N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := "pkg.Sym" + strconv.Itoa(i%N)
		_ = idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"})
	}
}

func BenchmarkSymbolIndex_Contains_Parallel10k(b *testing.B) {
	idx := NewSymbolIndex()
	const N = 10000
	benchPopulateSymbols(idx, N)

	var ops int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := "pkg.Sym" + strconv.Itoa(i%N)
			_ = idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"})
			atomic.AddInt64(&ops, 1)
			i++
		}
	})
	b.ReportMetric(float64(ops)/b.Elapsed().Seconds(), "ops/sec")
}

func BenchmarkSymbolIndex_Register(b *testing.B) {
	idx := NewSymbolIndex()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Register(EcoGo, "p.S"+strconv.Itoa(i), "1.0")
	}
}
