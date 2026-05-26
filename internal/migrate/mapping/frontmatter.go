// SPDX-License-Identifier: MIT
package mapping

import (
	"strings"

	"github.com/cbip-solutions/hades-system/internal/migrate/pyident"
)

func generateFrontmatter(skillName string, body []byte) map[string]string {
	return map[string]string{
		"name":        skillName,
		"description": firstLine(body),
		"keywords":    strings.Join(extractKeywords(body, 6), ", "),
		"version":     "0.0.1",
		"license":     "imported",
	}
}

func firstLine(body []byte) string {
	lines := strings.SplitN(string(body), "\n", 16)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

func sanitizePyIdent(s string) string {
	return pyident.FromName(s)
}
