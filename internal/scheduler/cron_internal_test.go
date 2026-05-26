package scheduler

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestContainsToken_BoundaryBranches(t *testing.T) {
	cases := []struct {
		name string
		s    string
		tok  string
		want bool
	}{

		{"start match with delim after", "MON 8 * * *", "MON", true},

		{"end match with delim before", "0 8 * * MON", "MON", true},

		{"left-bounded substring fail", "FOO MON", "OO", false},

		{"single-char tok preceded by digit", "5L", "L", false},

		{"single-char tok = whole string", "L", "L", true},

		{"two-char tok at end", "0 8 * JAN", "JAN", true},

		{"absent", "0 8 * * *", "MON", false},

		{"token preceded by alpha", "AMON", "MON", false},

		{"token followed by alpha", "MOND", "MON", false},

		{"first bounded, second unbounded", "MON MONDAY", "MON", true},

		{"first unbounded, second bounded", "AMON MON", "MON", true},

		{"all unbounded triggers idx>=len(s)", "AMONA AMONB AMONX", "MON", false},

		{"unbounded tok at last pos triggers idx exit", "XMONX", "MON", false},

		{"empty haystack non-empty needle", "", "MON", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsToken(tc.s, tc.tok); got != tc.want {
				t.Errorf("containsToken(%q, %q) = %v, want %v", tc.s, tc.tok, got, tc.want)
			}
		})
	}
}

func TestContainsAttachedExtension_AllBranches(t *testing.T) {
	cases := []struct {
		name string
		s    string
		ext  string
		want bool
	}{

		{"15W at end", "0 8 15W", "W", true},

		{"15W with space after", "0 8 15W *", "W", true},

		{"5L with comma after", "0 0 5L,15 * *", "L", true},

		{"L at position 0", "L * * * *", "L", false},

		{"L preceded by space", "0 8 L * *", "L", false},

		{"5LK not at boundary", "5LK", "L", false},

		{"absent", "0 8 * * *", "L", false},

		{"5W exact end", "5W", "W", true},

		{"first fails left, second succeeds", "L 5L", "L", true},

		{"empty haystack", "", "W", false},

		{"single-char ext alone", "W", "W", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsAttachedExtension(tc.s, tc.ext); got != tc.want {
				t.Errorf("containsAttachedExtension(%q, %q) = %v, want %v", tc.s, tc.ext, got, tc.want)
			}
		})
	}
}

func TestIsCronDelim(t *testing.T) {
	delims := []byte{' ', ',', '/', '-'}
	for _, c := range delims {
		if !isCronDelim(c) {
			t.Errorf("isCronDelim(%q) = false, want true", c)
		}
	}
	nonDelims := []byte{'a', 'A', '0', '9', '*', '?', '#', '\t', '\n', '_'}
	for _, c := range nonDelims {
		if isCronDelim(c) {
			t.Errorf("isCronDelim(%q) = true, want false", c)
		}
	}
}

func TestGranularityFloor_AllBranches(t *testing.T) {
	cases := []struct {
		name string
		in   doctrine.Name
		want time.Duration
	}{
		{"max-scope", doctrine.NameMaxScope, 30 * time.Second},
		{"capa-firewall", doctrine.NameCapaFirewall, 5 * time.Minute},
		{"default explicit", doctrine.NameDefault, 1 * time.Minute},
		{"unknown name", doctrine.Name("bogus"), 1 * time.Minute},
		{"empty name", doctrine.Name(""), 1 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := granularityFloor(tc.in); got != tc.want {
				t.Errorf("granularityFloor(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
