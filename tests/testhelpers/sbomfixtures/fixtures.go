// SPDX-License-Identifier: MIT

package sbomfixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func FixtureSupplement(t *testing.T, dir string) string {
	t.Helper()
	supplementDir := filepath.Join(dir, "docs", "sbom")
	if err := os.MkdirAll(supplementDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(supplementDir, "cgo-supplement.cdx.json")

	bom := map[string]interface{}{
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.6",
		"version":      1,
		"serialNumber": "urn:uuid:fixture-0000-0000-0000-000000000001",
		"components": []map[string]interface{}{
			{
				"type":     "library",
				"name":     "sqlite-vec",
				"version":  "0.1.6",
				"licenses": []map[string]interface{}{{"license": map[string]interface{}{"id": "Apache-2.0"}}},
				"properties": []map[string]interface{}{
					{"name": "hades-system:cgo-classification", "value": "cgo-static"},
					{"name": "hades-system:go-binding", "value": "github.com/asg017/sqlite-vec-go-bindings v0.1.6"},
				},
			},
			{
				"type":     "framework",
				"name":     "Foundation framework",
				"version":  "macos-sdk",
				"licenses": []map[string]interface{}{{"license": map[string]interface{}{"name": "Proprietary"}}},
				"properties": []map[string]interface{}{
					{"name": "hades-system:cgo-classification", "value": "system-framework"},
				},
			},
			{
				"type":     "library",
				"name":     "smacker/go-tree-sitter",
				"version":  "v0.0.0-20240827094217-dd81d9e9be82",
				"licenses": []map[string]interface{}{{"license": map[string]interface{}{"id": "MIT"}}},
				"properties": []map[string]interface{}{
					{"name": "hades-system:cgo-classification", "value": "cgo-static"},
					{"name": "hades-system:go-binding", "value": "github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(bom, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func FixtureGoMod(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "go.mod")
	body := `module github.com/cbip-solutions/hades-system

go 1.25.6

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func FixtureVendorDir(t *testing.T, dir string) string {
	t.Helper()
	vendor := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	return vendor
}

func FixtureDistArtifact(t *testing.T, dir, platform string) string {
	t.Helper()
	distDir := filepath.Join(dir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tarPath := filepath.Join(distDir, "hades-system-1.0.0-"+platform+".tar.gz")
	if err := os.WriteFile(tarPath, []byte("dummy tarball bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	companions := map[string]string{
		".sha256":       "abc123  hades-system-1.0.0-" + platform + ".tar.gz\n",
		".cdx.json":     `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"serialNumber":"urn:uuid:fixture-0000-0000-0000-000000000002","components":[{"name":"sqlite-vec"},{"name":"Foundation framework"},{"name":"smacker/go-tree-sitter"}]}`,
		".spdx.json":    `{"spdxVersion":"SPDX-3.0.1","SPDXID":"SPDXRef-DOCUMENT","name":"hades-system-1.0.0-` + platform + `"}`,
		".sig":          "fake-signature-bytes",
		".pem":          "-----BEGIN CERTIFICATE-----\nFAKE\n-----END CERTIFICATE-----\n",
		".intoto.jsonl": `{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"hades-system","digest":{"sha256":"abc"}}]}`,
	}
	for suffix, content := range companions {
		if err := os.WriteFile(tarPath+suffix, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return tarPath
}
