// SPDX-License-Identifier: MIT
// Package testhelpers provides shared test fixtures + helpers for
// zen-swarm tests. Importable from any *_test.go in the repo.
package testhelpers

import (
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/store"
)

func NewTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func NewStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

type TestStore struct {
	Store *store.Store
}

func OpenInMemoryStore(t *testing.T) *TestStore {
	t.Helper()
	return &TestStore{Store: NewTestStore(t)}
}
