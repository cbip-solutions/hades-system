// go:build !darwin

// SPDX-License-Identifier: MIT

package cli

import "errors"

func storeKeychainKey(_, _ string) error {
	return errors.New("keychain rotate: unsupported platform (macOS Keychain required)")
}
