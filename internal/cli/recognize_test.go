package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecognizeCmd_JSONFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir, "--json", "--no-audit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v\nstdout:\n%s", err, buf.String())
	}
	if parsed["schemaVersion"] != "1.0" {
		t.Errorf("schemaVersion = %v; want 1.0", parsed["schemaVersion"])
	}
	if parsed["primaryLanguage"] != "Go" {
		t.Errorf("primaryLanguage = %v; want Go", parsed["primaryLanguage"])
	}
}

func TestRecognizeCmd_HumanOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir, "--no-audit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Primary language: Go") {
		t.Errorf("human output missing 'Primary language: Go'; got:\n%s", out)
	}
	if !strings.Contains(out, "Path:") {
		t.Errorf("human output missing 'Path:' header; got:\n%s", out)
	}
}

func TestRecognizeCmd_PathArgDefaults(t *testing.T) {
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--json", "--no-audit"})

	_ = cmd.Execute()
}

func TestRecognizeCmd_NonexistentPath(t *testing.T) {
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"/nonexistent/path/that/does/not/exist", "--no-audit"})
	err := cmd.Execute()
	if err == nil {
		t.Error("Execute returned nil err for nonexistent path; want error")
	}
}

func TestRecognizeCmd_FilePathRejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{file, "--no-audit"})
	err := cmd.Execute()
	if err == nil {
		t.Error("Execute returned nil err for file PATH; want directory-required error")
	}
}

func TestRecognizeCmd_HumanFullOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pnpm-workspace.yaml"), []byte("packages: ['*']\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "next.config.js"), []byte("module.exports = {};"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","dependencies":{"next":"^14"}}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := NewRecognizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{dir, "--no-audit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Path:", "Monorepo:", "Frameworks:", "Maturity:"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q section; got:\n%s", want, out)
		}
	}
}
