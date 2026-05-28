// SPDX-License-Identifier: MIT
// Package keychain is the shared OS-credential resolver for hades-system/HADES.
//
// It exists so every direct backend (anthropic_paygo, gemini,
// openai_compat) resolves its API key from one place. Earlier,
// tier1-sidecar, internal/litestream, and the cohere
// ecosystem path each carried their own Keychain accessor; bypass's
// was hard-wired to the refresh-token slot and lived in a package
// internal/providers may not import (invariant). This package is
// boundary-neutral — importing it from internal/providers does NOT
// violate invariant (it imports only internal/redact + stdlib +
// github.com/keybase/go-keychain on darwin).
//
// Convention service = "hades/<provider>" or legacy
// "hades-system/<provider>", account = "hades-system" — the latter remains
// supported for private-history compatibility.
//
// Cross-platform path: SystemResolver.Lookup checks HADES_KEYCHAIN_* env
// aliases before any OS store. The legacy HADES_KEYCHAIN_* aliases and
// HADES_KEYCHAIN_DISABLE=1 remain supported for CI and old operator shells.
// Real-world integration tests skip cleanly when neither the Keychain nor
// an env var supplies a key.
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

// validateServiceName enforces the convention service = "hades/<provider>",
// "hades-system/<provider>", or "anthropic-bypass" (the legacy bypass slot).
// Returns ErrInvalidService wrapped with a descriptive message when the input
// violates the rules.
//
// Rules
// - non-empty
// - only [a-z0-9-/] characters (no uppercase, no UTF-8, no other punctuation)
// - no leading or trailing - or /
// - no consecutive separators (-- or // or -/ or /-)
//
// invariant (no silent credential mismatch): a well-formed service maps
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
	return "HADES_KEYCHAIN_" + envVarSuffix(service)
}

func publicEnvVarName(service string) string {
	return "HADES_KEYCHAIN_" + envVarSuffix(shortServiceName(service))
}

func publicFullEnvVarName(service string) string {
	return "HADES_KEYCHAIN_" + envVarSuffix(service)
}

func envVarSuffix(service string) string {
	var b strings.Builder
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

func shortServiceName(service string) string {
	if i := strings.LastIndex(service, "/"); i >= 0 && i < len(service)-1 {
		return service[i+1:]
	}
	return service
}

func CredentialEnvVars(service string) ([]string, error) {
	if err := validateServiceName(service); err != nil {
		return nil, err
	}
	candidates := []string{
		publicEnvVarName(service),
		publicFullEnvVarName(service),
		envVarName(service),
	}
	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, name := range candidates {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

// CredentialPublicEnvVar returns the single documented env-var alias for a
// credential service. Compatibility aliases are intentionally omitted so
// public CLI and daemon messages do not teach legacy/private names.
func CredentialPublicEnvVar(service string) (string, error) {
	if err := validateServiceName(service); err != nil {
		return "", err
	}
	return publicEnvVarName(service), nil
}

func lookupFromEnv(service string) (redact.Secret, error) {
	names, err := CredentialEnvVars(service)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		v := sanitizeKeyValue(os.Getenv(name))
		if v == "" {
			continue
		}
		return redact.NewSecret(v), nil
	}
	return nil, ErrNotFound
}

func sanitizeKeyValue(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) >= 2 && v[0] == '<' && v[len(v)-1] == '>' {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}
