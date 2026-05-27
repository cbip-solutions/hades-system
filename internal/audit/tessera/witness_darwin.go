//go:build darwin

// SPDX-License-Identifier: MIT

package tessera

import (
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"os"
	"sync"

	gokeychain "github.com/keybase/go-keychain"
)

const (
	witnessKeychainService = "zen-swarm-tessera-witness"
	witnessKeychainAccount = "default"
)

type macWitnessBackend struct{}

type memWitnessBackend struct {
	mu  sync.Mutex
	key *ecdsa.PrivateKey
}

var defaultMemWitnessBackend = &memWitnessBackend{}

func resetTestWitnessKeychain() {
	defaultMemWitnessBackend.mu.Lock()
	defer defaultMemWitnessBackend.mu.Unlock()
	defaultMemWitnessBackend.key = nil
}

func defaultWitnessBackend() witnessBackend {
	if os.Getenv("ZEN_BYPASS_DISABLE_KEYCHAIN") == "1" {
		return defaultMemWitnessBackend
	}
	return &macWitnessBackend{}
}

func (m *macWitnessBackend) Load() (*ecdsa.PrivateKey, error) {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(witnessKeychainService)
	q.SetAccount(witnessKeychainAccount)
	q.SetMatchLimit(gokeychain.MatchLimitOne)
	q.SetReturnData(true)
	results, err := gokeychain.QueryItem(q)
	if err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return nil, ErrWitnessKeyMissing
		}
		return nil, err
	}
	if len(results) == 0 {
		return nil, ErrWitnessKeyMissing
	}
	priv, err := x509.ParseECPrivateKey(results[0].Data)
	if err != nil {
		return nil, err
	}
	return priv, nil
}

func (m *macWitnessBackend) Store(priv *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(witnessKeychainService)
	item.SetAccount(witnessKeychainAccount)
	item.SetLabel(witnessKeychainService + "/" + witnessKeychainAccount)
	item.SetData(der)
	item.SetSynchronizable(gokeychain.SynchronizableNo)
	item.SetAccessible(gokeychain.AccessibleWhenUnlockedThisDeviceOnly)
	if err := gokeychain.AddItem(item); err != nil {
		if errors.Is(err, gokeychain.ErrorDuplicateItem) {

			return ErrWitnessKeyAlreadyExists
		}
		return err
	}
	return nil
}

func (m *macWitnessBackend) Delete() error {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(witnessKeychainService)
	q.SetAccount(witnessKeychainAccount)
	if err := gokeychain.DeleteItem(q); err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (m *memWitnessBackend) Load() (*ecdsa.PrivateKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key == nil {
		return nil, ErrWitnessKeyMissing
	}
	return m.key, nil
}

func (m *memWitnessBackend) Store(priv *ecdsa.PrivateKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key != nil {
		return ErrWitnessKeyAlreadyExists
	}
	m.key = priv
	return nil
}

func (m *memWitnessBackend) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.key = nil
	return nil
}
