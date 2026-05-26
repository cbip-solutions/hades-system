package semantic

import (
	"errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{ErrCGODisabled, ErrNoDispatcher, ErrBuildBroken}
	for i, e := range sentinels {
		if e == nil {
			t.Fatalf("sentinel[%d] is nil", i)
		}
	}
	if errors.Is(ErrCGODisabled, ErrNoDispatcher) ||
		errors.Is(ErrNoDispatcher, ErrBuildBroken) ||
		errors.Is(ErrCGODisabled, ErrBuildBroken) {
		t.Error("sentinels must be distinct error values")
	}
}

func TestDefaultLLMProfileIsLocalCode(t *testing.T) {
	if DefaultLLMProfile != "local-code" {
		t.Errorf("DefaultLLMProfile = %q; want \"local-code\" (spec §13, Roster C2 Ollama coder)", DefaultLLMProfile)
	}
}
