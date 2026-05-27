// go:build darwin
//go:build darwin
// +build darwin

package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestMPSEmbedderRespectsContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; skip MPS subprocess test")
	}
	scriptPath := os.Getenv("ZEN_EMBED_PYTHON_SCRIPT")
	if scriptPath == "" {

		scriptPath = "scripts/zen_embed.py"
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Skipf("zen_embed.py not found at %s; skip", scriptPath)
	}
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Skipf("NewMPSEmbedder unavailable (sentence-transformers not installed?): %v", err)
	}
	defer e.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestMPSEmbedderReturnsCorrectDimensions(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	scriptPath := os.Getenv("ZEN_EMBED_PYTHON_SCRIPT")
	if scriptPath == "" {
		scriptPath = "scripts/zen_embed.py"
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Skipf("zen_embed.py not found at %s", scriptPath)
	}
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Skipf("NewMPSEmbedder unavailable: %v", err)
	}
	defer e.Close()
	v, err := e.Embed(context.Background(), "hello world")
	if err != nil {

		t.Skipf("Embed unavailable (sentence-transformers not installed?): %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d; want 384", len(v))
	}
}

func TestMPSEmbedderUnavailableScriptReturnsError(t *testing.T) {
	_, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: "/does/not/exist.py",
		Dimensions: 384,
	})
	if err == nil {
		t.Error("expected error for missing script")
	}
	if !errors.Is(err, ErrMPSUnavailable) && !os.IsNotExist(err) {
		t.Errorf("error should be ErrMPSUnavailable or os.IsNotExist: %v", err)
	}
}

func TestMPSEmbedderEmptyScriptPathReturnsError(t *testing.T) {
	_, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: "",
		Dimensions: 384,
	})
	if err == nil {
		t.Error("expected error for empty ScriptPath")
	}
	if !errors.Is(err, ErrMPSUnavailable) {
		t.Errorf("error should wrap ErrMPSUnavailable: %v", err)
	}
}

func TestMPSEmbedderInvalidDimensionsReturnsError(t *testing.T) {
	_, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: "scripts/zen_embed.py",
		Dimensions: 0,
	})
	if err == nil {
		t.Error("expected error for Dimensions=0")
	}
}

func TestMPSEmbedderDefaultPythonPath(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	scriptPath := writeTempScript(t, 4)

	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "",
		ScriptPath: scriptPath,
		Dimensions: 4,
	})
	if err != nil {
		t.Fatalf("NewMPSEmbedder with empty PythonPath: %v", err)
	}
	defer e.Close()
	if e.Dimensions() != 4 {
		t.Errorf("Dimensions = %d; want 4", e.Dimensions())
	}
}

func TestMPSEmbedderDimensionsMethod(t *testing.T) {
	scriptPath := os.Getenv("ZEN_EMBED_PYTHON_SCRIPT")
	if scriptPath == "" {
		scriptPath = "scripts/zen_embed.py"
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Skipf("zen_embed.py not found at %s", scriptPath)
	}
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 512,
	})
	if err != nil {
		t.Skipf("NewMPSEmbedder unavailable: %v", err)
	}
	defer e.Close()
	if e.Dimensions() != 512 {
		t.Errorf("Dimensions = %d; want 512", e.Dimensions())
	}
}

func TestMPSEmbedderCloseIdempotent(t *testing.T) {
	scriptPath := os.Getenv("ZEN_EMBED_PYTHON_SCRIPT")
	if scriptPath == "" {
		scriptPath = "scripts/zen_embed.py"
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Skipf("zen_embed.py not found at %s", scriptPath)
	}
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Skipf("NewMPSEmbedder unavailable: %v", err)
	}

	if err := e.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func writeTempScript(t *testing.T, n int) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "zen_embed_stub_*.py")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()
	dims := make([]string, n)
	for i := range dims {
		dims[i] = "0.0"
	}
	script := `#!/usr/bin/env python3
import json, sys, math

dims = ` + fmt.Sprintf("%d", n) + `
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    text = req.get("text", "")
    emb = [math.sin(i * 0.1 + hash(text) * 0.0001) for i in range(dims)]
    # normalize
    mag = math.sqrt(sum(x*x for x in emb)) or 1.0
    emb = [x/mag for x in emb]
    json.dump({"embedding": emb, "dimensions": dims}, sys.stdout)
    sys.stdout.write("\n")
    sys.stdout.flush()
`
	if _, err := f.WriteString(script); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := os.Chmod(f.Name(), 0755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	return f.Name()
}

func TestMPSEmbedderWithStubScript(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	scriptPath := writeTempScript(t, 384)
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Fatalf("NewMPSEmbedder: %v", err)
	}
	defer e.Close()
	v, err := e.Embed(context.Background(), "hello stub")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 384 {
		t.Errorf("dim = %d; want 384", len(v))
	}
}

func TestMPSEmbedderWithStubScriptDimMismatch(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}

	scriptPath := writeTempScript(t, 10)
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Fatalf("NewMPSEmbedder: %v", err)
	}
	defer e.Close()
	_, err = e.Embed(context.Background(), "mismatch")
	if err == nil {
		t.Error("expected dim mismatch error")
	}
}

func TestMPSEmbedderEmbedAfterClose(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	scriptPath := writeTempScript(t, 384)
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		Dimensions: 384,
	})
	if err != nil {
		t.Fatalf("NewMPSEmbedder: %v", err)
	}
	e.Close()
	_, err = e.Embed(context.Background(), "after close")
	if err == nil {
		t.Error("expected error embedding after Close")
	}
}
