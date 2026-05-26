package yaml

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSchemaVersionZeroIsMissing(t *testing.T) {
	if err := validateSchemaVersion(0); !errors.Is(err, ErrMissingSchemaVersion) {
		t.Errorf("validateSchemaVersion(0) = %v; want ErrMissingSchemaVersion", err)
	}
}

func TestValidateSchemaVersionV1Passes(t *testing.T) {
	if err := validateSchemaVersion(1); err != nil {
		t.Errorf("validateSchemaVersion(1) = %v; want nil", err)
	}
}

func TestValidateBaseURLExclusiveExactlyOneOK(t *testing.T) {
	cases := []Service{
		{BaseURL: "http://x", TargetRepo: "r"},
		{BaseURLEnv: "X_URL", TargetRepo: "r"},
		{BaseURLPattern: `^https?://`, TargetRepo: "r"},
	}
	for i, s := range cases {
		if err := validateBaseURLExclusive(s); err != nil {
			t.Errorf("case[%d] %+v: %v; want nil", i, s, err)
		}
	}
}

func TestValidateBaseURLExclusiveTwoOrMoreIsRefused(t *testing.T) {
	cases := []Service{
		{BaseURL: "http://x", BaseURLEnv: "X_URL", TargetRepo: "r"},
		{BaseURL: "http://x", BaseURLPattern: `^https?://`, TargetRepo: "r"},
		{BaseURLEnv: "X_URL", BaseURLPattern: `^https?://`, TargetRepo: "r"},
		{BaseURL: "http://x", BaseURLEnv: "X_URL", BaseURLPattern: `^https?://`, TargetRepo: "r"},
	}
	for i, s := range cases {
		if err := validateBaseURLExclusive(s); !errors.Is(err, ErrMultipleBaseURLVariants) {
			t.Errorf("case[%d] %+v: %v; want ErrMultipleBaseURLVariants", i, s, err)
		}
	}
}

func TestValidateBaseURLExclusiveNoneIsRefused(t *testing.T) {
	if err := validateBaseURLExclusive(Service{TargetRepo: "r"}); !errors.Is(err, ErrMultipleBaseURLVariants) {
		t.Errorf("empty: %v; want ErrMultipleBaseURLVariants", err)
	}
}

func TestValidateTargetRepoMemberOK(t *testing.T) {
	if err := validateTargetRepo("auth-svc", []string{"client-app", "auth-svc", "billing-svc"}); err != nil {
		t.Errorf("validateTargetRepo(member) = %v; want nil", err)
	}
}

func TestValidateTargetRepoNonMemberIsRefused(t *testing.T) {
	if err := validateTargetRepo("intruder", []string{"client-app", "auth-svc"}); !errors.Is(err, ErrUnknownTargetRepo) {
		t.Errorf("validateTargetRepo(non-member) = %v; want ErrUnknownTargetRepo", err)
	}
}

func TestValidateInlineSecretsSnakeKebabCamelCaseFold(t *testing.T) {
	// Each entry: a YAML field name appearing in a Service block. The walker
	// is case-insensitive + variant-normalising; every entry MUST be refused.
	bad := []string{
		"password", "PASSWORD", "Password",
		"api_key", "api-key", "apiKey", "API_KEY", "ApiKey",
		"auth_token", "auth-token", "authToken", "AUTH_TOKEN",
		"private_key", "private-key", "privateKey",
		"bearer", "BEARER",
		"token", "TOKEN", "Token",
		"secret", "SECRET", "Secret",
	}
	for _, name := range bad {
		err := validateInlineSecrets(map[string]string{name: "REDACTED"})
		if !errors.Is(err, ErrInlineSecret) {
			t.Errorf("validateInlineSecrets(%q) = %v; want ErrInlineSecret", name, err)
		}
	}
}

func TestValidateInlineSecretsAllowsBaseURL(t *testing.T) {
	ok := []string{"base_url", "base_url_env", "base_url_pattern", "target_repo", "notes", "schema_version", "services", "unresolved_policy"}
	for _, name := range ok {
		if err := validateInlineSecrets(map[string]string{name: "http://x"}); err != nil {
			t.Errorf("validateInlineSecrets(%q) = %v; want nil (architectural, not secret)", name, err)
		}
	}
}

func TestValidatePatternRunesUnder512OK(t *testing.T) {
	if err := validatePatternRunes(strings.Repeat("a", MaxPatternRunes)); err != nil {
		t.Errorf("validatePatternRunes(512 runes) = %v; want nil", err)
	}
}

func TestValidatePatternRunesOver512IsRefused(t *testing.T) {
	if err := validatePatternRunes(strings.Repeat("a", MaxPatternRunes+1)); !errors.Is(err, ErrPatternTooLong) {
		t.Errorf("validatePatternRunes(513 runes) = %v; want ErrPatternTooLong", err)
	}
}

// TestValidatePatternRegexDoSPathologicalIsRefused pins the load-bearing
// adversarial path: `^(a+)+(b+)+(c+)+(d+)+$` is the textbook ReDoS pattern
// (4 nested + alternating repetition arms). The regexp/syntax re-walk MUST
// reject it with ErrPatternRegexDoS BEFORE regexp.Compile.
func TestValidatePatternRegexDoSPathologicalIsRefused(t *testing.T) {
	pathological := []string{
		`^(a+)+(b+)+(c+)+(d+)+$`,
		`^(a+)+(a+)+(a+)+(a+)+(a+)+$`,
		`^(.*)+$`,
		`(x+x+)+y`,
	}
	for i, p := range pathological {
		if err := validatePatternRegexDoS(p); !errors.Is(err, ErrPatternRegexDoS) {
			t.Errorf("case[%d] %q = %v; want ErrPatternRegexDoS", i, p, err)
		}
	}
}

func TestValidatePatternRegexDoSSoberPatternsAccepted(t *testing.T) {
	sober := []string{
		`^https?://shipping-[a-z0-9]+\.internal/`,
		`^http://billing-svc`,
		`^https://auth\.example\.com/`,
		`^https?://(api|web)\.example\.com/v[12]/`,
	}
	for i, p := range sober {
		if err := validatePatternRegexDoS(p); err != nil {
			t.Errorf("case[%d] %q = %v; want nil (sober matcher false-positive)", i, p, err)
		}
	}
}

func TestValidatePatternRegexDoSInvalidSyntaxIsRefused(t *testing.T) {
	if err := validatePatternRegexDoS(`(`); err == nil {
		t.Errorf("validatePatternRegexDoS(unclosed group) = nil; want non-nil syntax error")
	}
}

func TestValidateUnresolvedPolicyEnum(t *testing.T) {
	for _, ok := range []UnresolvedPolicy{PolicySurface, PolicyFail, PolicySilent} {
		if err := validateUnresolvedPolicy(ok); err != nil {
			t.Errorf("validateUnresolvedPolicy(%q) = %v; want nil", ok, err)
		}
	}
	for _, bad := range []UnresolvedPolicy{"loud", "fast", "fatal", "SURFACE"} {
		if err := validateUnresolvedPolicy(bad); !errors.Is(err, ErrInvalidUnresolvedPolicy) {
			t.Errorf("validateUnresolvedPolicy(%q) = %v; want ErrInvalidUnresolvedPolicy", bad, err)
		}
	}
}
