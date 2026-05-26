//go:build darwin

package keychain

import (
	"errors"
	"testing"

	gokeychain "github.com/keybase/go-keychain"
	"github.com/cbip-solutions/hades-system/internal/manualkeychain"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

const keychainTestService = "zen-swarm/keychain-test-only"
const keychainTestAccount = "zen-swarm"

func storeTestEntry(t *testing.T, secret redact.Secret) {
	t.Helper()

	_ = deleteTestEntry()

	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(keychainTestService)
	item.SetAccount(keychainTestAccount)
	item.SetData(secret.Reveal())
	item.SetAccessible(gokeychain.AccessibleWhenUnlocked)
	if err := gokeychain.AddItem(item); err != nil {
		t.Fatalf("storeTestEntry: AddItem: %v", err)
	}
	t.Cleanup(func() { _ = deleteTestEntry() })
}

func deleteTestEntry() error {
	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(keychainTestService)
	item.SetAccount(keychainTestAccount)
	return gokeychain.DeleteItem(item)
}

func TestLookupDarwinRoundtrip(t *testing.T) {
	if !manualkeychain.Enabled {
		t.Skip(manualkeychain.Reason)
	}
	want := redact.NewSecret("sk-keychain-test-NOT-REAL-abc123")
	storeTestEntry(t, want)
	defer func() { _ = deleteTestEntry() }()

	t.Setenv("ZEN_KEYCHAIN_DISABLE", "0")

	got, err := SystemResolver{}.Lookup(keychainTestService, keychainTestAccount)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if string(got.Reveal()) != string(want.Reveal()) {
		t.Errorf("roundtrip mismatch: got %q, want %q",
			string(got.Reveal()), string(want.Reveal()))
	}
}

func TestLookupDarwinReturnsWipeable(t *testing.T) {
	if !manualkeychain.Enabled {
		t.Skip(manualkeychain.Reason)
	}
	storeTestEntry(t, redact.NewSecret("sk-wipe-darwin-test-1234"))
	defer func() { _ = deleteTestEntry() }()

	t.Setenv("ZEN_KEYCHAIN_DISABLE", "0")

	sec, err := SystemResolver{}.Lookup(keychainTestService, keychainTestAccount)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	revealed := sec.Reveal()
	if len(revealed) == 0 {
		t.Fatal("revealed buffer is empty")
	}
	sec.Wipe()
	for i, b := range revealed {
		if b != 0 {
			t.Errorf("byte %d = 0x%02x, want 0 after Wipe", i, b)
		}
	}
}

func TestLookupDarwinNotFoundError(t *testing.T) {
	if !manualkeychain.Enabled {
		t.Skip(manualkeychain.Reason)
	}

	_ = deleteTestEntry()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "0")

	_, err := SystemResolver{}.Lookup(keychainTestService, keychainTestAccount)
	if err == nil {
		t.Fatal("Lookup of absent entry returned nil error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want wrapping ErrNotFound", err)
	}
}
