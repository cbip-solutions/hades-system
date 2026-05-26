package caronte

import (
	"errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{ErrEngineClosed, ErrProjectUnavailable, ErrDegraded}
	for i, e := range sentinels {
		if e == nil {
			t.Fatalf("sentinel[%d] is nil", i)
		}
	}
	if errors.Is(ErrEngineClosed, ErrProjectUnavailable) ||
		errors.Is(ErrProjectUnavailable, ErrDegraded) ||
		errors.Is(ErrEngineClosed, ErrDegraded) {
		t.Error("sentinels must be distinct error values")
	}
}
