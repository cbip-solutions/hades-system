package errors

import (
	"fmt"
	"testing"
)

func BenchmarkLookupHit(b *testing.B) {
	const target Code = "daemon.not-running"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lookup(target)
	}
}

func BenchmarkLookupMiss(b *testing.B) {
	const target Code = "definitely.not.a.real.code"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lookup(target)
	}
}

func BenchmarkLookupAllCodes(b *testing.B) {
	keys := make([]Code, 0, len(catalog))
	for k := range catalog {
		keys = append(keys, k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lookup(keys[i%len(keys)])
	}
}

func BenchmarkLookupRegressionGate(b *testing.B) {
	const threshold = 1000.0
	const target Code = "daemon.not-running"

	for i := 0; i < 10_000; i++ {
		_ = Lookup(target)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Lookup(target)
	}
	b.StopTimer()

	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	if nsPerOp > threshold {
		b.Errorf("Lookup ns/op = %.2f, exceeds threshold %.0f ns/op (suspected O(N) regression)", nsPerOp, threshold)
	} else {
		b.Logf("Lookup ns/op = %.2f (threshold %.0f ns/op) — within budget", nsPerOp, threshold)
	}
}

func BenchmarkNewConstructor(b *testing.B) {
	cause := fmt.Errorf("synthetic cause")
	ctx := map[string]string{"k": "v"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New("daemon.not-running", cause, ctx)
	}
}

func BenchmarkWrapConstructor(b *testing.B) {
	cause := fmt.Errorf("synthetic cause")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Wrap("daemon.not-running", cause)
	}
}
