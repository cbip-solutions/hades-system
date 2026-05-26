//go:build realworld
// +build realworld

package plan9_mps_embedder_realworld_test

import (
	"context"
	"math"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
)

func TestRealworld_MacMPSEmbedderLatency(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Mac MPS only available on darwin")
	}
	if os.Getenv("ZEN_TEST_REAL_EMBEDDER") != "1" {
		t.Skip("set ZEN_TEST_REAL_EMBEDDER=1 to enable; requires staged Python env + scripts/zen_embed.py")
	}
	if testing.Short() {
		t.Skip("realworld skipped under -short")
	}

	scriptPath := os.Getenv("ZEN_TEST_MPS_SCRIPT_PATH")
	if scriptPath == "" {
		t.Skip("ZEN_TEST_MPS_SCRIPT_PATH not set; cannot locate scripts/zen_embed.py")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mps, err := embed.NewMPSEmbedder(embed.MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
		Model:      "all-mpnet-base-v2",
	})
	if err != nil {
		t.Skipf("NewMPSEmbedder unavailable: %v", err)
	}
	defer func() { _ = mps.Close() }()
	if mps.Dimensions() != 384 {
		t.Errorf("Dimensions = %d, want 384", mps.Dimensions())
	}

	for i := 0; i < 3; i++ {
		if _, err := mps.Embed(ctx, "warmup query"); err != nil {
			t.Fatalf("warmup Embed[%d]: %v", i, err)
		}
	}

	start := time.Now()
	v1, err := mps.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed[1]: %v", err)
	}
	latency := time.Since(start)
	if latency > 500*time.Millisecond {
		t.Errorf("warm-call latency = %v, want < 500ms", latency)
	}
	if len(v1) != 384 {
		t.Errorf("Embed returned %d-dim vector, want 384", len(v1))
	}

	v2, err := mps.Embed(ctx, "hello universe")
	if err != nil {
		t.Fatalf("Embed[2]: %v", err)
	}
	mpsSim := cosine(v1, v2)

	mock := embed.NewMockEmbedder(384).WithTokenSumMode()
	m1, err := mock.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("mock Embed[1]: %v", err)
	}
	m2, err := mock.Embed(ctx, "hello universe")
	if err != nil {
		t.Fatalf("mock Embed[2]: %v", err)
	}
	mockSim := cosine(m1, m2)

	if mpsSim <= mockSim {
		t.Errorf("MPS semantic similarity %.3f did not exceed mock token-sum %.3f", mpsSim, mockSim)
	}
	t.Logf("MPS cosine(hello-world, hello-universe) = %.3f; mock token-sum = %.3f", mpsSim, mockSim)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
