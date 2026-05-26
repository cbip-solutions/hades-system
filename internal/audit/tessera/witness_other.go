//go:build !darwin

// SPDX-License-Identifier: MIT

package tessera

import (
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type fileWitnessBackend struct {
	mu sync.Mutex
}

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
	return &fileWitnessBackend{}
}

func witnessFilePath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "zen-swarm", "witness-priv.der"), nil
}

func (f *fileWitnessBackend) Load() (*ecdsa.PrivateKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := witnessFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrWitnessKeyMissing
		}
		return nil, err
	}
	return x509.ParseECPrivateKey(data)
}

func (f *fileWitnessBackend) Store(priv *ecdsa.PrivateKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := witnessFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return ErrWitnessKeyAlreadyExists
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	return os.WriteFile(p, der, 0o600)
}

func (f *fileWitnessBackend) Delete() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := witnessFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
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
