// go:build integration
package ecosystem

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestJinaCodeEmbeddings_Integration_RealSubprocess(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping real-subprocess integration test")
	}
	probe := exec.Command("python3", "-c", "import sentence_transformers")
	if err := probe.Run(); err != nil {
		t.Skip("sentence-transformers not installed; skipping real-subprocess integration test")
	}

	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   false,
		BatchSize:  16,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin, fp32, err := emb.EmbedBoth(ctx, "package main\n\nfunc main() {}")
	if err != nil {
		t.Fatalf("EmbedBoth real: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("real bin len=%d, want 32", len(bin))
	}
	if len(fp32) != 1536 {
		t.Errorf("real fp32 len=%d, want 1536", len(fp32))
	}

	wantBin := quantizeBinary256(fp32[:256])
	if string(bin) != string(wantBin) {
		t.Errorf("real cross-shape inconsistent")
	}
}

func TestJinaCodeEmbeddings_Integration_LatencyBudget(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	probe := exec.Command("python3", "-c", "import sentence_transformers, torch; assert torch.backends.mps.is_available()")
	if err := probe.Run(); err != nil {
		t.Skip("MPS not available; record-only test skipped")
	}

	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   false,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := emb.EmbedBinary256d(ctx, "warmup"); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	const N = 100
	latencies := make([]time.Duration, N)
	for i := 0; i < N; i++ {
		start := time.Now()
		if _, err := emb.EmbedBinary256d(ctx, "func helloWorld()"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		latencies[i] = time.Since(start)
	}
	sortDurations(latencies)
	p95 := latencies[int(float64(N)*0.95)]
	t.Logf("M4 MPS jina-code-1.5b query-encode p95 = %v (target <50ms)", p95)

}

func sortDurations(d []time.Duration) {

	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}
