// SPDX-License-Identifier: MIT
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode"
)

func Slug(absPath string) string {
	base := strings.ToLower(filepath.Base(absPath))
	var clean strings.Builder
	clean.Grow(len(base))
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || unicode.IsDigit(r) {
			clean.WriteRune(r)
		}
	}
	if clean.Len() == 0 {
		clean.WriteString("project")
	}
	sum := sha256.Sum256([]byte(absPath))
	prefix := hex.EncodeToString(sum[:])[:8]
	return clean.String() + "-" + prefix
}
