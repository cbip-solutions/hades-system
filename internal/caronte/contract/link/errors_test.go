package link

import (
	"errors"
	"testing"
)

func TestLinkSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrCGODisabled,
		ErrNoManifestEntry,
		ErrAmbiguousResolution,
		ErrConfidenceTierDowngrade,
	}
	for i, e := range sentinels {
		if e == nil {
			t.Errorf("sentinel[%d] is nil", i)
		}
	}
	for i := range sentinels {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i], sentinels[j]) || errors.Is(sentinels[j], sentinels[i]) {
				t.Errorf("sentinels %d and %d must be distinct", i, j)
			}
		}
	}
}
