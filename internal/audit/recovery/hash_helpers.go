// SPDX-License-Identifier: MIT
// internal/audit/recovery/hash_helpers.go
package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func sha256OfReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
