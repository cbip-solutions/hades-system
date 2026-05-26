package bcdetect

import (
	"errors"
	"testing"
)

func TestSentinelErrorsAreNonNilAndDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrUnknownDetectorKind": ErrUnknownDetectorKind,
		"ErrSpecTooLarge":        ErrSpecTooLarge,
		"ErrInvalidSpec":         ErrInvalidSpec,
		"ErrNodeBinaryMissing":   ErrNodeBinaryMissing,
		"ErrBespokeDiffRefused":  ErrBespokeDiffRefused,
		"ErrParamsBelowFloor":    ErrParamsBelowFloor,
	}

	for name, e := range sentinels {
		if e == nil {
			t.Errorf("%s is nil", name)
		}
	}
	// Distinctness gate: errors.Is between any two distinct sentinels MUST
	// be false. Self-equality MUST hold.
	for nameA, a := range sentinels {
		for nameB, b := range sentinels {
			if nameA == nameB {
				if !errors.Is(a, b) {
					t.Errorf("%s is not Is-equal to itself", nameA)
				}
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("%s and %s are Is-equal but should be distinct", nameA, nameB)
			}
		}
	}
}

func TestSentinelMessagesArePrefixed(t *testing.T) {
	for name, e := range map[string]error{
		"ErrUnknownDetectorKind": ErrUnknownDetectorKind,
		"ErrSpecTooLarge":        ErrSpecTooLarge,
		"ErrInvalidSpec":         ErrInvalidSpec,
		"ErrNodeBinaryMissing":   ErrNodeBinaryMissing,
		"ErrBespokeDiffRefused":  ErrBespokeDiffRefused,
		"ErrParamsBelowFloor":    ErrParamsBelowFloor,
	} {
		if got := e.Error(); !startsWith(got, "bcdetect:") {
			t.Errorf("%s message %q missing 'bcdetect:' prefix", name, got)
		}
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
