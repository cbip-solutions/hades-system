// SPDX-License-Identifier: MIT

package verifier

import (
	"path/filepath"
	"strings"
)

func classifyArtifact(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".cdx.json"):
		return "sbom-cyclonedx"
	case strings.HasSuffix(base, ".spdx.json"):
		return "sbom-spdx"
	case strings.HasSuffix(base, ".intoto.jsonl"):
		return "attestation"
	case strings.HasSuffix(base, ".sig"):
		return "cosign-signature"
	case strings.HasSuffix(base, ".pem"):
		return "cosign-certificate"
	case strings.HasSuffix(base, ".sha256") || base == "checksums.txt":
		return "checksum"
	case strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tar.gz.txt"):
		return "binary"
	case strings.HasSuffix(base, ".deb"):
		return "deb"
	case strings.HasSuffix(base, ".rpm"):
		return "rpm"
	default:
		return "unknown"
	}
}

func classifyPlatform(filename string) string {
	for _, p := range canonicalPlatforms {
		if strings.Contains(filename, p) {
			return p
		}
	}
	return ""
}
