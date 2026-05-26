package embed

import (
	"runtime"
	"testing"
)

func TestNewEmbedderModeMockReturnsMockEmbedder(t *testing.T) {
	cfg := Config{Backend: "mock", Dimensions: 384}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	if e.Dimensions() != 384 {
		t.Errorf("dim = %d; want 384", e.Dimensions())
	}
	if _, ok := e.(*MockEmbedder); !ok {
		t.Errorf("backend=mock did not return MockEmbedder; got %T", e)
	}
}

func TestNewEmbedderModeCPUReturnsCPUEmbedder(t *testing.T) {
	cfg := Config{Backend: "cpu", Dimensions: 384, Model: "gte-small-placeholder"}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	if _, ok := e.(*CPUEmbedder); !ok {
		t.Errorf("backend=cpu did not return CPUEmbedder; got %T", e)
	}
}

func TestNewEmbedderModeAutoOnLinuxReturnsCPUEmbedder(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("auto on darwin attempts MPS first; test specific to non-darwin")
	}
	cfg := Config{Backend: "auto", Dimensions: 384}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	if _, ok := e.(*CPUEmbedder); !ok {
		t.Errorf("backend=auto on %s did not fall back to CPU; got %T", runtime.GOOS, e)
	}
}

func TestNewEmbedderRejectsUnknownBackend(t *testing.T) {
	cfg := Config{Backend: "xyzzy", Dimensions: 384}
	_, err := NewEmbedder(cfg)
	if err == nil {
		t.Error("unknown backend should fail")
	}
}

func TestNewEmbedderDefaultDimensions(t *testing.T) {

	cfg := Config{Backend: "mock", Dimensions: 0}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	if e.Dimensions() != 384 {
		t.Errorf("default dim = %d; want 384", e.Dimensions())
	}
}

func TestNewEmbedderEmptyBackendDefaults(t *testing.T) {

	cfg := Config{Backend: "", Dimensions: 384}
	_, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder with empty backend: %v", err)
	}
}

func TestNewEmbedderModeMPSExplicit(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("explicit mps backend only tested on darwin")
	}

	cfg := Config{Backend: "mps", Dimensions: 384, ScriptPath: "/does/not/exist.py"}
	_, err := NewEmbedder(cfg)
	if err == nil {
		t.Error("explicit backend=mps with missing script should return error")
	}
}

func TestNewEmbedderModeAutoOnDarwinFallsBackToCPU(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	cfg := Config{Backend: "auto", Dimensions: 384, ScriptPath: "/does/not/exist.py"}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder auto fallback: %v", err)
	}

	if _, ok := e.(*CPUEmbedder); !ok {
		t.Errorf("auto fallback on darwin did not return CPUEmbedder; got %T", e)
	}
}

func TestNewEmbedderModeAutoFakePythonFallsBackToCPU(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	cfg := Config{Backend: "auto", Dimensions: 384, PythonPath: "/does/not/exist/python3"}
	e, err := NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewEmbedder auto with bad python: %v", err)
	}

	if _, ok := e.(*CPUEmbedder); !ok {
		t.Errorf("auto fallback (bad python) did not return CPUEmbedder; got %T", e)
	}
}
