// SPDX-License-Identifier: MIT
package intent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
)

const manifestSchemaVersion = 1

var adrIDPattern = regexp.MustCompile(`^ADR-[0-9]{4}$`)

type coverageEntry struct {
	Package string   `toml:"package"`
	ADRs    []string `toml:"adrs"`
}

type CoverageManifest struct {
	SchemaVersion int             `toml:"schema_version"`
	Coverage      []coverageEntry `toml:"coverage"`

	byPackage map[string][]string
}

func (m *CoverageManifest) ADRsForPackage(pkg string) []string {
	if m == nil || m.byPackage == nil {
		return []string{}
	}
	out := m.byPackage[pkg]
	if out == nil {
		return []string{}
	}
	return out
}

func ManifestPathFor(canonicalPath string) string {
	return filepath.Join(canonicalPath, ".hades", "caronte-intent.toml")
}

func LoadCoverageManifest(path string) (*CoverageManifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &CoverageManifest{SchemaVersion: manifestSchemaVersion, byPackage: map[string][]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("caronte/intent: read manifest %s: %w", path, err)
	}
	var m CoverageManifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("caronte/intent: parse manifest %s: %w", path, err)
	}
	if m.SchemaVersion != manifestSchemaVersion {
		return nil, fmt.Errorf("caronte/intent: manifest %s schema_version=%d unsupported (want %d)", path, m.SchemaVersion, manifestSchemaVersion)
	}
	m.byPackage = make(map[string][]string, len(m.Coverage))
	for _, c := range m.Coverage {
		if c.Package == "" {
			return nil, fmt.Errorf("caronte/intent: manifest %s has a [[coverage]] with empty package", path)
		}
		for _, id := range c.ADRs {
			if !adrIDPattern.MatchString(id) {
				return nil, fmt.Errorf("caronte/intent: manifest %s package %q references malformed ADR id %q (want ADR-NNNN)", path, c.Package, id)
			}
		}

		m.byPackage[c.Package] = append(m.byPackage[c.Package], c.ADRs...)
	}
	return &m, nil
}
