// SPDX-License-Identifier: MIT
// Package keychain is the shared OS-credential resolver for hades-system.
//
// It exists so every direct backend (anthropic_paygo, gemini,
// openai_compat) resolves its API key from one place. Earlier,
// private-tier1-module, internal/litestream, and the cohere
// ecosystem path each carried their own Keychain accessor; bypass's
// was hard-wired to the refresh-token slot and lived in a package
// internal/providers may not import (inv-hades-031). This package is
// boundary-neutral — importing it from internal/providers does NOT
// violate inv-hades-031 (it imports only internal/redact + stdlib +
// github.com/keybase/go-keychain on darwin).
//
// Convention service = "hades-system/<provider>", account = "hades-system" —
// the form documented in HANDOFF
// (security add-generic-password -s "hades-system/<provider>" -a "hades-system").
//
// CI path: the macOS Keychain is not reliably available in CI (known
// blocker — HADES_BYPASS_DISABLE_KEYCHAIN=1 precedent). When
// HADES_KEYCHAIN_DISABLE=1 is set, SystemResolver.Lookup reads the
// credential from a HADES_KEYCHAIN_<SERVICE> environment variable instead
// (service mangled to upper-case with every non-alphanumeric run
// collapsed to a single underscore). Real-world integration tests skip
// cleanly when neither the Keychain nor the env var supplies a key.
package keychain

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

type Resolver interface {
	Lookup(service, account string) (redact.Secret, error)
}

var ErrNotFound = errors.New("keychain: entry not found")

var ErrUnsupported = errors.New("keychain: unsupported platform")

var ErrInvalidService = errors.New("keychain: invalid service name")

// validateServiceName enforces the convention service = "hades-system/<provider>"
// or "anthropic-bypass" (the legacy bypass slot). Returns ErrInvalidService
// wrapped with a descriptive message when the input violates the rules.
//
// Rules
// - non-empty
// - only [a-z0-9-/] characters (no uppercase, no UTF-8, no other punctuation)
// - no leading or trailing - or /
// - no consecutive separators (-- or // or -/ or /-)
//
// inv-hades-068 (no silent credential mismatch): a well-formed service maps
// to exactly one env var and exactly one Keychain slot.
//
// Implemented as a byte-level scan (no regexp) to avoid the regexp
// dependency in a security-critical hot path.
func validateServiceName(service string) error {
	if service == "" {
		return fmt.Errorf("keychain: service is empty: %w", ErrInvalidService)
	}

	for i := 0; i < len(service); i++ {
		b := service[i]
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '/' {
			continue
		}
		return fmt.Errorf("keychain: service %q contains invalid byte 0x%02x at position %d: %w",
			service, b, i, ErrInvalidService)
	}

	first, last := service[0], service[len(service)-1]
	if first == '-' || first == '/' {
		return fmt.Errorf("keychain: service %q has leading separator %q: %w",
			service, first, ErrInvalidService)
	}
	if last == '-' || last == '/' {
		return fmt.Errorf("keychain: service %q has trailing separator %q: %w",
			service, last, ErrInvalidService)
	}

	for i := 0; i < len(service)-1; i++ {
		cur, next := service[i], service[i+1]
		isSep := func(c byte) bool { return c == '-' || c == '/' }
		if isSep(cur) && isSep(next) {
			return fmt.Errorf("keychain: service %q has consecutive separators at position %d: %w",
				service, i, ErrInvalidService)
		}
	}
	return nil
}

const disableEnvVar = "HADES_KEYCHAIN_DISABLE"

func keychainDisabled() bool {
	return os.Getenv(disableEnvVar) == "1"
}

func envVarName(service string) string {
	var b strings.Builder
	b.WriteString("HADES_KEYCHAIN_")
	prevUnderscore := false
	for _, r := range strings.ToUpper(service) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}

		if !prevUnderscore {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}
	return b.String()
}

func lookupFromEnv(service string) (redact.Secret, error) {
	if err := validateServiceName(service); err != nil {
		return nil, err
	}
	v := sanitizeKeyValue(os.Getenv(envVarName(service)))
	if v == "" {
		return nil, ErrNotFound
	}
	return redact.NewSecret(v), nil
}

func sanitizeKeyValue(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) >= 2 && v[0] == '<' && v[len(v)-1] == '>' {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}
