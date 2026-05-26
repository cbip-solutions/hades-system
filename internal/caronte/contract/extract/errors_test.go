package extract

import (
	"errors"
	"testing"
)

func TestErrDuplicateExtractorIsDistinct(t *testing.T) {
	if ErrDuplicateExtractor == nil {
		t.Fatal("ErrDuplicateExtractor is nil; sentinel must be a real error value")
	}
	other := errors.New("caronte/extract: some other error")
	if errors.Is(other, ErrDuplicateExtractor) {
		t.Error("ErrDuplicateExtractor matched an unrelated error; sentinel is not distinct")
	}
}

func TestErrNoExtractorIsDistinct(t *testing.T) {
	if ErrNoExtractor == nil {
		t.Fatal("ErrNoExtractor is nil; sentinel must be a real error value")
	}
	if errors.Is(ErrNoExtractor, ErrDuplicateExtractor) {
		t.Error("ErrNoExtractor matched ErrDuplicateExtractor; the two sentinels collided")
	}
	if errors.Is(ErrDuplicateExtractor, ErrNoExtractor) {
		t.Error("ErrDuplicateExtractor matched ErrNoExtractor; the two sentinels collided")
	}
}

func TestSentinelMessagePrefix(t *testing.T) {
	for _, e := range []error{ErrDuplicateExtractor, ErrNoExtractor} {
		if e.Error() == "" {
			t.Errorf("sentinel %v has empty message", e)
		}

		if got := e.Error(); got[:len("caronte/extract:")] != "caronte/extract:" {
			t.Errorf("sentinel %q does not start with caronte/extract: prefix", got)
		}
	}
}
