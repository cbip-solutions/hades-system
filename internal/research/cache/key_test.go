//go:build cgo
// +build cgo

package cache

import (
	"strings"
	"testing"
)

func TestCanonicalizeQueryTrimWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"leading space", " hello", "hello"},
		{"trailing space", "hello ", "hello"},
		{"both sides", "  hello  ", "hello"},
		{"tabs", "\thello\t", "hello"},
		{"newline", "\nhello\n", "hello"},
		{"nbsp", " hello ", "hello"},
		{"mixed", " \t\nhello\n\t ", "hello"},
		{"empty", "", ""},
		{"only spaces", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalizeQuery(tc.input)
			if got != tc.want {
				t.Errorf("CanonicalizeQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeQueryLowercase(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"ascii uppercase", "Hello World", "hello world"},
		{"all caps", "QUANTUM COMPUTING", "quantum computing"},
		{"mixed case", "QuAnTuM", "quantum"},
		{"already lower", "quantum", "quantum"},
		{"unicode uppercase", "ÜBER", "über"},
		{"unicode mixed", "Ångström", "ångström"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalizeQuery(tc.input)
			if got != tc.want {
				t.Errorf("CanonicalizeQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeQueryNFC(t *testing.T) {

	decomposed := "é"

	precomposed := "é"

	canonical := CanonicalizeQuery(decomposed)
	if canonical != precomposed {
		t.Errorf("NFC normalization failed: CanonicalizeQuery(%q) = %q, want %q",
			decomposed, canonical, precomposed)
	}

	if CanonicalizeQuery(decomposed) != CanonicalizeQuery(precomposed) {
		t.Errorf("decomposed and precomposed forms canonicalize differently")
	}
}

func TestCanonicalizeQueryCollapseInternalWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"double space", "hello  world", "hello world"},
		{"triple space", "hello   world", "hello world"},
		{"tab between words", "hello\tworld", "hello world"},
		{"newline between words", "hello\nworld", "hello world"},
		{"mixed whitespace", "a  \t  b  \n  c", "a b c"},
		{"single space preserved", "hello world", "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalizeQuery(tc.input)
			if got != tc.want {
				t.Errorf("CanonicalizeQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeQueryIdempotent(t *testing.T) {
	inputs := []string{
		"Hello World",
		"  QUANTUM   computing  ",
		"é",
		"ÜBER  alles",
		"\t\nhello\n\t",
		"a  b  c",
		"",
		"already canonical",
	}
	for _, in := range inputs {
		once := CanonicalizeQuery(in)
		twice := CanonicalizeQuery(once)
		if once != twice {
			t.Errorf("not idempotent for %q: first=%q, second=%q", in, once, twice)
		}
	}
}

func TestComputeQueryHashStable(t *testing.T) {
	inputs := []string{
		"quantum computing",
		"  Quantum  Computing  ",
		"ÜBER",
		"",
		"hello world",
	}
	for _, in := range inputs {
		h1 := ComputeQueryHash(in)
		h2 := ComputeQueryHash(in)
		if h1 != h2 {
			t.Errorf("hash not stable for %q: got %q then %q", in, h1, h2)
		}

		if len(h1) != 64 {
			t.Errorf("ComputeQueryHash(%q) = %q, want 64-char hex, got len=%d", in, h1, len(h1))
		}
		if strings.ToLower(h1) != h1 {
			t.Errorf("hash %q is not lowercase hex", h1)
		}
	}
}

func TestComputeQueryHashUsesCanonical(t *testing.T) {
	pairs := []struct {
		a, b string
	}{
		{"hello world", "  HELLO   WORLD  "},
		{"quantum computing", "QUANTUM\t\tCOMPUTING"},
		{"über", "ÜBER"},
		{"é", "é"},
		{"  leading", "leading"},
	}
	for _, p := range pairs {
		ha := ComputeQueryHash(p.a)
		hb := ComputeQueryHash(p.b)
		if ha != hb {
			t.Errorf("ComputeQueryHash(%q) = %q != ComputeQueryHash(%q) = %q; expected equal after canonicalization",
				p.a, ha, p.b, hb)
		}
	}
}

func TestComputeQueryHashDistinctForDistinctSemantic(t *testing.T) {
	distinct := []string{
		"quantum computing",
		"classical computing",
		"machine learning",
		"deep learning",
		"reinforcement learning",
		"natural language processing",
		"computer vision",
		"robotics",
	}
	seen := make(map[string]string)
	for _, q := range distinct {
		h := ComputeQueryHash(q)
		if prev, ok := seen[h]; ok {
			t.Errorf("hash collision: %q and %q both hash to %q", prev, q, h)
		}
		seen[h] = q
	}
}

func TestCollapseWhitespaceInternal(t *testing.T) {

	cases := []struct {
		input string
		want  string
	}{
		{"a  b", "a b"},
		{"a   b   c", "a b c"},
		{"a\tb", "a b"},
		{"a\nb", "a b"},
		{"a \t b", "a b"},
		{"singleword", "singleword"},
		{"a b", "a b"},
	}
	for _, tc := range cases {
		got := CanonicalizeQuery(tc.input)
		if got != tc.want {
			t.Errorf("collapseWhitespace via CanonicalizeQuery(%q) = %q, want %q",
				tc.input, got, tc.want)
		}
	}
}
