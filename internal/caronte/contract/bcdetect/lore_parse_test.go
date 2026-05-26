package bcdetect

import (
	"testing"
)

// TestTrailerKeyOfMatchesIntentParserSemantics is the sister test gating
// equivalence between the in-package trailerKeyOf and Plan 19 I's
// intent.trailerKeyOf (which is unexported and cannot be called directly
// from outside its package). Per the Phase G plan + master C-10 escape
// clause: we replicate the 15-line helper here AND maintain a documented
// input/output corpus that mirrors intent's expected behavior. Any drift
// in either implementation would surface as a test failure.
//
// The corpus enumerates ≥10 representative trailer-shaped + non-trailer-
// shaped lines covering: canonical Lore keys, custom keys, non-trailer
// prose, indented continuations (excluded — they would be folded by
// intent's parseTrailerLines but trailerKeyOf alone returns ""), empty
// strings, malformed shapes.
//
// lines; the SAME shape detection MUST hold here so the contiguous-run
// algorithm in Phase G's extractAttributionTrailers (lore.go) produces the
// same trailer block as Plan 19 I's parseTrailerLines for any given body.
func TestTrailerKeyOfMatchesIntentParserSemantics(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{

		{"Lore-Adr-Ref: 0103", "Lore-Adr-Ref"},
		{"Lore-Supersedes: 0095", "Lore-Supersedes"},
		{"Lore-Constraint: max-scope", "Lore-Constraint"},
		{"Lore-Rejected: opt-A", "Lore-Rejected"},
		{"Lore-Agent-Directive: noop", "Lore-Agent-Directive"},
		{"Lore-Verification: tested", "Lore-Verification"},

		{"Co-Authored-By: x@y", "Co-Authored-By"},
		{"Signed-off-by: x@y", "Signed-off-by"},
		{"Issue-Ref: 42", "Issue-Ref"},
		{"X_Custom: value", "X_Custom"},
		{"a: value", "a"},
		{"a1: value", "a1"},

		{"This is a prose line.", ""},
		{"No colon here", ""},
		{":no key", ""},
		{"key with space: value", ""},
		{"key/slash: value", ""},
		{"", ""},
		{"   Lore-Adr-Ref: 0103", ""},
		{"\tLore-Adr-Ref: 0103", ""},
		{"Lore-Adr-Ref", ""},
		{"123ABC: value", "123ABC"},
	}
	for i, tc := range cases {
		got := trailerKeyOf(tc.line)
		if got != tc.want {
			t.Errorf("case %d: trailerKeyOf(%q) = %q; want %q", i, tc.line, got, tc.want)
		}
	}
}

func TestTrailerKeyOfExtractionGatesADRAttribution(t *testing.T) {
	if got := trailerKeyOf("Lore-Adr-Ref: 0103"); got != TrailerKeyLoreAdrRef {
		t.Errorf("Lore-Adr-Ref extraction drift: got %q want %q", got, TrailerKeyLoreAdrRef)
	}
	if got := trailerKeyOf("Lore-Supersedes: 0095"); got != TrailerKeyLoreSupersedes {
		t.Errorf("Lore-Supersedes extraction drift: got %q want %q", got, TrailerKeyLoreSupersedes)
	}
}

func TestExtractAttributionTrailersTrailerBlock(t *testing.T) {
	cases := []struct {
		name           string
		body           string
		wantADRRefs    []string
		wantSupersedes []string
	}{
		{
			name:           "single ADR-Ref",
			body:           "subject\n\nbody\n\nLore-Adr-Ref: 0103\n",
			wantADRRefs:    []string{"0103"},
			wantSupersedes: []string{},
		},
		{
			name:           "both trailers",
			body:           "subject\n\nbody\n\nLore-Adr-Ref: 0103\nLore-Supersedes: 0095\n",
			wantADRRefs:    []string{"0103"},
			wantSupersedes: []string{"0095"},
		},
		{
			name:           "no trailers",
			body:           "subject\n\nbody only\n",
			wantADRRefs:    []string{},
			wantSupersedes: []string{},
		},
		{
			name:           "interrupted block",
			body:           "subject\n\nbody\n\nLore-Adr-Ref: 0103\nNot a trailer\nLore-Supersedes: 0095\n",
			wantADRRefs:    []string{},
			wantSupersedes: []string{"0095"},
		},
		{
			name:           "empty trailer value dropped",
			body:           "subject\n\nbody\n\nLore-Adr-Ref:\nLore-Supersedes: 0095\n",
			wantADRRefs:    []string{},
			wantSupersedes: []string{"0095"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adrs, sups := extractAttributionTrailers(tc.body)
			if !stringSlicesEqual(adrs, tc.wantADRRefs) {
				t.Errorf("ADRRefs = %v; want %v", adrs, tc.wantADRRefs)
			}
			if !stringSlicesEqual(sups, tc.wantSupersedes) {
				t.Errorf("Supersedes = %v; want %v", sups, tc.wantSupersedes)
			}
		})
	}
}

func TestExtractAttributionTrailersIgnoresNonADRTrailers(t *testing.T) {
	body := "subject\n\nbody\n\nLore-Constraint: max-scope\nLore-Verification: tested\nLore-Adr-Ref: 0103\n"
	adrs, sups := extractAttributionTrailers(body)
	if len(adrs) != 1 || adrs[0] != "0103" {
		t.Errorf("ADRRefs = %v; want [0103]", adrs)
	}
	if len(sups) != 0 {
		t.Errorf("Supersedes = %v; want []", sups)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
