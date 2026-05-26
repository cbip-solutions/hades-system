package store

import (
	"errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{ErrCGODisabled, ErrEmptyDB, ErrNotFound}
	for i, e := range sentinels {
		if e == nil {
			t.Fatalf("sentinel[%d] is nil", i)
		}
	}
	if errors.Is(ErrCGODisabled, ErrEmptyDB) || errors.Is(ErrEmptyDB, ErrNotFound) || errors.Is(ErrCGODisabled, ErrNotFound) {
		t.Error("sentinels must be distinct error values")
	}
}

func TestDefaultDriverIsMattn(t *testing.T) {
	if DefaultDriver != "sqlite3" {
		t.Errorf("DefaultDriver = %q; want \"sqlite3\" (mattn CGO driver)", DefaultDriver)
	}
}
