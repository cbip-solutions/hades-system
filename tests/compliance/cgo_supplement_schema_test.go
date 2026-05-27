// SPDX-License-Identifier: MIT

// tests/compliance/cgo_supplement_schema_test.go — CGO supplement
// structural compliance.
//
// Asserts configs/cgo-supplement.cdx.json parses as valid CycloneDX 1.6
// JSON con the 3 required components — entities syft cannot inventory from
// a compiled Go binary:
//
// 1. sqlite-vec (asg017; Apache-2.0; cgo-static; statically-linked C extension)
// 2. Foundation framework (Apple; Proprietary; system-framework; macOS SDK)
// 3. smacker/go-tree-sitter (smacker; MIT; cgo-static; Caronte native parsers
// policy + Caronte SHIPPED v0.18.0)
//
// Each component must declare a SPDX license identifier (or "Proprietary"
// for Foundation) and the hades-system:cgo-classification property
// (cgo-static | system-framework | vendored).
//
// reality-check rationale: the spec-text list of 6
// components included vllm-mlx + litestream + transparency.dev/trillian-tessera
// that either (a) do not exist in this codebase (no vendor/vllm-mlx, no
// benbjohnson/litestream in go.mod) or (b) ARE inventoried by syft already
// (github.com/transparency-dev/tessera is a pure-Go module syft can see).
// Per doctrine "no stubs": placeholder SBOM components promising vendoring
// not yet done would be misleading. Only entries syft genuinely cannot
// inventory belong in the CGO supplement.
//
// invariant structural check.

package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCGOSupplementCycloneDX16Valid(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, "docs", "sbom", "cgo-supplement.cdx.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var bom struct {
		BOMFormat    string `json:"bomFormat"`
		SpecVersion  string `json:"specVersion"`
		Version      int    `json:"version"`
		SerialNumber string `json:"serialNumber"`
		Components   []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Version   string `json:"version"`
			Publisher string `json:"publisher"`
			Licenses  []struct {
				License struct {
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"license"`
			} `json:"licenses"`
			Properties []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if bom.BOMFormat != "CycloneDX" {
		t.Errorf("bomFormat: want CycloneDX, got %q", bom.BOMFormat)
	}
	if bom.SpecVersion != "1.6" {
		t.Errorf("specVersion: want 1.6, got %q", bom.SpecVersion)
	}
	if bom.Version != 1 {
		t.Errorf("version: want 1, got %d", bom.Version)
	}
	if len(bom.SerialNumber) < 9 || bom.SerialNumber[:9] != "urn:uuid:" {
		t.Errorf("serialNumber: want urn:uuid:... prefix, got %q", bom.SerialNumber)
	}

	wantComponents := map[string]struct {
		license string
		zenType string
	}{
		"sqlite-vec":             {"Apache-2.0", "cgo-static"},
		"Foundation framework":   {"Proprietary", "system-framework"},
		"smacker/go-tree-sitter": {"MIT", "cgo-static"},
	}
	found := map[string]bool{}
	for _, c := range bom.Components {
		want, ok := wantComponents[c.Name]
		if !ok {
			continue
		}
		found[c.Name] = true

		gotLicense := ""
		if len(c.Licenses) > 0 {
			if c.Licenses[0].License.ID != "" {
				gotLicense = c.Licenses[0].License.ID
			} else {
				gotLicense = c.Licenses[0].License.Name
			}
		}
		if gotLicense != want.license {
			t.Errorf("component %s: license want %q got %q", c.Name, want.license, gotLicense)
		}

		gotType := ""
		for _, p := range c.Properties {
			if p.Name == "hades-system:cgo-classification" {
				gotType = p.Value
				break
			}
		}
		if gotType != want.zenType {
			t.Errorf("component %s: hades-system:cgo-classification want %q got %q", c.Name, want.zenType, gotType)
		}
	}
	for name := range wantComponents {
		if !found[name] {
			t.Errorf("required component missing: %s", name)
		}
	}
}
