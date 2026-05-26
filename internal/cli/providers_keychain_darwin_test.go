//go:build darwin

package cli

import (
	"errors"
	"testing"

	gokeychain "github.com/keybase/go-keychain"
	"github.com/cbip-solutions/hades-system/internal/manualkeychain"
)

const keychainTestService = "zen-swarm-test/providers-rotate"

func deleteTestKeychainEntry(t *testing.T) {
	t.Helper()
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(keychainTestService)
	q.SetAccount("zen-swarm")
	if err := gokeychain.DeleteItem(q); err != nil && !errors.Is(err, gokeychain.ErrorItemNotFound) {
		t.Errorf("cleanup deleteTestKeychainEntry: %v", err)
	}
}

func readTestKeychainEntry(t *testing.T) ([]byte, error) {
	t.Helper()
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(keychainTestService)
	q.SetAccount("zen-swarm")
	q.SetMatchLimit(gokeychain.MatchLimitOne)
	q.SetReturnData(true)
	results, err := gokeychain.QueryItem(q)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, gokeychain.ErrorItemNotFound
	}
	cp := make([]byte, len(results[0].Data))
	copy(cp, results[0].Data)
	return cp, nil
}

func TestStoreKeychainKey_RoundTrip(t *testing.T) {
	if !manualkeychain.Enabled {
		t.Skip(manualkeychain.Reason)
	}

	_ = func() error {
		q := gokeychain.NewItem()
		q.SetSecClass(gokeychain.SecClassGenericPassword)
		q.SetService(keychainTestService)
		q.SetAccount("zen-swarm")
		return gokeychain.DeleteItem(q)
	}()
	t.Cleanup(func() { deleteTestKeychainEntry(t) })
	defer deleteTestKeychainEntry(t)

	const want = "sk-test-roundtrip-value-1"
	if err := storeKeychainKey(keychainTestService, want); err != nil {
		t.Fatalf("storeKeychainKey first call: %v", err)
	}
	got, err := readTestKeychainEntry(t)
	if err != nil {
		t.Fatalf("readTestKeychainEntry: %v", err)
	}
	if string(got) != want {
		t.Errorf("readback = %q, want %q", string(got), want)
	}
}

// TestStoreKeychainKey_OverwritesDuplicate verifies the
// ErrorDuplicateItem → delete-then-add overwrite path: a second call
// with a different value MUST succeed and the new value MUST be visible
// on the next read. This is the path the reviewer flagged as
// reachable-in-practice (stale Keychain from a previous rotate).
func TestStoreKeychainKey_OverwritesDuplicate(t *testing.T) {
	if !manualkeychain.Enabled {
		t.Skip(manualkeychain.Reason)
	}

	_ = func() error {
		q := gokeychain.NewItem()
		q.SetSecClass(gokeychain.SecClassGenericPassword)
		q.SetService(keychainTestService)
		q.SetAccount("zen-swarm")
		return gokeychain.DeleteItem(q)
	}()
	t.Cleanup(func() { deleteTestKeychainEntry(t) })
	defer deleteTestKeychainEntry(t)

	if err := storeKeychainKey(keychainTestService, "first-value"); err != nil {
		t.Fatalf("storeKeychainKey first: %v", err)
	}

	const second = "second-value-overwrites-first"
	if err := storeKeychainKey(keychainTestService, second); err != nil {
		t.Fatalf("storeKeychainKey second (overwrite): %v", err)
	}

	got, err := readTestKeychainEntry(t)
	if err != nil {
		t.Fatalf("readTestKeychainEntry after overwrite: %v", err)
	}
	if string(got) != second {
		t.Errorf("readback after overwrite = %q, want %q (delete-then-add path)", string(got), second)
	}
}
