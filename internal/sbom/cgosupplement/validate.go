// SPDX-License-Identifier: MIT

package cgosupplement

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

func (s *Supplement) ValidateAgainstGoMod(goModPath string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read go.mod %s: %w", goModPath, err)
	}
	mf, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod %s: %w", goModPath, err)
	}

	gomodVersions := map[string]string{}
	for _, r := range mf.Require {
		gomodVersions[r.Mod.Path] = r.Mod.Version
	}

	var drifts []string
	for _, e := range s.Entries {

		if e.GoBinding == "" {
			continue
		}

		parts := strings.Fields(e.GoBinding)
		if len(parts) != 2 {
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: malformed hades-system:go-binding property %q (want '<module> <version>')",
				e.Name, e.GoBinding))
			continue
		}
		modPath := parts[0]
		expectedVersion := parts[1]

		actualVersion, ok := gomodVersions[modPath]
		if !ok {
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: go-binding %s not present in go.mod require block",
				e.Name, modPath))
			continue
		}
		if actualVersion != expectedVersion {
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: drift detected — supplement declares %s %s; go.mod has %s %s",
				e.Name, modPath, expectedVersion, modPath, actualVersion))
		}
	}
	if len(drifts) > 0 {
		return errors.New(strings.Join(drifts, "\n"))
	}
	return nil
}

func (s *Supplement) ValidateAgainstVendorDir(vendorRoot string) error {
	var drifts []string
	for _, e := range s.Entries {
		if e.VendorPath == "" {
			continue
		}

		rel := strings.TrimPrefix(e.VendorPath, "vendor/")
		fullPath := filepath.Join(vendorRoot, rel)

		st, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				drifts = append(drifts, fmt.Sprintf(
					"entry %q: vendor path %s does not exist", e.Name, fullPath))
				continue
			}
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: stat vendor path %s: %v", e.Name, fullPath, err))
			continue
		}
		if !st.IsDir() {
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: vendor path %s is not a directory", e.Name, fullPath))
			continue
		}

		licensePresent := false
		for _, candidate := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING", "COPYING.txt"} {
			if _, err := os.Stat(filepath.Join(fullPath, candidate)); err == nil {
				licensePresent = true
				break
			}
		}
		if !licensePresent {
			drifts = append(drifts, fmt.Sprintf(
				"entry %q: vendor path %s missing LICENSE file (Apache §4(d) attribution required)",
				e.Name, fullPath))
		}
	}
	if len(drifts) > 0 {
		return errors.New(strings.Join(drifts, "\n"))
	}
	return nil
}

func (s *Supplement) MergeIntoSBOM(sbomPath string) error {
	targetData, err := os.ReadFile(sbomPath)
	if err != nil {
		return fmt.Errorf("read target SBOM %s: %w", sbomPath, err)
	}

	var target map[string]json.RawMessage
	if err := json.Unmarshal(targetData, &target); err != nil {
		return fmt.Errorf("parse target SBOM %s: %w", sbomPath, err)
	}

	var existing []json.RawMessage
	if rawComp, ok := target["components"]; ok {
		if err := json.Unmarshal(rawComp, &existing); err != nil {
			return fmt.Errorf("parse target components: %w", err)
		}
	}

	existingNames := map[string]bool{}
	for _, raw := range existing {
		var probe struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &probe)
		existingNames[probe.Name] = true
	}

	var suppBOM map[string]json.RawMessage
	if err := json.Unmarshal(s.raw, &suppBOM); err != nil {
		return fmt.Errorf("parse supplement raw: %w", err)
	}
	var suppComps []json.RawMessage
	if rawComp, ok := suppBOM["components"]; ok {
		if err := json.Unmarshal(rawComp, &suppComps); err != nil {
			return fmt.Errorf("parse supplement components: %w", err)
		}
	}

	merged := existing
	for _, raw := range suppComps {
		var probe struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &probe)
		if existingNames[probe.Name] {
			continue
		}
		merged = append(merged, raw)
		existingNames[probe.Name] = true
	}

	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("marshal merged components: %w", err)
	}
	target["components"] = mergedBytes

	out, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged SBOM: %w", err)
	}

	tmpPath := sbomPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return fmt.Errorf("write tempfile %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, sbomPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, sbomPath, err)
	}
	return nil
}
