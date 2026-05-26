//go:build darwin

// SPDX-License-Identifier: MIT

package litestream

import (
	"errors"
	"fmt"

	keychain "github.com/keybase/go-keychain"
)

func loadKeychainImpl(projectID string) (S3Credentials, error) {
	q := keychain.NewItem()
	q.SetSecClass(keychain.SecClassGenericPassword)
	q.SetService(s3CredentialsServiceName(projectID))
	q.SetMatchLimit(keychain.MatchLimitOne)
	q.SetReturnData(true)
	q.SetReturnAttributes(true)
	results, err := keychain.QueryItem(q)
	if err != nil {
		if errors.Is(err, keychain.ErrorItemNotFound) {
			return S3Credentials{}, ErrKeychainNoSuchEntry
		}
		return S3Credentials{}, fmt.Errorf("litestream: keychain query: %w", err)
	}
	if len(results) == 0 {
		return S3Credentials{}, ErrKeychainNoSuchEntry
	}
	return parseKeychainPayload(results[0].Data)
}

func saveKeychainImpl(projectID string, creds S3Credentials) error {
	body, err := formatKeychainPayload(creds)
	if err != nil {
		return err
	}

	_ = deleteKeychainImpl(projectID)

	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(s3CredentialsServiceName(projectID))
	item.SetAccount("zen-swarm-audit-s3")
	item.SetData(body)
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)
	if err := keychain.AddItem(item); err != nil {
		return fmt.Errorf("litestream: keychain add: %w", err)
	}
	return nil
}

func deleteKeychainImpl(projectID string) error {
	q := keychain.NewItem()
	q.SetSecClass(keychain.SecClassGenericPassword)
	q.SetService(s3CredentialsServiceName(projectID))
	if err := keychain.DeleteItem(q); err != nil {
		if errors.Is(err, keychain.ErrorItemNotFound) {
			return nil
		}
		return fmt.Errorf("litestream: keychain delete: %w", err)
	}
	return nil
}
