// SPDX-License-Identifier: MIT
package redact

import (
	"crypto/subtle"
	"fmt"
	"strconv"
)

type Secret []byte

func NewSecret(s string) Secret {
	if s == "" {
		return nil
	}
	cp := make([]byte, len(s))
	copy(cp, s)
	return Secret(cp)
}

func (s Secret) String() string { return Marker }

func (s Secret) Format(f fmt.State, verb rune) {

	switch verb {
	case 'x':
		fmt.Fprintf(f, "%x", []byte(Marker))
	case 'X':
		fmt.Fprintf(f, "%X", []byte(Marker))
	case 'q':
		fmt.Fprint(f, strconv.Quote(Marker))
	default:

		if width, ok := f.Width(); ok {
			fmt.Fprintf(f, "%-*s", width, Marker)
		} else {
			fmt.Fprint(f, Marker)
		}
	}
}

func (s Secret) GoString() string { return Marker }

func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + Marker + `"`), nil
}

func (s Secret) MarshalText() ([]byte, error) {
	return []byte(Marker), nil
}

// Reveal returns the underlying bytes. ONLY for narrow callsites:
//
// - internal/redact/transport.go: setting the Authorization header.
// - private-tier1-module/refresh.go: building the OAuth refresh
// request body.
// - private-tier1-module/credentials.go: writing to macOS Keychain
// via security(1).
//
// Returns nil for an empty Secret.
//
// IMPORTANT — aliasing contract:
//
// The returned []byte ALIASES the Secret's internal storage. This is
// deliberate: callers transmit the bytes immediately (HTTP header,
// keychain write) and a copy would expand the plaintext footprint
// without benefit. But it imposes three rules on the caller:
// 1. Do NOT retain the slice past the immediate transmission. The
// slice header captured in a variable keeps the Secret's
// plaintext alive past Wipe().
// 2. Do NOT mutate the slice. Mutation corrupts the Secret in
// place; subsequent Reveal()/Equal()/Wipe() see garbage.
// 3. Do NOT call Reveal() concurrently with Wipe() on the same
// Secret — the slice can be partially zeroed mid-read. See
// Wipe() docs.
//
// If you need to retain plaintext (e.g., to pass to a third-party SDK
// that copies asynchronously), explicitly copy:
//
// cp := append([]byte(nil), s.Reveal()...)
func (s Secret) Reveal() []byte {
	if len(s) == 0 {
		return nil
	}
	return []byte(s)
}

func (s Secret) Len() int { return len(s) }

func (s Secret) Equal(other Secret) bool {
	return subtle.ConstantTimeCompare(s, other) == 1
}

func (s Secret) Wipe() {
	for i := range s {
		s[i] = 0
	}
}
