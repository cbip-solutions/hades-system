// tests/adversarial/plan20_caronte_yaml_inline_secret_test.go
//
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario (spec §13.4 fourth bullet + spec §6 + master C-6 +
// inv-zen-268): a hostile / careless operator commits a caronte.yaml
// that names a credential-bearing field (`password`, `token`, `api_key`,
// `secret`, `bearer`, `auth_token`, `private_key`) under any casing
// variant (snake / kebab / camel / UPPER) at any nesting position
// (top-level / nested under services[i] / nested under arbitrary
// sub-maps). yaml.Load MUST refuse with ErrInlineSecret BEFORE the
// strict-mode unmarshal would surface a generic "unknown field" error
// (defence-in-depth ordering per spec §6).
//
// This is the inv-zen-268 normative statement under the per-field
// exhaustive corpus assault. The plan-L sweep: 7 base names × 4 casing
// variants × 3 positions = ~84 hostile inputs.
//
// Bite-check: temporarily strip one variant from InlineSecretBlacklist
// (e.g., drop "api_key" → "api_key" canon no longer matches; the camel
// "apiKey" variant escapes through walkAndValidateInlineSecretsBytes
// and the strict-mode unmarshal accepts unknown-fields silently if
// they're at a nested arbitrary position). Test must surface the
// regression for THAT variant.

//go:build adversarial

package adversarial

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
)

func TestPlan20AdversarialCaronteYAMLInlineSecret(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	bases := []string{
		"password", "token", "api_key", "secret",
		"bearer", "auth_token", "private_key",
	}

	casings := []struct {
		name string
		fn   func(string) string
	}{
		{"snake", func(s string) string { return s }},
		{"kebab", snakeToKebab},
		{"camel", snakeToCamel},
		{"UPPER", upperCase},
	}

	positions := []struct {
		name string
		fn   func(field string) string
	}{
		{"top_level", func(f string) string {
			return "schema_version: 1\n" + f + ": \"REDACTED\"\nservices: []\n"
		}},
		{"in_service_entry", func(f string) string {
			return "schema_version: 1\nservices:\n  - target_repo: \"svc-a\"\n    base_url: \"https://svc-a.test/\"\n    " + f + ": \"REDACTED\"\n"
		}},
		{"in_nested_map", func(f string) string {
			return "schema_version: 1\nextra:\n  nested:\n    " + f + ": \"REDACTED\"\nservices: []\n"
		}},
	}

	tmpDir := t.TempDir()
	roster := []string{"svc-a", "svc-b"}

	type result struct {
		caseLabel string
		err       error
	}
	var failures []result

	for _, base := range bases {
		for _, c := range casings {
			for _, p := range positions {
				field := c.fn(base)
				yamlContent := p.fn(field)
				caseLabel := base + "/" + c.name + "/" + p.name
				path := filepath.Join(tmpDir, "caronte-"+sanitizeForFilename(caseLabel)+".yaml")
				if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
					t.Fatalf("[%s] write file: %v", caseLabel, err)
				}
				_, err := yaml.Load(path, roster)
				if err == nil {
					failures = append(failures, result{caseLabel: caseLabel, err: nil})
					continue
				}
				if !errors.Is(err, yaml.ErrInlineSecret) {
					failures = append(failures, result{caseLabel: caseLabel, err: err})
				}
			}
		}
	}

	if len(failures) > 0 {
		t.Errorf("plan20 adv L-9: %d (base × casing × position) variants escaped ErrInlineSecret refusal:", len(failures))
		for _, f := range failures {
			t.Errorf("  [%s] err = %v (want errors.Is ErrInlineSecret true)", f.caseLabel, f.err)
		}
	}

	want := len(bases) * len(casings) * len(positions)
	if want != 7*4*3 {
		t.Errorf("plan20 adv L-9: matrix shape = %d; want 84 (7 bases × 4 casings × 3 positions)", want)
	}
}

func snakeToKebab(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			out[i] = '-'
		} else {
			out[i] = s[i]
		}
	}
	return string(out)
}

func snakeToCamel(s string) string {
	out := make([]byte, 0, len(s))
	upNext := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			upNext = true
			continue
		}
		if upNext && c >= 'a' && c <= 'z' {
			out = append(out, c-32)
			upNext = false
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}

func upperCase(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			out[i] = c - 32
		} else {
			out[i] = c
		}
	}
	return string(out)
}

func sanitizeForFilename(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' || c == ':' {
			out[i] = '_'
		} else {
			out[i] = c
		}
	}
	return string(out)
}
