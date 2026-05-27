//go:build darwin

// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"

	gokeychain "github.com/keybase/go-keychain"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func storeKeychainKey(service, key string) error {
	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(service)
	item.SetAccount("hades-system")
	item.SetLabel(service)
	item.SetData([]byte(key))
	item.SetSynchronizable(gokeychain.SynchronizableNo)
	item.SetAccessible(gokeychain.AccessibleWhenUnlockedThisDeviceOnly)
	err := gokeychain.AddItem(item)
	if err == nil {
		return nil
	}
	if errors.Is(err, gokeychain.ErrorDuplicateItem) {
		query := gokeychain.NewItem()
		query.SetSecClass(gokeychain.SecClassGenericPassword)
		query.SetService(service)
		query.SetAccount("hades-system")
		if delErr := gokeychain.DeleteItem(query); delErr != nil && !errors.Is(delErr, gokeychain.ErrorItemNotFound) {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("keychain rotate: delete-before-add: %w", delErr))
		}
		if addErr := gokeychain.AddItem(item); addErr != nil {
			return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("keychain rotate: re-add: %w", addErr))
		}
		return nil
	}
	return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("keychain rotate: store: %w", err))
}
