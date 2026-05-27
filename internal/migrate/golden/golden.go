// SPDX-License-Identifier: MIT
// Package golden provides regression-protected fixtures for the migrate
// pipeline. Each fixture pair has input/ + output/ directories; the harness
// runs source.ReadAll → mapping.Map → writer.Apply against a temp dir, then
// asserts byte-identical output.
//
// Doctrine alignment: adding a NEW mapping table row REQUIRES adding a
// fixture pair (TestGoldenFixturesCoverEntireMappingTable enforces). "No
// tech debt" means drift in the mapping table → golden test failure → halt.
package golden

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

func runFixture(t *testing.T, fixtureName string) {
	t.Helper()
	inputDir := filepath.Join("fixtures", fixtureName, "input")
	outputDir := filepath.Join("fixtures", fixtureName, "output")
	if _, err := os.Stat(inputDir); err != nil {
		t.Skipf("fixture missing input/: %v", err)
		return
	}
	if _, err := os.Stat(outputDir); err != nil {
		t.Skipf("fixture missing output/: %v", err)
		return
	}
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "hades-system")
	hermesCfg := filepath.Join(tmp, "hermes", "config.yaml")
	zenCfgRoot := filepath.Join(tmp, "hades-config")
	inv, err := source.ReadAll(inputDir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	plan, err := mapping.Map(inv, mapping.PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	w := writer.New(writer.WriterConfig{
		HermesPluginRoot: pluginRoot,
		HermesConfigPath: hermesCfg,
		HadesConfigRoot:  zenCfgRoot,
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertDirsByteIdentical(t, outputDir, tmp)
}

func assertDirsByteIdentical(t *testing.T, expected, actual string) {
	t.Helper()
	if err := filepath.Walk(expected, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(expected, path)
		if err != nil {
			return err
		}
		actualPath := filepath.Join(actual, rel)
		expectedBody, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actualBody, err := os.ReadFile(actualPath)
		if err != nil {
			t.Errorf("missing in actual: %s (%v)", rel, err)
			return nil
		}

		expectedNorm := normalizeFixtureBody(expectedBody, actualBody)
		if !bytesEqual(expectedNorm, actualBody) {
			t.Errorf("byte-diff: %s\nEXPECTED (normalized):\n%s\nACTUAL:\n%s",
				rel, expectedNorm, actualBody)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeFixtureBody(expected, actual []byte) []byte {
	const ph = "<SOURCE>"
	if !contains(expected, []byte(ph)) {
		return expected
	}

	actStr := string(actual)
	markers := []string{"# Raw source: ", "# 1:1 preservation per inv-hades-183. Source: "}
	for _, m := range markers {
		if idx := strings.Index(actStr, m); idx >= 0 {
			end := strings.Index(actStr[idx:], "\n")
			if end < 0 {
				end = len(actStr) - idx
			}
			replacement := actStr[idx+len(m) : idx+end]
			return []byte(strings.ReplaceAll(string(expected), ph, replacement))
		}
	}
	return expected
}

func contains(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
