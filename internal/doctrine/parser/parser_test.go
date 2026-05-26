package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestExtractSchemaVersionHappy(t *testing.T) {
	data := mustReadFixture(t, "valid_max_scope_minimal.toml")
	v, err := ExtractSchemaVersion(data)
	if err != nil {
		t.Fatalf("ExtractSchemaVersion: %v", err)
	}
	if v != "1.0" {
		t.Errorf("schema_version = %q, want %q", v, "1.0")
	}
}

func TestExtractSchemaVersionMissing(t *testing.T) {
	data := mustReadFixture(t, "invalid_schema_version_missing.toml")
	v, err := ExtractSchemaVersion(data)
	if err != nil {
		t.Fatalf("unexpected error for missing schema_version: %v", err)
	}
	if v != "" {
		t.Errorf("schema_version = %q, want empty string for absent field", v)
	}
}

func TestExtractSchemaVersionTooOld(t *testing.T) {
	data := mustReadFixture(t, "invalid_schema_version_too_old.toml")
	v, err := ExtractSchemaVersion(data)
	if err != nil {
		t.Fatalf("ExtractSchemaVersion: %v", err)
	}
	if v != "0.5" {
		t.Errorf("schema_version = %q, want literal %q (parser is policy-free)", v, "0.5")
	}
}

func TestExtractSchemaVersionNMinus1(t *testing.T) {
	data := mustReadFixture(t, "valid_schema_version_n_minus_1.toml")
	v, err := ExtractSchemaVersion(data)
	if err != nil {
		t.Fatalf("ExtractSchemaVersion: %v", err)
	}
	if v != "0.9" {
		t.Errorf("schema_version = %q, want %q", v, "0.9")
	}
}

func TestExtractSchemaVersionMalformedTOML(t *testing.T) {
	data := []byte("schema_version = \"unclosed\n")
	_, err := ExtractSchemaVersion(data)
	if err == nil {
		t.Fatal("expected error for malformed TOML; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err is not ErrParseFailed: %v", err)
	}
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return b
}

func TestParseStrictHappyMinimal(t *testing.T) {
	data := mustReadFixture(t, "valid_max_scope_minimal.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:valid_max_scope_minimal.toml", &s, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseStrict: %v", err)
	}
	if s.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", s.SchemaVersion, "1.0")
	}
	if s.DoctrineVersion != "1.0.0" {
		t.Errorf("DoctrineVersion = %q, want %q", s.DoctrineVersion, "1.0.0")
	}
}

func TestParseStrictAllowsTransverseInBuiltin(t *testing.T) {
	data := mustReadFixture(t, "valid_max_scope_with_transverse.toml")
	var s v1.Schema
	err := ParseStrict(data, "embed:max-scope.toml", &s, ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err != nil {
		t.Fatalf("ParseStrict on built-in flavor: %v", err)
	}
	if !s.Transverse.NoTechDebt {
		t.Error("Transverse.NoTechDebt = false; want true (TOML literal)")
	}
	if !s.Transverse.NoStubs {
		t.Error("Transverse.NoStubs = false; want true")
	}
	if !s.Transverse.BuildFinalProduct {
		t.Error("Transverse.BuildFinalProduct = false; want true")
	}
	if !s.Transverse.NoDefer {
		t.Error("Transverse.NoDefer = false; want true")
	}
}

func TestParseStrictTargetNilReturnsError(t *testing.T) {
	data := mustReadFixture(t, "valid_max_scope_minimal.toml")
	err := ParseStrict(data, "test:nil_target", nil, ParseOpts{})
	if err == nil {
		t.Fatal("expected error for nil target; got nil")
	}
}

func TestParseStrictEmptyData(t *testing.T) {
	var s v1.Schema
	err := ParseStrict(nil, "test:empty", &s, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseStrict on empty input: unexpected error %v", err)
	}
	if s.SchemaVersion != "" {
		t.Errorf("SchemaVersion = %q, want empty for empty input", s.SchemaVersion)
	}
}

func TestParseStrictSyntaxErrorLineCol(t *testing.T) {
	data := mustReadFixture(t, "invalid_syntax_unclosed_string.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:invalid_syntax_unclosed_string.toml", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected error for syntax violation; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}
	msg := err.Error()

	if !strings.Contains(msg, ":2:") {
		t.Errorf("error message lacks line:2 marker: %q", msg)
	}
	if !strings.Contains(msg, "test:invalid_syntax_unclosed_string.toml") {
		t.Errorf("error message lacks source label: %q", msg)
	}
}

func TestParseStrictSyntaxErrorPreservesUnwrap(t *testing.T) {
	data := mustReadFixture(t, "invalid_syntax_unclosed_string.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:unwrap", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("errors.Is(err, ErrParseFailed) = false; want true")
	}

	if u := errors.Unwrap(err); u == nil {
		t.Error("errors.Unwrap(err) returned nil; expected at least one wrap layer")
	}
}

func TestParseStrictAcceptsBOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	body := []byte("schema_version = \"1.0\"\ndoctrine_version = \"1.0.0\"\n")
	data := append(bom, body...)
	var s v1.Schema
	err := ParseStrict(data, "test:bom", &s, ParseOpts{})
	if err != nil {
		t.Fatalf("ParseStrict on BOM-prefixed input: %v", err)
	}
	if s.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", s.SchemaVersion, "1.0")
	}
}

func TestParseStrictUnknownKeyMessageMentionsCount(t *testing.T) {
	data := []byte(`schema_version = "1.0"
doctrine_version = "1.0.0"
typo_one = "x"
typo_two = "y"
typo_three = "z"
`)
	var s v1.Schema
	err := ParseStrict(data, "test:multi_typo", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "total 3") {
		t.Errorf("error message lacks count of 3 unknown keys: %v", err)
	}
}

func TestExtractSchemaVersionPropagatesSourceLabel(t *testing.T) {
	data := []byte("schema_version = \"unclosed\n")
	_, err := ExtractSchemaVersion(data)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "<unknown>") {
		t.Errorf("ExtractSchemaVersion error lacks <unknown> source sentinel: %v", err)
	}
}

func TestWrapParseErrorNonParseErrorFallback(t *testing.T) {

	data := []byte("schema_version = 12345\n")
	var s v1.Schema
	err := ParseStrict(data, "test:type_mismatch", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}

	if !strings.Contains(err.Error(), "test:type_mismatch") {
		t.Errorf("error message lacks source label: %v", err)
	}
}
