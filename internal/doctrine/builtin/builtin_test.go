package builtin_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
)

func TestLoadAllReturnsThreeDoctrines(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v, want nil", err)
	}
	want := []string{"max-scope", "default", "capa-firewall"}
	if len(reg) != len(want) {
		t.Fatalf("LoadAll() registry size = %d, want %d", len(reg), len(want))
	}
	for _, name := range want {
		if _, ok := reg[name]; !ok {
			t.Errorf("LoadAll() registry missing %q", name)
		}
	}
}

func TestPerDoctrineAccessors(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v, want nil", err)
	}
	if got := builtin.MaxScope(); got != reg["max-scope"] {
		t.Errorf("MaxScope() pointer mismatch")
	}
	if got := builtin.Default(); got != reg["default"] {
		t.Errorf("Default() pointer mismatch")
	}
	if got := builtin.CapaFirewall(); got != reg["capa-firewall"] {
		t.Errorf("CapaFirewall() pointer mismatch")
	}
}

func TestLoadAllSchemasNonNil(t *testing.T) {
	reg, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v, want nil", err)
	}
	for name, s := range reg {
		if s == nil {
			t.Errorf("registry[%q] is nil", name)
			continue
		}
		if s.SchemaVersion != schema.CurrentSchemaVersion {
			t.Errorf("registry[%q].SchemaVersion = %q, want %q",
				name, s.SchemaVersion, schema.CurrentSchemaVersion)
		}
	}
}

func TestLoadErrorTypeWraps(t *testing.T) {
	wrapped := errors.New("simulated parse failure")
	le := &builtin.LoadError{
		Source:  "embed:max-scope.toml",
		Stage:   builtin.StageParse,
		Wrapped: wrapped,
	}
	if !errors.Is(le, wrapped) {
		t.Errorf("LoadError does not unwrap to wrapped err")
	}
	if le.Source != "embed:max-scope.toml" {
		t.Errorf("LoadError.Source = %q, want %q", le.Source, "embed:max-scope.toml")
	}
}

// TestRegistryReadOnly verifies the returned Registry is a defensive copy
// (mutating the result MUST NOT affect subsequent LoadAll() calls).
// Built-ins are immutable post-init.
func TestRegistryReadOnly(t *testing.T) {
	reg1, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() err=%v, want nil", err)
	}
	delete(reg1, "max-scope")
	reg2, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() second call err=%v, want nil", err)
	}
	if _, ok := reg2["max-scope"]; !ok {
		t.Errorf("LoadAll() returned shared registry; deletion in caller leaked")
	}
}

// TestBytes_ReturnsEmbeddedTOML verifies Bytes() returns the verbatim
// embedded TOML bytes for each canonical built-in.
// The bytes returned MUST contain a "schema_version" header field as a
// minimum-viable shape check.
func TestBytes_ReturnsEmbeddedTOML(t *testing.T) {
	for _, name := range builtin.Names() {
		data, ok := builtin.Bytes(name)
		if !ok {
			t.Errorf("Bytes(%q) returned ok=false; want true", name)
			continue
		}
		if len(data) == 0 {
			t.Errorf("Bytes(%q) returned empty []byte", name)
		}
		if !strings.Contains(string(data), "schema_version") {
			t.Errorf("Bytes(%q) does not contain 'schema_version' header", name)
		}
	}
}

func TestBytes_UnknownNameReturnsFalse(t *testing.T) {
	cases := []string{"", "max_scope", "MAX-SCOPE", "default ", "../etc/passwd"}
	for _, name := range cases {
		data, ok := builtin.Bytes(name)
		if ok {
			t.Errorf("Bytes(%q) returned ok=true; want false", name)
		}
		if data != nil {
			t.Errorf("Bytes(%q) returned non-nil data; want nil", name)
		}
	}
}
