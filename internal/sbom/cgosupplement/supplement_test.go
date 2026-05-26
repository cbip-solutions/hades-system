// SPDX-License-Identifier: MIT

package cgosupplement

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSupplement(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cgo-supplement.cdx.json")
	body := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "serialNumber": "urn:uuid:00000000-0000-0000-0000-000000000001",
  "components": [
    {
      "type": "library",
      "name": "sqlite-vec",
      "version": "0.1.6",
      "licenses": [{"license": {"id": "Apache-2.0"}}],
      "properties": [
        {"name":"hades-system:cgo-classification","value":"cgo-static"},
        {"name":"hades-system:go-binding","value":"github.com/asg017/sqlite-vec-go-bindings v0.1.6"}
      ]
    },
    {
      "type": "framework",
      "name": "Foundation framework",
      "version": "macos-sdk",
      "licenses": [{"license": {"name": "Proprietary"}}],
      "properties": [{"name":"hades-system:cgo-classification","value":"system-framework"}]
    },
    {
      "type": "library",
      "name": "transparency.dev/trillian-tessera",
      "version": "v0.1.0",
      "licenses": [{"license": {"id": "Apache-2.0"}}],
      "properties": [
        {"name":"hades-system:cgo-classification","value":"vendored"},
        {"name":"hades-system:vendor-path","value":"vendor/transparency.dev/trillian-tessera"}
      ]
    },
    {
      "type": "library",
      "name": "vllm-mlx",
      "version": "vendored-snapshot",
      "licenses": [{"license": {"id": "Apache-2.0"}}],
      "properties": [
        {"name":"hades-system:cgo-classification","value":"vendored"},
        {"name":"hades-system:vendor-path","value":"vendor/vllm-mlx"}
      ]
    },
    {
      "type": "library",
      "name": "litestream",
      "version": "v0.3.13",
      "licenses": [{"license": {"id": "Apache-2.0"}}],
      "properties": [
        {"name":"hades-system:cgo-classification","value":"vendored"},
        {"name":"hades-system:go-binding","value":"github.com/benbjohnson/litestream v0.3.13"}
      ]
    },
    {
      "type": "library",
      "name": "smacker/go-tree-sitter",
      "version": "v0.0.0-20240827094217-dd81d9e9be82",
      "licenses": [{"license": {"id": "MIT"}}],
      "properties": [
        {"name":"hades-system:cgo-classification","value":"cgo-static"},
        {"name":"hades-system:go-binding","value":"github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82"}
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeGoMod(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeVendorDir(t *testing.T, dir string, paths []string) string {
	t.Helper()
	vendor := filepath.Join(dir, "vendor")
	for _, p := range paths {
		full := filepath.Join(vendor, p)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(full, "LICENSE"), []byte("Apache License 2.0"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vendor
}

func TestSupplement_Load(t *testing.T) {
	dir := t.TempDir()
	path := writeSupplement(t, dir)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 6 {
		t.Fatalf("want 6 entries, got %d", len(s.Entries))
	}
	wantNames := map[string]bool{
		"sqlite-vec":                        false,
		"Foundation framework":              false,
		"transparency.dev/trillian-tessera": false,
		"vllm-mlx":                          false,
		"litestream":                        false,
		"smacker/go-tree-sitter":            false,
	}
	for _, e := range s.Entries {
		wantNames[e.Name] = true
	}
	for n, ok := range wantNames {
		if !ok {
			t.Errorf("entry missing: %s", n)
		}
	}
}

func TestSupplement_Load_RejectsInvalidBOMFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.cdx.json")
	if err := os.WriteFile(path, []byte(`{"bomFormat":"NotCycloneDX","specVersion":"1.6","version":1,"components":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for bomFormat!=CycloneDX, got nil")
	}
}

func TestSupplement_Load_RejectsInvalidSpecVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.cdx.json")
	if err := os.WriteFile(path, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.4","version":1,"components":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for specVersion!=1.6, got nil")
	}
}

func TestSupplement_ValidateAgainstGoMod_Pass(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, err := Load(suppPath)
	if err != nil {
		t.Fatal(err)
	}

	gomod := `module github.com/cbip-solutions/hades-system

go 1.25.6

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/benbjohnson/litestream v0.3.13
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	gmPath := writeGoMod(t, dir, gomod)
	if err := s.ValidateAgainstGoMod(gmPath); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestSupplement_ValidateAgainstGoMod_VersionDrift(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	gomod := `module github.com/cbip-solutions/hades-system

go 1.25.6

require (
	github.com/asg017/sqlite-vec-go-bindings v0.2.0
	github.com/benbjohnson/litestream v0.3.13
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	gmPath := writeGoMod(t, dir, gomod)
	err := s.ValidateAgainstGoMod(gmPath)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), "sqlite-vec") {
		t.Errorf("expected drift error mentioning sqlite-vec, got %v", err)
	}
}

func TestSupplement_ValidateAgainstGoMod_MissingEntry(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	gomod := `module github.com/cbip-solutions/hades-system

go 1.25.6

require (
	github.com/benbjohnson/litestream v0.3.13
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	gmPath := writeGoMod(t, dir, gomod)
	err := s.ValidateAgainstGoMod(gmPath)
	if err == nil {
		t.Fatal("expected missing-entry error, got nil")
	}
	if !strings.Contains(err.Error(), "sqlite-vec") || !strings.Contains(err.Error(), "not present") {
		t.Errorf("expected error mentioning sqlite-vec not present, got %v", err)
	}
}

func TestSupplement_ValidateAgainstVendorDir_Pass(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	writeVendorDir(t, dir, []string{
		"transparency.dev/trillian-tessera",
		"vllm-mlx",
	})
	if err := s.ValidateAgainstVendorDir(filepath.Join(dir, "vendor")); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestSupplement_ValidateAgainstVendorDir_MissingPath(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	writeVendorDir(t, dir, []string{
		"transparency.dev/trillian-tessera",
	})
	err := s.ValidateAgainstVendorDir(filepath.Join(dir, "vendor"))
	if err == nil {
		t.Fatal("expected error for missing vllm-mlx, got nil")
	}
	if !strings.Contains(err.Error(), "vllm-mlx") {
		t.Errorf("expected error mentioning vllm-mlx, got %v", err)
	}
}

func TestSupplement_ValidateAgainstVendorDir_MissingLICENSE(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	writeVendorDir(t, dir, []string{
		"transparency.dev/trillian-tessera",
		"vllm-mlx",
	})
	if err := os.Remove(filepath.Join(dir, "vendor", "vllm-mlx", "LICENSE")); err != nil {
		t.Fatal(err)
	}
	err := s.ValidateAgainstVendorDir(filepath.Join(dir, "vendor"))
	if err == nil {
		t.Fatal("expected error for missing LICENSE file, got nil")
	}
	if !strings.Contains(err.Error(), "LICENSE") {
		t.Errorf("expected error mentioning LICENSE, got %v", err)
	}
}

func TestSupplement_MergeIntoSBOM(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	autoSBOM := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "serialNumber": "urn:uuid:11111111-2222-3333-4444-555555555555",
  "components": [
    {"type": "library", "name": "github.com/cbip-solutions/hades-system", "version": "v1.0.0"}
  ]
}`
	autoPath := filepath.Join(dir, "auto.cdx.json")
	if err := os.WriteFile(autoPath, []byte(autoSBOM), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeIntoSBOM(autoPath); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(autoPath)
	var merged struct {
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatal(err)
	}
	if len(merged.Components) != 7 {
		t.Errorf("want 7 components (1 original + 6 supplement), got %d", len(merged.Components))
	}
	wantNames := map[string]bool{
		"sqlite-vec":                        false,
		"Foundation framework":              false,
		"transparency.dev/trillian-tessera": false,
		"vllm-mlx":                          false,
		"litestream":                        false,
		"smacker/go-tree-sitter":            false,
	}
	for _, c := range merged.Components {
		wantNames[c.Name] = true
	}
	for n, ok := range wantNames {
		if !ok {
			t.Errorf("merged SBOM missing supplement entry: %s", n)
		}
	}
}

func TestSupplement_MergeIntoSBOM_MissingTarget(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	if err := s.MergeIntoSBOM(filepath.Join(dir, "does-not-exist.cdx.json")); err == nil {
		t.Fatal("expected error for missing target SBOM, got nil")
	}
}

func TestSupplement_MergeIntoSBOM_InvalidTargetJSON(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	autoPath := filepath.Join(dir, "auto.cdx.json")
	if err := os.WriteFile(autoPath, []byte("not-json-at-all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.MergeIntoSBOM(autoPath); err == nil {
		t.Fatal("expected error for invalid target JSON, got nil")
	}
}

func TestSupplement_ValidateAgainstGoMod_MissingFile(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	if err := s.ValidateAgainstGoMod(filepath.Join(dir, "does-not-exist", "go.mod")); err == nil {
		t.Fatal("expected error for missing go.mod, got nil")
	}
}

func TestSupplement_ValidateAgainstGoMod_MalformedBinding(t *testing.T) {

	dir := t.TempDir()
	suppPath := filepath.Join(dir, "bad-binding.cdx.json")
	body := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"serialNumber":"urn:uuid:00000000-0000-0000-0000-000000000002","components":[{"type":"library","name":"bad","version":"v0","licenses":[{"license":{"id":"MIT"}}],"properties":[{"name":"hades-system:cgo-classification","value":"cgo-static"},{"name":"hades-system:go-binding","value":"no-version-just-module-path"}]}]}`
	if err := os.WriteFile(suppPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(suppPath)
	if err != nil {
		t.Fatal(err)
	}

	gomod := `module example
go 1.25
`
	gmPath := writeGoMod(t, dir, gomod)
	err = s.ValidateAgainstGoMod(gmPath)
	if err == nil {
		t.Fatal("expected malformed-binding error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("expected error mentioning 'malformed', got %v", err)
	}
}

func TestSupplement_MergeIntoSBOM_Idempotent(t *testing.T) {
	dir := t.TempDir()
	suppPath := writeSupplement(t, dir)
	s, _ := Load(suppPath)

	autoPath := filepath.Join(dir, "auto.cdx.json")
	if err := os.WriteFile(autoPath, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeIntoSBOM(autoPath); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeIntoSBOM(autoPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(autoPath)
	var merged struct {
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, c := range merged.Components {
		if c.Name == "sqlite-vec" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("idempotent merge expected 1 sqlite-vec entry, got %d", count)
	}
}
