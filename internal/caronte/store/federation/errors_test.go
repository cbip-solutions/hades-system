package federation

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrCGODisabled, ErrEmptyDB, ErrEmptyStatePath, ErrNotFound,
		ErrUnknownEventType, ErrCorruptAuditLeaf,
	}
	for i, e := range sentinels {
		if e == nil {
			t.Fatalf("sentinel[%d] is nil", i)
		}
	}
	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i], sentinels[j]) {
				t.Errorf("sentinels[%d] and sentinels[%d] are aliased", i, j)
			}
		}
	}
}

func TestDefaultDriverAliasesStore(t *testing.T) {
	if DefaultDriver != store.DefaultDriver {
		t.Errorf("DefaultDriver = %q; want %q (alias of store.DefaultDriver)", DefaultDriver, store.DefaultDriver)
	}
	if DefaultDriver != "sqlite3" {
		t.Errorf("DefaultDriver = %q; want \"sqlite3\" (mattn CGO driver)", DefaultDriver)
	}
}
