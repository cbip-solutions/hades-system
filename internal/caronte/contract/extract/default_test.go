package extract

import (
	"reflect"
	"testing"
)

func TestDefaultReturnsNonNilSingleton(t *testing.T) {
	got := Default()
	if got == nil {
		t.Fatal("Default() returned nil; want non-nil process-global Registry")
	}
}

func TestDefaultIsSingleton(t *testing.T) {
	first := Default()
	second := Default()
	if first != second {
		t.Errorf("Default() returned distinct pointers across calls (first=%p second=%p); want singleton",
			first, second)
	}
}

func TestDefaultIsRegistry(t *testing.T) {
	d := Default()
	if got, want := reflect.TypeOf(d), reflect.TypeOf((*Registry)(nil)); got != want {
		t.Errorf("Default() returned %s; want *Registry", got)
	}
}
