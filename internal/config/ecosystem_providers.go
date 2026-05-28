// SPDX-License-Identifier: MIT
// internal/config/ecosystem_providers.go
//
// TOML schema types + Load/Validate/default helpers for the 5 ecosystem
// provider config files HADES design ingester + retrieval pipeline consume at
// daemon startup.
//
// Files live at ~/.config/hades-system/providers/ (operator home) — NOT in
// the repo. Missing files use the embedded Go defaults; well-formed files
// override per-field; malformed files fail Load* with a typed error so
// daemon-startup surfaces the misconfig before reaching the dispatch
// path.
//
// The five surfaces:
//
// - ecosystem-prefix.toml — Contextual Retrieval prefix LLM
// (qwen2.5:7b / ollama / parallelism=4 baseline; spec §2.3 C2)
// - ecosystem-embedder.toml — Embedding model
// (jina-code-embeddings-1.5b / MPS primary; voyage-code-3 fallback)
// - ecosystem-reranker.toml — Reranker + per-ecosystem λ map
// (BGE-reranker-v2-m3; invariant ≤300ms; invariant λ tunable)
// - ecosystem-router.toml — Local classifier + heuristic
// pre-filter (no LLM; single-egress doctrine preserved)
// - ecosystem-version-detect.toml — 5-layer cascade
// (FileParser → regex → optional Haiku → default)
//
// Validation gates: each Validate* enforces closed-set Backend membership
// (catches typos like "ollam") + non-negative Parallelism / TimeoutMs /
// FeatureDim (catches semantically-broken-but-well-formed config).
//
// The install script that materializes the TOML files on first run lives
// downstream — F-8 ships only
// the schema + Load/Validate/default surface.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type EcosystemPrefixConfig struct {
	Primary struct {
		Model       string `toml:"model"`
		Backend     string `toml:"backend"`
		Parallelism int    `toml:"parallelism"`
		BatchSize   int    `toml:"batch_size"`
	} `toml:"primary"`
	CodeChunks struct {
		Model       string `toml:"model"`
		Parallelism int    `toml:"parallelism"`
	} `toml:"code_chunks"`
	Retry struct {
		MaxAttempts int `toml:"max_attempts"`
		BackoffMs   int `toml:"backoff_ms"`
	} `toml:"retry"`
}

type EcosystemEmbedderConfig struct {
	Primary struct {
		Model     string `toml:"model"`
		Backend   string `toml:"backend"`
		BatchSize int    `toml:"batch_size"`
	} `toml:"primary"`
	Fallback struct {
		Model     string `toml:"model"`
		Backend   string `toml:"backend"`
		APIKeyRef string `toml:"api_key_ref"`
		BatchSize int    `toml:"batch_size"`
	} `toml:"fallback"`
	Performance struct {
		EncodeTimeoutMs int `toml:"encode_timeout_ms"`
	} `toml:"performance"`
}

type EcosystemRerankerConfig struct {
	Primary struct {
		Model        string `toml:"model"`
		Backend      string `toml:"backend"`
		MaxLatencyMs int    `toml:"max_latency_ms"`
	} `toml:"primary"`
	Fallback struct {
		Model     string `toml:"model"`
		Backend   string `toml:"backend"`
		APIKeyRef string `toml:"api_key_ref"`
	} `toml:"fallback"`
	Abstention struct {
		Lambda map[string]float64 `toml:"lambda"`
	} `toml:"abstention"`
}

type EcosystemRouterConfig struct {
	Classifier struct {
		ModelPath   string `toml:"model_path"`
		FeatureDim  int    `toml:"feature_dim"`
		SoftmaxTopK int    `toml:"softmax_top_k"`
	} `toml:"classifier"`
	Heuristics struct {
		GoTokens         []string `toml:"go_tokens"`
		PythonTokens     []string `toml:"python_tokens"`
		TypeScriptTokens []string `toml:"typescript_tokens"`
		RustTokens       []string `toml:"rust_tokens"`
	} `toml:"heuristics"`
	Retrain struct {
		MinSamplesDelta int    `toml:"min_samples_delta"`
		ScheduleCron    string `toml:"schedule_cron"`
	} `toml:"retrain"`
}

type EcosystemVersionDetectConfig struct {
	Layer4LLM struct {
		Enabled   bool   `toml:"enabled"`
		Model     string `toml:"model"`
		TimeoutMs int    `toml:"timeout_ms"`
	} `toml:"layer4_llm"`
	Defaults struct {
		Go         string `toml:"go"`
		Python     string `toml:"python"`
		TypeScript string `toml:"typescript"`
		Rust       string `toml:"rust"`
	} `toml:"defaults"`
	FilePatterns struct {
		GoFiles         []string `toml:"go_files"`
		PythonFiles     []string `toml:"python_files"`
		TypeScriptFiles []string `toml:"typescript_files"`
		RustFiles       []string `toml:"rust_files"`
	} `toml:"file_patterns"`
}

var validPrefixBackends = map[string]bool{
	"ollama":                   true,
	"claude-via-dispatcher":    true,
	"deepseek-via-siliconflow": true,
	"openrouter":               true,
}

var validEmbedderBackends = map[string]bool{
	"mps":        true,
	"cpu":        true,
	"voyage-api": true,
}

var validRerankerBackends = map[string]bool{
	"mps":        true,
	"cpu":        true,
	"cohere-api": true,
}

func LoadEcosystemPrefixConfig(dir string) (*EcosystemPrefixConfig, error) {
	cfg := defaultEcosystemPrefixConfig()
	path := filepath.Join(dir, "ecosystem-prefix.toml")
	if err := loadTOML(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ecosystem-prefix.toml: %w", err)
	}
	return cfg, nil
}

func ValidateEcosystemPrefixConfig(cfg *EcosystemPrefixConfig) error {
	if cfg.Primary.Backend != "" && !validPrefixBackends[cfg.Primary.Backend] {
		return fmt.Errorf("ecosystem-prefix: primary.backend %q not in allowed set", cfg.Primary.Backend)
	}
	if cfg.Primary.Parallelism < 0 {
		return errors.New("ecosystem-prefix: primary.parallelism must be non-negative")
	}
	if cfg.CodeChunks.Parallelism < 0 {
		return errors.New("ecosystem-prefix: code_chunks.parallelism must be non-negative")
	}
	return nil
}

func defaultEcosystemPrefixConfig() *EcosystemPrefixConfig {
	cfg := &EcosystemPrefixConfig{}
	cfg.Primary.Model = "qwen2.5:7b"
	cfg.Primary.Backend = "ollama"
	cfg.Primary.Parallelism = 4
	cfg.Primary.BatchSize = 16
	cfg.Retry.MaxAttempts = 3
	cfg.Retry.BackoffMs = 500
	return cfg
}

func LoadEcosystemEmbedderConfig(dir string) (*EcosystemEmbedderConfig, error) {
	cfg := defaultEcosystemEmbedderConfig()
	path := filepath.Join(dir, "ecosystem-embedder.toml")
	if err := loadTOML(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ecosystem-embedder.toml: %w", err)
	}
	return cfg, nil
}

func ValidateEcosystemEmbedderConfig(cfg *EcosystemEmbedderConfig) error {
	if cfg.Primary.Backend != "" && !validEmbedderBackends[cfg.Primary.Backend] {
		return fmt.Errorf("ecosystem-embedder: primary.backend %q not in allowed set", cfg.Primary.Backend)
	}
	if cfg.Fallback.Backend != "" && !validEmbedderBackends[cfg.Fallback.Backend] {
		return fmt.Errorf("ecosystem-embedder: fallback.backend %q not in allowed set", cfg.Fallback.Backend)
	}
	if cfg.Performance.EncodeTimeoutMs < 0 {
		return errors.New("ecosystem-embedder: performance.encode_timeout_ms must be non-negative")
	}
	return nil
}

func defaultEcosystemEmbedderConfig() *EcosystemEmbedderConfig {
	cfg := &EcosystemEmbedderConfig{}
	cfg.Primary.Model = "jina-code-embeddings-1.5b"
	cfg.Primary.Backend = "mps"
	cfg.Primary.BatchSize = 32
	cfg.Performance.EncodeTimeoutMs = 100
	return cfg
}

func LoadEcosystemRerankerConfig(dir string) (*EcosystemRerankerConfig, error) {
	cfg := defaultEcosystemRerankerConfig()
	path := filepath.Join(dir, "ecosystem-reranker.toml")
	if err := loadTOML(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ecosystem-reranker.toml: %w", err)
	}
	return cfg, nil
}

func ValidateEcosystemRerankerConfig(cfg *EcosystemRerankerConfig) error {
	if cfg.Primary.Backend != "" && !validRerankerBackends[cfg.Primary.Backend] {
		return fmt.Errorf("ecosystem-reranker: primary.backend %q not in allowed set", cfg.Primary.Backend)
	}
	if cfg.Fallback.Backend != "" && !validRerankerBackends[cfg.Fallback.Backend] {
		return fmt.Errorf("ecosystem-reranker: fallback.backend %q not in allowed set", cfg.Fallback.Backend)
	}
	if cfg.Primary.MaxLatencyMs < 0 {
		return errors.New("ecosystem-reranker: primary.max_latency_ms must be non-negative")
	}
	for eco, lambda := range cfg.Abstention.Lambda {
		if lambda < 0 {
			return fmt.Errorf("ecosystem-reranker: abstention.lambda[%q] = %v must be non-negative (invariant)", eco, lambda)
		}
	}
	return nil
}

func defaultEcosystemRerankerConfig() *EcosystemRerankerConfig {
	cfg := &EcosystemRerankerConfig{}
	cfg.Primary.Model = "bge-reranker-v2-m3"
	cfg.Primary.Backend = "mps"
	cfg.Primary.MaxLatencyMs = 300
	cfg.Abstention.Lambda = map[string]float64{
		"go":         0.3,
		"python":     0.5,
		"typescript": 0.8,
		"rust":       0.4,
	}
	return cfg
}

func LoadEcosystemRouterConfig(dir string) (*EcosystemRouterConfig, error) {
	cfg := defaultEcosystemRouterConfig()
	path := filepath.Join(dir, "ecosystem-router.toml")
	if err := loadTOML(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ecosystem-router.toml: %w", err)
	}
	return cfg, nil
}

func ValidateEcosystemRouterConfig(cfg *EcosystemRouterConfig) error {
	if cfg.Classifier.FeatureDim < 0 {
		return errors.New("ecosystem-router: classifier.feature_dim must be non-negative")
	}
	if cfg.Classifier.SoftmaxTopK < 0 {
		return errors.New("ecosystem-router: classifier.softmax_top_k must be non-negative")
	}
	if cfg.Retrain.MinSamplesDelta < 0 {
		return errors.New("ecosystem-router: retrain.min_samples_delta must be non-negative")
	}
	return nil
}

func defaultEcosystemRouterConfig() *EcosystemRouterConfig {
	cfg := &EcosystemRouterConfig{}
	cfg.Classifier.FeatureDim = 128
	cfg.Classifier.SoftmaxTopK = 2
	cfg.Heuristics.GoTokens = []string{"go.mod", "go.sum", ":=", "goroutine", "defer", "chan", "func("}
	cfg.Heuristics.PythonTokens = []string{"import", "def ", "pip install", "pyproject.toml", "requirements.txt"}
	cfg.Heuristics.TypeScriptTokens = []string{"interface ", "type ", "npm install", "package.json", "tsconfig"}
	cfg.Heuristics.RustTokens = []string{"Cargo.toml", "fn ", "impl ", "crate::", "use std::"}
	cfg.Retrain.MinSamplesDelta = 1000
	return cfg
}

func LoadEcosystemVersionDetectConfig(dir string) (*EcosystemVersionDetectConfig, error) {
	cfg := defaultEcosystemVersionDetectConfig()
	path := filepath.Join(dir, "ecosystem-version-detect.toml")
	if err := loadTOML(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ecosystem-version-detect.toml: %w", err)
	}
	return cfg, nil
}

func ValidateEcosystemVersionDetectConfig(cfg *EcosystemVersionDetectConfig) error {
	if cfg.Layer4LLM.TimeoutMs < 0 {
		return errors.New("ecosystem-version-detect: layer4_llm.timeout_ms must be non-negative")
	}
	return nil
}

func defaultEcosystemVersionDetectConfig() *EcosystemVersionDetectConfig {
	cfg := &EcosystemVersionDetectConfig{}
	cfg.Layer4LLM.Enabled = true
	cfg.Layer4LLM.Model = "claude-haiku-4-5"
	cfg.Layer4LLM.TimeoutMs = 500
	cfg.Defaults.Go = "1.22"
	cfg.Defaults.Python = "3.12"
	cfg.Defaults.TypeScript = "5.4"
	cfg.Defaults.Rust = "1.78"
	cfg.FilePatterns.GoFiles = []string{"go.mod", "go.work"}
	cfg.FilePatterns.PythonFiles = []string{"pyproject.toml", "setup.cfg", "setup.py", "Pipfile"}
	cfg.FilePatterns.TypeScriptFiles = []string{"package.json", "tsconfig.json"}
	cfg.FilePatterns.RustFiles = []string{"Cargo.toml"}
	return cfg
}

func loadTOML(path string, dst interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return toml.Unmarshal(data, dst)
}
