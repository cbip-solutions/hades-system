//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func CanonicalizeQuery(in string) string {

	s := strings.TrimFunc(in, unicode.IsSpace)

	s = norm.NFC.String(s)

	s = strings.Map(unicode.ToLower, s)

	s = collapseWhitespace(s)

	return s
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}

		} else {
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}

func ComputeQueryHash(query string) string {
	c := CanonicalizeQuery(query)
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:])
}
