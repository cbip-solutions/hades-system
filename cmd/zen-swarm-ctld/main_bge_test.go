package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBGEModelAvailable_FilesPresent_ReturnsTrue(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "bge-reranker-v2-m3.onnx")
	tokPath := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(modelPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !bgeModelAvailable(modelPath) {
		t.Fatal("want true; both files present")
	}
}

func TestBGEModelAvailable_ModelMissing_ReturnsFalse(t *testing.T) {
	if bgeModelAvailable("/nonexistent/model.onnx") {
		t.Fatal("want false")
	}
}

func TestBGEModelAvailable_TokenizerMissing_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "bge-reranker-v2-m3.onnx")
	if err := os.WriteFile(modelPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if bgeModelAvailable(modelPath) {
		t.Fatal("want false; tokenizer absent")
	}
}

// TestBGEModelAvailable_ResolvesDefaultPath_NoPanic asserts the helper
// MUST NOT panic when the explicit path is empty and the env is unset.
// On systems where os.UserHomeDir() resolves, the helper computes a
// default path under ~/.local/share/zen-swarm/models/ and stats it; on
// systems where it fails the helper returns false. Either path is OK;
// the contract is "no panic".
func TestBGEModelAvailable_ResolvesDefaultPath_NoPanic(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "")

	_ = bgeModelAvailable("")
}

func TestBGEModelAvailable_EnvVarOverridesExplicitEmpty(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "bge-reranker-v2-m3.onnx")
	tokPath := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(modelPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_BGE_MODEL_PATH", modelPath)
	if !bgeModelAvailable("") {
		t.Fatal("want true; env var points at real files")
	}

	t.Setenv("ZEN_BGE_MODEL_PATH", "/nonexistent/env/model.onnx")
	if bgeModelAvailable("") {
		t.Fatal("want false; env var points at missing file")
	}
}
