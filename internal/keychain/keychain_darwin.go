// go:build darwin

// SPDX-License-Identifier: MIT

package keychain

import (
	"errors"
	"fmt"

	gokeychain "github.com/keybase/go-keychain"
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

	query := gokeychain.NewItem()
	query.SetSecClass(gokeychain.SecClassGenericPassword)
	query.SetService(service)
	query.SetAccount(account)
	query.SetMatchLimit(gokeychain.MatchLimitOne)
	query.SetReturnData(true)

	results, err := gokeychain.QueryItem(query)
	if err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return nil, fmt.Errorf("keychain.Lookup(%q): %w", service, ErrNotFound)
		}
		return nil, fmt.Errorf("keychain.Lookup(%q): query: %w", service, err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("keychain.Lookup(%q): %w", service, ErrNotFound)
	}

	sanitized := sanitizeKeyValue(string(results[0].Data))
	if sanitized == "" {
		return nil, fmt.Errorf("keychain.Lookup(%q): value sanitises to empty (paste-error or empty entry?): %w", service, ErrNotFound)
	}
	cp := make([]byte, len(sanitized))
	copy(cp, sanitized)
	return redact.Secret(cp), nil
}
