// go:build darwin && realworld
package keychain

import (
	"errors"
	"testing"
)

func TestSystemResolverRealKeychain(t *testing.T) {

	t.Setenv("ZEN_KEYCHAIN_DISABLE", "0")
	sec, err := SystemResolver{}.Lookup("anthropic-bypass", "refresh-token")
	switch {
	case err == nil:
		if sec.Len() == 0 {
			t.Error("Lookup returned nil error but empty secret")
		}
	case errors.Is(err, ErrNotFound):
		t.Skip("no anthropic-bypass entry provisioned; darwin Lookup path reached cleanly")
	default:
		t.Fatalf("Lookup hit a hard Keychain error: %v", err)
	}
}
