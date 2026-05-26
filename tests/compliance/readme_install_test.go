// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadReadme(t *testing.T) string {
	t.Helper()
	root := repoRootForHandbook(t)
	path := filepath.Join(root, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestReadmeInstallSectionExists(t *testing.T) {
	text := loadReadme(t)
	requiredFragments := []string{
		"## Install",
		"brew install cbip-solutions/tap/",
		"docker pull ghcr.io/cbip-solutions/hades-system",
		"sudo dpkg -i ",
		"sudo rpm -i ",
		"gh attestation verify",
		"xattr -d com.apple.quarantine",
		"docs/operations/release-engineering.md",
	}
	for _, frag := range requiredFragments {
		if !strings.Contains(text, frag) {
			t.Errorf("README install section missing fragment: %q", frag)
		}
	}
}

func TestReadmeNonInstallSectionsPreserved(t *testing.T) {
	text := loadReadme(t)
	requiredSections := []string{
		"## Documentation map",
		"## What Plan 15 adds",
		"## What Plan 13 adds",
		"## What Plan 11 adds",
		"## What Plan 9 adds",
		"## What Plan 8 adds",
		"## What Plan 7 adds",
		"## What Plan 6 adds",
		"## What Plan 5 adds",
		"## What Plan 4 adds",
		"## What Plan 3 adds",
		"## What Plan 2 adds",
		"## Usage examples",
		"## CLI command reference",
		"## Plugin",
		"## Develop",
		"## Doctrine",
		"## License",
	}
	for _, sect := range requiredSections {
		if !strings.Contains(text, sect) {
			t.Errorf("README missing pre-existing section: %q (D-12 accidentally removed?)", sect)
		}
	}
}

func TestReadmeNoClaudeAttribution(t *testing.T) {
	text := loadReadme(t)
	forbidden := []string{
		"Co-Authored-By: prohibited assistant",
		"Co-Authored-By: claude",
		"Generated with prohibited assistant",
		"generated with claude",
	}
	for _, bad := range forbidden {
		if strings.Contains(text, bad) {
			t.Errorf("README contains forbidden Claude attribution: %q", bad)
		}
	}
}

func TestReadmeInstallChannelOrder(t *testing.T) {
	text := loadReadme(t)
	installIdx := strings.Index(text, "## Install")
	if installIdx < 0 {
		t.Fatal("## Install section missing")
	}

	end := installIdx + 4000
	if end > len(text) {
		end = len(text)
	}
	section := text[installIdx:end]
	if endIdx := strings.Index(section[3:], "\n## "); endIdx > 0 {
		section = section[:endIdx+3]
	}

	brewIdx := strings.Index(section, "brew install")
	dockerIdx := strings.Index(section, "docker pull")
	dpkgIdx := strings.Index(section, "sudo dpkg")
	tarIdx := strings.Index(section, "xattr -d")

	if brewIdx < 0 {
		t.Fatal("brew install command missing")
	}
	if dockerIdx >= 0 && dockerIdx < brewIdx {
		t.Errorf("docker path appears before brew (dockerIdx=%d < brewIdx=%d); brew should be first", dockerIdx, brewIdx)
	}
	if dpkgIdx >= 0 && dpkgIdx < brewIdx {
		t.Errorf("dpkg path appears before brew (dpkgIdx=%d < brewIdx=%d); brew should be first", dpkgIdx, brewIdx)
	}
	if tarIdx >= 0 && tarIdx < brewIdx {
		t.Errorf("tarball path appears before brew (tarIdx=%d < brewIdx=%d); brew should be first", tarIdx, brewIdx)
	}
}
