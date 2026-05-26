package keychain

import (
	"errors"
	"testing"
)

func TestEnvVarName(t *testing.T) {
	cases := []struct {
		service string
		want    string
	}{
		{"zen-swarm/deepseek", "ZEN_KEYCHAIN_ZEN_SWARM_DEEPSEEK"},
		{"zen-swarm/google-ai", "ZEN_KEYCHAIN_ZEN_SWARM_GOOGLE_AI"},
		{"anthropic-bypass", "ZEN_KEYCHAIN_ANTHROPIC_BYPASS"},
	}
	for _, c := range cases {
		if got := envVarName(c.service); got != c.want {
			t.Errorf("envVarName(%q) = %q, want %q", c.service, got, c.want)
		}
	}
}

func TestSanitizeKeyValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean key", "sk-abc123", "sk-abc123"},
		{"trailing newline", "sk-abc123\n", "sk-abc123"},
		{"surrounding whitespace + newline", "  sk-abc123 \n", "sk-abc123"},
		{"angle-bracket paste error", "<sk-abc123>", "sk-abc123"},
		{"angle brackets + whitespace", "  <sk-abc123>\r\n", "sk-abc123"},
		{"only opening bracket — kept", "<sk-abc123", "<sk-abc123"},
		{"only closing bracket — kept", "sk-abc123>", "sk-abc123>"},
		{"empty stays empty", "", ""},
		{"only whitespace becomes empty", "   \n", ""},
		{"only brackets (no value) — stripped", "<>", ""},
	}
	for _, c := range cases {
		got := sanitizeKeyValue(c.in)
		if got != c.want {
			t.Errorf("%s: sanitizeKeyValue(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLookupFromEnv(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_DEEPSEEK", "sk-test-123")

	sec, err := SystemResolver{}.Lookup("zen-swarm/deepseek", "zen-swarm")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if string(sec.Reveal()) != "sk-test-123" {
		t.Errorf("revealed secret = %q, want sk-test-123", string(sec.Reveal()))
	}
}

func TestLookupFromEnvMissing(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	// Deliberately do NOT set ZEN_KEYCHAIN_ZEN_SWARM_ABSENT.
	_, err := SystemResolver{}.Lookup("zen-swarm/absent", "zen-swarm")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestValidateServiceNameValid(t *testing.T) {
	valid := []string{
		"zen-swarm/deepseek", "zen-swarm/google-ai", "zen-swarm/openrouter",
		"zen-swarm/anthropic-paygo", "anthropic-bypass", "a", "a-b",
	}
	for _, s := range valid {
		if err := validateServiceName(s); err != nil {
			t.Errorf("validateServiceName(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateServiceNameInvalid(t *testing.T) {
	invalid := []string{
		"",
		"zen-swarm//foo",
		"zen-swarm--foo",
		"-zen-swarm",
		"zen-swarm/",
		"zen-swarm-",
		"Zen-Swarm/foo",
		"zen-swárm",
		"zen swarm/foo",
		"zen.swarm/foo",
	}
	for _, s := range invalid {
		if err := validateServiceName(s); !errors.Is(err, ErrInvalidService) {
			t.Errorf("validateServiceName(%q) = %v, want ErrInvalidService", s, err)
		}
	}
}

func TestLookupFromEnvTrimsTrailingWhitespace(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_TRIM_TEST", "sk-test\n")
	sec, err := SystemResolver{}.Lookup("zen-swarm/trim-test", "zen-swarm")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got := string(sec.Reveal()); got != "sk-test" {
		t.Errorf("revealed = %q, want %q (trailing newline must be stripped)", got, "sk-test")
	}
}

func TestLookupReturnsWipeable(t *testing.T) {
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_KEYCHAIN_ZEN_SWARM_WIPETEST", "sk-wipe-target-1234")
	sec, err := SystemResolver{}.Lookup("zen-swarm/wipetest", "zen-swarm")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	revealed := sec.Reveal()
	if len(revealed) == 0 {
		t.Fatal("revealed buffer is empty")
	}
	sec.Wipe()
	for i, b := range revealed {
		if b != 0 {
			t.Errorf("byte %d = 0x%02x, want 0 after Wipe", i, b)
		}
	}
}
