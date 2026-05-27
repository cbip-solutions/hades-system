// go:build !darwin
package keychain

import (
	"errors"
	"testing"
)

func TestLookupNonDarwinReturnsErrUnsupported(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "0")
	_, err := SystemResolver{}.Lookup("zen-swarm/x", "zen-swarm")
	if err == nil {
		t.Fatal("Lookup returned nil error on non-darwin")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want chain to ErrUnsupported", err)
	}
}
