package plugin

import (
	"strings"
	"testing"
)

func TestSlugDeterministic(t *testing.T) {
	path := "/Users/op/projects/myapp"
	s1 := Slug(path)
	s2 := Slug(path)
	if s1 != s2 {
		t.Errorf("Slug not deterministic: %q != %q", s1, s2)
	}
}

func TestSlugIncludesBasename(t *testing.T) {
	s := Slug("/Users/op/projects/myapp")
	if !strings.HasPrefix(s, "myapp-") {
		t.Errorf("Slug = %q, want prefix myapp-", s)
	}
}

func TestSlugDistinctForSameBasenameDifferentPath(t *testing.T) {
	s1 := Slug("/Users/op/projects/myapp")
	s2 := Slug("/Users/op/work/myapp")
	if s1 == s2 {
		t.Errorf("Slug collision for distinct paths: %q", s1)
	}
}

func TestSlugLowercaseAlphaNumDashOnly(t *testing.T) {
	s := Slug("/Users/Op/Projects/MyApp_v2!")
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			t.Errorf("Slug %q contains non-[a-z0-9-] character %q", s, r)
		}
	}
}

func TestSlugHashSuffixLength(t *testing.T) {
	s := Slug("/Users/op/projects/myapp")
	parts := strings.Split(s, "-")
	if len(parts) < 2 {
		t.Fatalf("Slug %q missing hash suffix", s)
	}
	suffix := parts[len(parts)-1]
	if len(suffix) != 8 {
		t.Errorf("Slug hash suffix len = %d, want 8 (sha256 prefix); slug=%q", len(suffix), s)
	}
}

func TestSlugAllPunctuationBasename(t *testing.T) {
	s := Slug("/Users/op/projects/!@#$")
	if !strings.HasPrefix(s, "project-") {
		t.Errorf("Slug for punctuation-only basename = %q, want prefix project-", s)
	}
}

func TestSlugEmptyPath(t *testing.T) {
	s := Slug("")
	if s == "" {
		t.Error("Slug(\"\") returned empty string")
	}
	if !strings.Contains(s, "-") {
		t.Errorf("Slug(\"\") = %q, want at least one hyphen separating basename + hash", s)
	}
}
