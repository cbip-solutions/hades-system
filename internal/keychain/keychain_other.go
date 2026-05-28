//go:build !darwin

// SPDX-License-Identifier: MIT

package keychain

import (
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

type SystemResolver struct{}

func (SystemResolver) Lookup(service, account string) (redact.Secret, error) {
	sec, envErr := lookupFromEnv(service)
	if envErr == nil {
		return sec, nil
	}
	if !errors.Is(envErr, ErrNotFound) {
		return nil, envErr
	}
	if keychainDisabled() {
		return nil, envErr
	}
	envVar, _ := CredentialPublicEnvVar(service)
	if envVar == "" {
		envVar = "HADES_KEYCHAIN_<PROVIDER>"
	}
	return nil, fmt.Errorf("keychain.Lookup(%q): %w (export %s)", service, ErrUnsupported, envVar)
}
