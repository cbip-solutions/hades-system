// tests/adversarial/plan20_regex_dos_test.go
//
// caronte.yaml registration.
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario (spec §13.4 third bullet + master C-6 + inv-zen-268): a
// hostile caronte.yaml ships a `base_url_pattern` regex designed to
// trigger exponential backtracking (`(a+)+$`, `(a|a)*b`, deep nested
// alternation `(a(b(c(d(e))))){10}`, or simply over-long literal). The
// yaml.Load MUST refuse with ErrPatternTooLong (length gate) or
// ErrPatternRegexDoS (regexp/syntax-tree complexity gate) BEFORE the
// regex enters the manifest — never proceed to regexp.Compile of a
// hostile pattern.
//
// Adversarial corpus walks the four failure modes:
//
//   - over-MaxPatternRunes literal (length gate);
//   - classic catastrophic-backtracking `(a+)+$`;
//   - nested-repetition product over MaxPatternComplexity;
//   - alternation × repetition combined.
//
// Bite-check candidates:
//   - Raise yaml.MaxPatternRunes from 512 to 65536 → over-length input
//     escapes the length gate. The complexity guard catches some but
//     not all length-only hostile inputs.
//   - Disable the regexp/syntax probe entirely → the
//     catastrophic-backtracking cases compile + reach the manifest.

//go:build adversarial

package adversarial

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
)

func TestPlan20AdversarialRegexDoS(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	cases := []struct {
		name      string
		pattern   string
		expectAny []error
	}{
		{
			name:      "over_length_literal",
			pattern:   strings.Repeat("a", yaml.MaxPatternRunes+1),
			expectAny: []error{yaml.ErrPatternTooLong},
		},
		{
			name:      "over_length_literal_2x",
			pattern:   strings.Repeat("z", yaml.MaxPatternRunes*2),
			expectAny: []error{yaml.ErrPatternTooLong},
		},
		{
			name:      "classic_catastrophic_backtrack",
			pattern:   "^(a+)+$",
			expectAny: []error{yaml.ErrPatternRegexDoS, yaml.ErrPatternTooLong},
		},
		{
			name:      "deep_alternation_repetition_product",
			pattern:   "^(a+|b+|c+|d+|e+|f+|g+|h+|i+|j+|k+|l+|m+|n+|o+|p+|q+|r+|s+|t+)+$",
			expectAny: []error{yaml.ErrPatternRegexDoS, yaml.ErrPatternTooLong},
		},
		{
			name:      "nested_repetition_chain",
			pattern:   "^(a(b(c(d(e(f(g(h+)+)+)+)+)+)+)+)+$",
			expectAny: []error{yaml.ErrPatternRegexDoS, yaml.ErrPatternTooLong},
		},
		{

			name:      "alternation_arm_with_nested_repetition",
			pattern:   "^((a+)+|(b+)+|(c+)+|(d+)+)$",
			expectAny: []error{yaml.ErrPatternRegexDoS, yaml.ErrPatternTooLong},
		},
	}

	tmpDir := t.TempDir()
	roster := []string{"svc-a"}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yamlContent := "schema_version: 1\nservices:\n  - target_repo: \"svc-a\"\n    base_url_pattern: " + jsonEscape(c.pattern) + "\n"
			path := filepath.Join(tmpDir, "caronte-"+c.name+".yaml")
			if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}
			_, err := yaml.Load(path, roster)
			if err == nil {
				t.Errorf("plan20 adv L-10 [%s]: yaml.Load returned nil; want one of %v (pattern accepted)",
					c.name, c.expectAny)
				return
			}
			matched := false
			for _, want := range c.expectAny {
				if errors.Is(err, want) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("plan20 adv L-10 [%s]: err = %v; want errors.Is(err, X) for X ∈ %v",
					c.name, err, c.expectAny)
			}
		})
	}
}

func jsonEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
