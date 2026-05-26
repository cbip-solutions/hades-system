package parser

import (
	"strings"
	"testing"
)

func TestContentHashDeterministic(t *testing.T) {
	in := "func (T) M() error { return nil }"
	a := ContentHash(in)
	b := ContentHash(in)
	if a != b {
		t.Errorf("ContentHash not deterministic: %q != %q", a, b)
	}
	if a == "" {
		t.Error("ContentHash returned empty string for non-empty input")
	}
}

func TestContentHashDiffersOnChange(t *testing.T) {
	a := ContentHash("func (T) M() error { return nil }")
	b := ContentHash("func (T) M() error { return err }")
	if a == b {
		t.Error("ContentHash collided on a changed body; edited nodes would be wrongly skipped")
	}
}

func TestContentHashEmpty(t *testing.T) {
	if got := ContentHash(""); got == "" {
		t.Error("ContentHash(\"\") returned empty; want the stable XXH64-of-empty hex")
	}
}

func TestContentHashIsHex16(t *testing.T) {
	h := ContentHash("anything")
	if len(h) != 16 {
		t.Errorf("ContentHash width = %d; want 16 (64-bit hex)", len(h))
	}
	if strings.ToLower(h) != h {
		t.Errorf("ContentHash %q not lowercase hex", h)
	}
	for _, r := range h {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("ContentHash %q has non-hex rune %q", h, r)
		}
	}
}
