//go:build !darwin

// SPDX-License-Identifier: MIT

package keychain

import (
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

type SystemResolver struct{}

func (SystemResolver) Lookup(service, account string) (redact.Secret, error) {
	if keychainDisabled() {
		return lookupFromEnv(service)
	}

	if err := validateServiceName(service); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("keychain.Lookup(%q): %w (set ZEN_KEYCHAIN_DISABLE=1 to use env vars)", service, ErrUnsupported)
}
