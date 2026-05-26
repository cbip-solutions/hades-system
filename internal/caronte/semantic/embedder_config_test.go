package semantic

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultEmbedderConfigMode(t *testing.T) {
	c := DefaultEmbedderConfig()
	if c.Mode != "auto" {
		t.Errorf("Mode = %q; want auto", c.Mode)
	}
	if c.JinaModelPath == "" {
		t.Fatal("JinaModelPath empty by default")
	}
	if !strings.HasSuffix(c.JinaModelPath, filepath.Join("jina-code", "model.onnx")) {
		t.Errorf("JinaModelPath = %q; want suffix jina-code/model.onnx", c.JinaModelPath)
	}
	if c.EcosystemMCPEndpoint != "" {
		t.Errorf("EcosystemMCPEndpoint = %q; want empty default", c.EcosystemMCPEndpoint)
	}
}

func TestDefaultJinaModelPathXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/xdg")
	c := DefaultEmbedderConfig()
	want := "/custom/xdg/zen-swarm/models/jina-code/model.onnx"
	if c.JinaModelPath != want {
		t.Errorf("JinaModelPath = %q; want %q", c.JinaModelPath, want)
	}
}

func TestDefaultJinaModelPathHomeFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/fake/home")
	c := DefaultEmbedderConfig()
	want := "/fake/home/.local/share/zen-swarm/models/jina-code/model.onnx"
	if c.JinaModelPath != want {
		t.Errorf("JinaModelPath = %q; want %q", c.JinaModelPath, want)
	}
}

func TestEmbedderConfigParseTOML(t *testing.T) {
	src := `
[caronte.embedder]
mode = "jina-local"
jina_model_path = "/opt/models/jina/model.onnx"
ecosystem_mcp_endpoint = "unix:///tmp/embed.sock"
`
	var wrapper struct {
		Caronte struct {
			Embedder EmbedderConfig `toml:"embedder"`
		} `toml:"caronte"`
	}
	if _, err := toml.Decode(src, &wrapper); err != nil {
		t.Fatalf("toml.Decode: %v", err)
	}
	got := wrapper.Caronte.Embedder
	if got.Mode != "jina-local" {
		t.Errorf("Mode = %q; want jina-local", got.Mode)
	}
	if got.JinaModelPath != "/opt/models/jina/model.onnx" {
		t.Errorf("JinaModelPath = %q", got.JinaModelPath)
	}
	if got.EcosystemMCPEndpoint != "unix:///tmp/embed.sock" {
		t.Errorf("EcosystemMCPEndpoint = %q", got.EcosystemMCPEndpoint)
	}
}

func TestEmbedderConfigInvalidMode(t *testing.T) {
	c := EmbedderConfig{Mode: "quantum"}
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(quantum) returned nil error")
	}
	if !strings.Contains(err.Error(), "quantum") {
		t.Errorf("error missing offending value: %v", err)
	}
}

func TestEmbedderConfigValidateNormalisesBlankFields(t *testing.T) {
	c := EmbedderConfig{}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate(empty): %v", err)
	}
	if c.Mode != "auto" {
		t.Errorf("Mode after Validate = %q; want auto", c.Mode)
	}
	if c.JinaModelPath == "" {
		t.Error("JinaModelPath empty after Validate")
	}
}

func TestEmbedderConfigValidateIdempotent(t *testing.T) {
	c := DefaultEmbedderConfig()
	c.JinaModelPath = "/opt/x.onnx"
	c.Mode = "auto"
	c.EcosystemMCPEndpoint = "unix:///custom"
	before := c
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate #1: %v", err)
	}
	if c != before {
		t.Errorf("Validate #1 mutated populated config: %+v -> %+v", before, c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate #2: %v", err)
	}
	if c != before {
		t.Errorf("Validate #2 mutated populated config: %+v -> %+v", before, c)
	}
}

func TestEmbedderConfigEncodeRoundTrip(t *testing.T) {
	cfg := EmbedderConfig{
		Mode:                 "ecosystem-mcp",
		JinaModelPath:        "/opt/models/jina-code/model.onnx",
		EcosystemMCPEndpoint: "unix:///tmp/ecosystem-embed.sock",
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got EmbedderConfig
	if _, err := toml.Decode(buf.String(), &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != cfg {
		t.Errorf("round-trip mismatch:\n  want %+v\n  got  %+v", cfg, got)
	}
}

func TestDefaultJinaModelPathHomeUnavailable(t *testing.T) {

	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	p := defaultJinaModelPath()
	if p == "" {
		t.Error("defaultJinaModelPath returned empty string")
	}

	_ = p

	if !strings.HasSuffix(p, ".onnx") {
		t.Errorf("defaultJinaModelPath = %q; want a *.onnx suffix", p)
	}

	_ = os.Getenv
}
