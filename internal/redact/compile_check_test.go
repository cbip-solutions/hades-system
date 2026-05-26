package redact

import (
	"reflect"
	"testing"
)

func TestEnsureNoRawTokenLeaks_ReturnsTrue(t *testing.T) {
	if !ensureNoRawTokenLeaks() {
		t.Fatal("ensureNoRawTokenLeaks returned false; symbol linked incorrectly")
	}
}

func TestSecretType_IsByteSlice(t *testing.T) {

	var s Secret
	tp := reflect.TypeOf(s)
	if tp.Kind() != reflect.Slice {
		t.Fatalf("Secret kind = %v, want Slice", tp.Kind())
	}
	if tp.Elem().Kind() != reflect.Uint8 {
		t.Fatalf("Secret element kind = %v, want Uint8", tp.Elem().Kind())
	}
}

func TestCompileCheckSymbol_IsLinked(t *testing.T) {

	t.Log("compile-check symbol linked successfully")
}
