package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoadEcosystemPrefixConfigDefaults verifies that a missing
// ecosystem-prefix.toml returns the C2 baseline defaults (qwen2.5:7b /
// ollama / parallelism=4) without error. inv-zen surface: defaults must
// produce a runnable pipeline so first-boot operators do not need to author
// TOML before ingest works.
func TestLoadEcosystemPrefixConfigDefaults(t *testing.T) {
	cfg, err := LoadEcosystemPrefixConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadEcosystemPrefixConfig: %v", err)
	}
	if cfg.Primary.Model != "qwen2.5:7b" {
		t.Errorf("default model = %q, want qwen2.5:7b", cfg.Primary.Model)
	}
	if cfg.Primary.Backend != "ollama" {
		t.Errorf("default backend = %q, want ollama", cfg.Primary.Backend)
	}
	if cfg.Primary.Parallelism != 4 {
		t.Errorf("default parallelism = %d, want 4", cfg.Primary.Parallelism)
	}
	if cfg.Primary.BatchSize != 16 {
		t.Errorf("default batch_size = %d, want 16", cfg.Primary.BatchSize)
	}
	if cfg.Retry.MaxAttempts != 3 {
		t.Errorf("default retry.max_attempts = %d, want 3", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.BackoffMs != 500 {
		t.Errorf("default retry.backoff_ms = %d, want 500", cfg.Retry.BackoffMs)
	}
}

func TestValidateEcosystemPrefixConfigInvalidBackend(t *testing.T) {
	cfg := defaultEcosystemPrefixConfig()
	cfg.Primary.Backend = "unknown-backend"
	err := ValidateEcosystemPrefixConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unknown-backend") {
		t.Errorf("error %q should mention the offending backend", err)
	}
}

func TestValidateEcosystemPrefixConfigAllValidBackends(t *testing.T) {
	for _, backend := range []string{
		"ollama", "claude-via-dispatcher",
		"deepseek-via-siliconflow", "openrouter",
	} {
		cfg := defaultEcosystemPrefixConfig()
		cfg.Primary.Backend = backend
		if err := ValidateEcosystemPrefixConfig(cfg); err != nil {
			t.Errorf("backend %q rejected: %v", backend, err)
		}
	}
}

func TestValidateEcosystemPrefixConfigEmptyBackend(t *testing.T) {
	cfg := &EcosystemPrefixConfig{}
	cfg.Primary.Backend = ""
	if err := ValidateEcosystemPrefixConfig(cfg); err != nil {
		t.Errorf("empty backend should be allowed (soft-gate): %v", err)
	}
}

func TestValidateEcosystemPrefixConfigNegativeParallelism(t *testing.T) {
	cfg := defaultEcosystemPrefixConfig()
	cfg.Primary.Parallelism = -1
	err := ValidateEcosystemPrefixConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative parallelism")
	}
}

func TestLoadEcosystemPrefixConfigFromTOML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-prefix.toml"), `
[primary]
model = "qwen2.5-coder:32b"
backend = "ollama"
parallelism = 1
batch_size = 8

[retry]
max_attempts = 5
backoff_ms = 1000
`)
	cfg, err := LoadEcosystemPrefixConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Primary.Model != "qwen2.5-coder:32b" {
		t.Errorf("model = %q, want qwen2.5-coder:32b", cfg.Primary.Model)
	}
	if cfg.Primary.Parallelism != 1 {
		t.Errorf("parallelism = %d, want 1", cfg.Primary.Parallelism)
	}
	if cfg.Primary.BatchSize != 8 {
		t.Errorf("batch_size = %d, want 8", cfg.Primary.BatchSize)
	}
	if cfg.Retry.MaxAttempts != 5 {
		t.Errorf("retry.max_attempts = %d, want 5", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.BackoffMs != 1000 {
		t.Errorf("retry.backoff_ms = %d, want 1000", cfg.Retry.BackoffMs)
	}
}

func TestLoadEcosystemPrefixConfigMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-prefix.toml"), `
[primary
model = "broken-toml"
`)
	_, err := LoadEcosystemPrefixConfig(dir)
	if err == nil {
		t.Fatal("expected parse error for malformed TOML")
	}
	if !strings.Contains(err.Error(), "ecosystem-prefix.toml") {
		t.Errorf("error %q should mention the offending file", err)
	}
}

func TestLoadEcosystemEmbedderConfigDefaults(t *testing.T) {
	cfg, err := LoadEcosystemEmbedderConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Primary.Model != "jina-code-embeddings-1.5b" {
		t.Errorf("default model = %q, want jina-code-embeddings-1.5b", cfg.Primary.Model)
	}
	if cfg.Primary.Backend != "mps" {
		t.Errorf("default backend = %q, want mps", cfg.Primary.Backend)
	}
	if cfg.Primary.BatchSize != 32 {
		t.Errorf("default batch_size = %d, want 32", cfg.Primary.BatchSize)
	}
	if cfg.Performance.EncodeTimeoutMs != 100 {
		t.Errorf("default encode_timeout_ms = %d, want 100", cfg.Performance.EncodeTimeoutMs)
	}
}

func TestValidateEcosystemEmbedderConfigInvalidBackend(t *testing.T) {
	cfg := defaultEcosystemEmbedderConfig()
	cfg.Primary.Backend = "cuda"
	err := ValidateEcosystemEmbedderConfig(cfg)
	if err == nil {
		t.Fatal("expected error for cuda (not in closed set)")
	}
}

func TestValidateEcosystemEmbedderConfigFallbackBackend(t *testing.T) {
	cfg := defaultEcosystemEmbedderConfig()
	cfg.Fallback.Backend = "openai-api"
	err := ValidateEcosystemEmbedderConfig(cfg)
	if err == nil {
		t.Fatal("expected error for openai-api (not in closed set)")
	}
}

func TestLoadEcosystemEmbedderConfigFromTOML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-embedder.toml"), `
[primary]
model = "jina-code-embeddings-1.5b"
backend = "mps"
batch_size = 64

[fallback]
model = "voyage-code-3"
backend = "voyage-api"
api_key_ref = "zen-swarm/voyage-code-3"
batch_size = 8

[performance]
encode_timeout_ms = 200
`)
	cfg, err := LoadEcosystemEmbedderConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Primary.BatchSize != 64 {
		t.Errorf("primary.batch_size = %d, want 64", cfg.Primary.BatchSize)
	}
	if cfg.Fallback.Backend != "voyage-api" {
		t.Errorf("fallback.backend = %q, want voyage-api", cfg.Fallback.Backend)
	}
	if cfg.Fallback.APIKeyRef != "zen-swarm/voyage-code-3" {
		t.Errorf("fallback.api_key_ref = %q, want zen-swarm/voyage-code-3", cfg.Fallback.APIKeyRef)
	}
	if cfg.Performance.EncodeTimeoutMs != 200 {
		t.Errorf("encode_timeout_ms = %d, want 200", cfg.Performance.EncodeTimeoutMs)
	}
}

func TestLoadEcosystemRerankerConfigDefaults(t *testing.T) {
	cfg, err := LoadEcosystemRerankerConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Primary.Model != "bge-reranker-v2-m3" {
		t.Errorf("default model = %q, want bge-reranker-v2-m3", cfg.Primary.Model)
	}
	if cfg.Primary.Backend != "mps" {
		t.Errorf("default backend = %q, want mps", cfg.Primary.Backend)
	}
	if cfg.Primary.MaxLatencyMs != 300 {
		t.Errorf("default max_latency_ms = %d, want 300 (inv-zen-198)", cfg.Primary.MaxLatencyMs)
	}

	if got, want := cfg.Abstention.Lambda["go"], 0.3; got != want {
		t.Errorf("default lambda[go] = %v, want %v", got, want)
	}
	if got, want := cfg.Abstention.Lambda["python"], 0.5; got != want {
		t.Errorf("default lambda[python] = %v, want %v", got, want)
	}
	if got, want := cfg.Abstention.Lambda["typescript"], 0.8; got != want {
		t.Errorf("default lambda[typescript] = %v, want %v", got, want)
	}
	if got, want := cfg.Abstention.Lambda["rust"], 0.4; got != want {
		t.Errorf("default lambda[rust] = %v, want %v", got, want)
	}
}

func TestLoadEcosystemRerankerConfigLambdaMap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-reranker.toml"), `
[primary]
model = "bge-reranker-v2-m3"
backend = "mps"
max_latency_ms = 250

[abstention.lambda]
go = 0.2
python = 0.6
typescript = 1.0
rust = 0.2
zig = 0.5
`)
	cfg, err := LoadEcosystemRerankerConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Primary.MaxLatencyMs != 250 {
		t.Errorf("max_latency_ms = %d, want 250", cfg.Primary.MaxLatencyMs)
	}
	if got, want := cfg.Abstention.Lambda["go"], 0.2; got != want {
		t.Errorf("lambda[go] = %v, want %v", got, want)
	}
	if got, want := cfg.Abstention.Lambda["typescript"], 1.0; got != want {
		t.Errorf("lambda[typescript] = %v, want %v", got, want)
	}

	if got, want := cfg.Abstention.Lambda["zig"], 0.5; got != want {
		t.Errorf("lambda[zig] = %v, want %v (new ecosystem must be accepted)", got, want)
	}
}

func TestValidateEcosystemRerankerConfigInvalidBackend(t *testing.T) {
	cfg := defaultEcosystemRerankerConfig()
	cfg.Primary.Backend = "tritonserver"
	err := ValidateEcosystemRerankerConfig(cfg)
	if err == nil {
		t.Fatal("expected error for tritonserver (not in closed set)")
	}
}

func TestValidateEcosystemRerankerConfigNegativeLambda(t *testing.T) {
	cfg := defaultEcosystemRerankerConfig()
	cfg.Abstention.Lambda["go"] = -0.1
	err := ValidateEcosystemRerankerConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative lambda (inv-zen-196)")
	}
}

func TestLoadEcosystemRouterConfigDefaults(t *testing.T) {
	cfg, err := LoadEcosystemRouterConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Classifier.FeatureDim != 128 {
		t.Errorf("default feature_dim = %d, want 128", cfg.Classifier.FeatureDim)
	}
	if cfg.Classifier.SoftmaxTopK != 2 {
		t.Errorf("default softmax_top_k = %d, want 2 (RRF fusion)", cfg.Classifier.SoftmaxTopK)
	}
	if len(cfg.Heuristics.GoTokens) == 0 {
		t.Error("default heuristics.go_tokens empty; should seed go.mod / := / ...")
	}
	if cfg.Retrain.MinSamplesDelta != 1000 {
		t.Errorf("default min_samples_delta = %d, want 1000", cfg.Retrain.MinSamplesDelta)
	}
}

func TestLoadEcosystemRouterConfigHeuristics(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-router.toml"), `
[classifier]
model_path = "/tmp/router.model"
feature_dim = 256
softmax_top_k = 3

[heuristics]
go_tokens = ["go.mod", "goroutine"]
python_tokens = ["import", "def ", "pyproject.toml", "uv pip"]
typescript_tokens = ["interface ", "package.json"]
rust_tokens = ["Cargo.toml", "fn "]

[retrain]
min_samples_delta = 500
schedule_cron = "0 2 * * 0"
`)
	cfg, err := LoadEcosystemRouterConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Classifier.ModelPath != "/tmp/router.model" {
		t.Errorf("model_path = %q, want /tmp/router.model", cfg.Classifier.ModelPath)
	}
	if cfg.Classifier.FeatureDim != 256 {
		t.Errorf("feature_dim = %d, want 256", cfg.Classifier.FeatureDim)
	}
	if cfg.Classifier.SoftmaxTopK != 3 {
		t.Errorf("softmax_top_k = %d, want 3", cfg.Classifier.SoftmaxTopK)
	}
	if got, want := len(cfg.Heuristics.PythonTokens), 4; got != want {
		t.Errorf("python_tokens len = %d, want %d", got, want)
	}
	if cfg.Heuristics.PythonTokens[3] != "uv pip" {
		t.Errorf("python_tokens[3] = %q, want \"uv pip\"", cfg.Heuristics.PythonTokens[3])
	}
	if cfg.Retrain.ScheduleCron != "0 2 * * 0" {
		t.Errorf("schedule_cron = %q, want 0 2 * * 0", cfg.Retrain.ScheduleCron)
	}
}

func TestValidateEcosystemRouterConfigNegativeFeatureDim(t *testing.T) {
	cfg := defaultEcosystemRouterConfig()
	cfg.Classifier.FeatureDim = -1
	err := ValidateEcosystemRouterConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative feature_dim")
	}
}

func TestValidateEcosystemRouterConfigNegativeSoftmaxTopK(t *testing.T) {
	cfg := defaultEcosystemRouterConfig()
	cfg.Classifier.SoftmaxTopK = -1
	err := ValidateEcosystemRouterConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative softmax_top_k")
	}
}

func TestLoadEcosystemVersionDetectConfigDefaults(t *testing.T) {
	cfg, err := LoadEcosystemVersionDetectConfig(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Layer4LLM.Enabled {
		t.Error("default layer4_llm.enabled = false, want true")
	}
	if cfg.Layer4LLM.Model != "claude-haiku-4-5" {
		t.Errorf("default layer4.model = %q, want claude-haiku-4-5", cfg.Layer4LLM.Model)
	}
	if cfg.Layer4LLM.TimeoutMs != 500 {
		t.Errorf("default layer4.timeout_ms = %d, want 500", cfg.Layer4LLM.TimeoutMs)
	}
	if cfg.Defaults.Go != "1.22" {
		t.Errorf("default go = %q, want 1.22", cfg.Defaults.Go)
	}
	if cfg.Defaults.Python != "3.12" {
		t.Errorf("default python = %q, want 3.12", cfg.Defaults.Python)
	}
	if cfg.Defaults.TypeScript != "5.4" {
		t.Errorf("default typescript = %q, want 5.4", cfg.Defaults.TypeScript)
	}
	if cfg.Defaults.Rust != "1.78" {
		t.Errorf("default rust = %q, want 1.78", cfg.Defaults.Rust)
	}
	if len(cfg.FilePatterns.GoFiles) == 0 {
		t.Error("default file_patterns.go_files empty; should seed go.mod, go.work")
	}
}

func TestLoadEcosystemVersionDetectConfigDisabledLayer4(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ecosystem-version-detect.toml"), `
[layer4_llm]
enabled = false
model = "claude-haiku-4-5"
timeout_ms = 500

[defaults]
go = "1.23"
python = "3.13"
typescript = "5.5"
rust = "1.80"

[file_patterns]
go_files = ["go.mod"]
python_files = ["pyproject.toml"]
typescript_files = ["package.json"]
rust_files = ["Cargo.toml"]
`)
	cfg, err := LoadEcosystemVersionDetectConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Layer4LLM.Enabled {
		t.Error("layer4 enabled = true, want false")
	}
	if cfg.Defaults.Go != "1.23" {
		t.Errorf("defaults.go = %q, want 1.23", cfg.Defaults.Go)
	}
	if cfg.Defaults.Rust != "1.80" {
		t.Errorf("defaults.rust = %q, want 1.80", cfg.Defaults.Rust)
	}
	if got, want := len(cfg.FilePatterns.GoFiles), 1; got != want {
		t.Errorf("file_patterns.go_files len = %d, want %d", got, want)
	}
}

func TestValidateEcosystemVersionDetectConfigNegativeTimeout(t *testing.T) {
	cfg := defaultEcosystemVersionDetectConfig()
	cfg.Layer4LLM.TimeoutMs = -1
	err := ValidateEcosystemVersionDetectConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative timeout_ms")
	}
}

func TestLoadTOMLPropagatesNonNotExistError(t *testing.T) {
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.toml")
	writeFile(t, bad, "this is not [valid")
	var dst EcosystemPrefixConfig
	err := loadTOML(bad, &dst)
	if err == nil {
		t.Fatal("expected parse error from loadTOML, got nil")
	}

	missing := filepath.Join(dir, "absent.toml")
	err = loadTOML(missing, &dst)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("loadTOML on missing file returned %v, want os.IsNotExist=true", err)
	}
}

func makeMalformedTOMLDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, name), "[[broken\nthis is not = valid")
	return dir
}

func TestLoadEcosystemEmbedderConfigMalformed(t *testing.T) {
	dir := makeMalformedTOMLDir(t, "ecosystem-embedder.toml")
	_, err := LoadEcosystemEmbedderConfig(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "ecosystem-embedder.toml") {
		t.Errorf("error %q should mention the offending file", err)
	}
}

func TestLoadEcosystemRerankerConfigMalformed(t *testing.T) {
	dir := makeMalformedTOMLDir(t, "ecosystem-reranker.toml")
	_, err := LoadEcosystemRerankerConfig(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "ecosystem-reranker.toml") {
		t.Errorf("error %q should mention the offending file", err)
	}
}

func TestLoadEcosystemRouterConfigMalformed(t *testing.T) {
	dir := makeMalformedTOMLDir(t, "ecosystem-router.toml")
	_, err := LoadEcosystemRouterConfig(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "ecosystem-router.toml") {
		t.Errorf("error %q should mention the offending file", err)
	}
}

func TestLoadEcosystemVersionDetectConfigMalformed(t *testing.T) {
	dir := makeMalformedTOMLDir(t, "ecosystem-version-detect.toml")
	_, err := LoadEcosystemVersionDetectConfig(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "ecosystem-version-detect.toml") {
		t.Errorf("error %q should mention the offending file", err)
	}
}

func TestValidateEcosystemPrefixConfigNegativeCodeChunksParallelism(t *testing.T) {
	cfg := defaultEcosystemPrefixConfig()
	cfg.CodeChunks.Parallelism = -1
	err := ValidateEcosystemPrefixConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative code_chunks.parallelism")
	}
	if !strings.Contains(err.Error(), "code_chunks") {
		t.Errorf("error %q should mention code_chunks", err)
	}
}

func TestValidateEcosystemEmbedderConfigNegativeEncodeTimeout(t *testing.T) {
	cfg := defaultEcosystemEmbedderConfig()
	cfg.Performance.EncodeTimeoutMs = -1
	err := ValidateEcosystemEmbedderConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative encode_timeout_ms")
	}
}

func TestValidateEcosystemEmbedderConfigPassesOnDefaults(t *testing.T) {
	cfg := defaultEcosystemEmbedderConfig()
	if err := ValidateEcosystemEmbedderConfig(cfg); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestValidateEcosystemRerankerConfigNegativeMaxLatency(t *testing.T) {
	cfg := defaultEcosystemRerankerConfig()
	cfg.Primary.MaxLatencyMs = -1
	err := ValidateEcosystemRerankerConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative max_latency_ms")
	}
}

func TestValidateEcosystemRerankerConfigFallbackBackend(t *testing.T) {
	cfg := defaultEcosystemRerankerConfig()
	cfg.Fallback.Backend = "voyage-rerank"
	err := ValidateEcosystemRerankerConfig(cfg)
	if err == nil {
		t.Fatal("expected error for voyage-rerank (not in closed set)")
	}
}

func TestValidateEcosystemRerankerConfigPassesOnDefaults(t *testing.T) {
	cfg := defaultEcosystemRerankerConfig()
	if err := ValidateEcosystemRerankerConfig(cfg); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestValidateEcosystemRouterConfigNegativeMinSamplesDelta(t *testing.T) {
	cfg := defaultEcosystemRouterConfig()
	cfg.Retrain.MinSamplesDelta = -1
	err := ValidateEcosystemRouterConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative min_samples_delta")
	}
}

func TestValidateEcosystemRouterConfigPassesOnDefaults(t *testing.T) {
	cfg := defaultEcosystemRouterConfig()
	if err := ValidateEcosystemRouterConfig(cfg); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestValidateEcosystemVersionDetectConfigPassesOnDefaults(t *testing.T) {
	cfg := defaultEcosystemVersionDetectConfig()
	if err := ValidateEcosystemVersionDetectConfig(cfg); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}
