// SPDX-License-Identifier: MIT
package embed

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type Config struct {
	Backend string

	Model string

	Dimensions int

	PythonPath string

	ScriptPath string
}

func NewEmbedder(cfg Config) (Embedder, error) {
	if cfg.Dimensions <= 0 {
		cfg.Dimensions = 384
	}
	switch cfg.Backend {
	case "", "auto":
		if e, ok := tryMPS(cfg); ok {
			return e, nil
		}
		return NewCPUEmbedder(CPUOptions{
			Dimensions: cfg.Dimensions,
			Model:      cfg.Model,
		})
	case "mps":
		e, ok := tryMPS(cfg)
		if !ok {
			return nil, fmt.Errorf("%w: explicit backend=mps but unavailable (try backend=auto)", ErrMPSUnavailable)
		}
		return e, nil
	case "cpu":
		return NewCPUEmbedder(CPUOptions{
			Dimensions: cfg.Dimensions,
			Model:      cfg.Model,
		})
	case "mock":
		return NewMockEmbedder(cfg.Dimensions), nil
	default:
		return nil, fmt.Errorf("embed: unknown backend %q (allowed: auto|mps|cpu|mock)", cfg.Backend)
	}
}

func tryMPS(cfg Config) (Embedder, bool) {
	if runtime.GOOS != "darwin" {
		return nil, false
	}
	pythonPath := cfg.PythonPath
	if pythonPath == "" {
		pythonPath = "python3"
	}
	if _, err := exec.LookPath(pythonPath); err != nil {
		return nil, false
	}
	scriptPath := cfg.ScriptPath
	if scriptPath == "" {

		exe, err := os.Executable()
		if err == nil {
			scriptPath = filepath.Join(filepath.Dir(exe), "..", "..", "internal", "knowledge", "embed", "scripts", "zen_embed.py")
		}
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, false
	}
	e, err := NewMPSEmbedder(MPSOptions{
		PythonPath: pythonPath,
		ScriptPath: scriptPath,
		Dimensions: cfg.Dimensions,
		Model:      cfg.Model,
	})
	if err != nil {

		return nil, false
	}
	return e, true
}
