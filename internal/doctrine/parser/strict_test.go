package parser

import (
	"errors"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestParseStrictRejectsUnknownTopKey(t *testing.T) {
	data := mustReadFixture(t, "invalid_unknown_top_key.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:invalid_unknown_top_key.toml", &s, ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err == nil {
		t.Fatal("expected error for unknown top-level key; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "doctrineverion") {
		t.Errorf("error message lacks unknown key name: %v", err)
	}
	if !strings.Contains(err.Error(), "test:invalid_unknown_top_key.toml") {
		t.Errorf("error message lacks source label: %v", err)
	}
}

func TestParseStrictRejectsUnknownNestedKey(t *testing.T) {
	data := mustReadFixture(t, "invalid_unknown_nested_key.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:invalid_unknown_nested_key.toml", &s, ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err == nil {
		t.Fatal("expected error for unknown nested key; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "no_tech_dept") {
		t.Errorf("error message lacks unknown nested key name: %v", err)
	}
}

func TestParseStrictRejectsTypoSection(t *testing.T) {
	data := mustReadFixture(t, "invalid_typo_section.toml")
	var s v1.Schema
	err := ParseStrict(data, "test:invalid_typo_section.toml", &s, ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err == nil {
		t.Fatal("expected error for typo section; got nil")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err not ErrParseFailed: %v", err)
	}
	// BurntSushi reports the typo'd section header itself as an unknown
	// key alongside each child; alphabetical sort places "doctrine_transvers"
	// (parent) before the dotted children. The error message MUST mention
	// the typo'd section name (which is the diagnosing label the operator
	// needs to spot the typo). Total unknown count includes parent + children.
	msg := err.Error()
	if !strings.Contains(msg, "doctrine_transvers") {
		t.Errorf("error message lacks typo'd section name: %v", err)
	}

	if !strings.Contains(msg, "total 3") {
		t.Errorf("error message lacks expected total of 3 unknown keys: %v", err)
	}
}

func TestParseStrictDeterministicUnknownKeyReport(t *testing.T) {
	data := mustReadFixture(t, "invalid_unknown_top_key.toml")
	var s1, s2 v1.Schema
	err1 := ParseStrict(data, "test:det", &s1, ParseOpts{AllowTransverseDeclaration: true})
	err2 := ParseStrict(data, "test:det", &s2, ParseOpts{AllowTransverseDeclaration: true})
	if err1 == nil || err2 == nil {
		t.Fatal("expected errors on both calls")
	}
	if err1.Error() != err2.Error() {
		t.Errorf("nondeterministic Undecoded() report:\n  call1: %v\n  call2: %v", err1, err2)
	}
}

func TestParseStrictRejectsTransverseInUserFile(t *testing.T) {
	data := mustReadFixture(t, "invalid_user_declares_transverse.toml")
	var s v1.Schema
	err := ParseStrict(data, "user:~/.config/zen-swarm/doctrines/foo.toml", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected *TransverseOverrideAttempt; got nil")
	}
	var tov *doctrineerrors.TransverseOverrideAttempt
	if !errors.As(err, &tov) {
		t.Fatalf("err is not *TransverseOverrideAttempt: %T = %v", err, err)
	}
	if tov.Source != "user:~/.config/zen-swarm/doctrines/foo.toml" {
		t.Errorf("Source = %q, want propagated from ParseStrict arg", tov.Source)
	}

	if tov.Section != "doctrine_transverse" {
		t.Errorf("Section = %q, want %q", tov.Section, "doctrine_transverse")
	}
	if !errors.Is(err, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Errorf("errors.Is(err, ErrTransverseOverrideAttempted) = false; want true")
	}
}

func TestParseStrictAcceptsTransverseInBuiltin(t *testing.T) {
	data := mustReadFixture(t, "invalid_user_declares_transverse.toml")
	var s v1.Schema
	err := ParseStrict(data, "embed:override.toml", &s, ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err != nil {
		t.Fatalf("ParseStrict with AllowTransverseDeclaration=true: %v", err)
	}

	if s.Transverse.NoStubs {
		t.Error("Transverse.NoStubs = true; want false (TOML literal)")
	}
}

func TestParseStrictTransverseErrorMessageReadable(t *testing.T) {
	data := mustReadFixture(t, "invalid_user_declares_transverse.toml")
	var s v1.Schema
	err := ParseStrict(data, "project:/repo/.zen/doctrine-override.toml", &s, ParseOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "/repo/.zen/doctrine-override.toml") {
		t.Errorf("message lacks source: %q", msg)
	}
	if !strings.Contains(msg, "inv-zen-135") {
		t.Errorf("message lacks inv-zen-135 reference: %q", msg)
	}
	if !strings.Contains(msg, "doctrine_transverse") {
		t.Errorf("message lacks section name: %q", msg)
	}
}
