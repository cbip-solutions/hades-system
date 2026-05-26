package check_test

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestParseSemverAccepts(t *testing.T) {
	tests := []struct {
		input string
		want  check.Version
	}{
		{"1.2.3", check.Version{Major: 1, Minor: 2, Patch: 3}},
		{"v1.2.3", check.Version{Major: 1, Minor: 2, Patch: 3}},
		{"0.13.0", check.Version{Major: 0, Minor: 13, Patch: 0}},
		{"0.0.0", check.Version{Major: 0, Minor: 0, Patch: 0}},
		{"100.200.300", check.Version{Major: 100, Minor: 200, Patch: 300}},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := check.ParseSemver(tc.input)
			if err != nil {
				t.Fatalf("ParseSemver(%q): unexpected error %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseSemver(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSemverRejects(t *testing.T) {
	tests := []string{
		"",
		"abc",
		"1.2",
		"1.2.3.4",
		"..",
		"1..3",
		"1.x.3",
		"1.2.3-pre",
		"v",
		"v.",
		" 1.2.3",
		"1.2.3 ",
	}
	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			_, err := check.ParseSemver(tc)
			if err == nil {
				t.Errorf("ParseSemver(%q): want error, got nil", tc)
			}
			if !errors.Is(err, check.ErrInvalidSemver) {
				t.Errorf("ParseSemver(%q): err not ErrInvalidSemver chain: %v", tc, err)
			}
		})
	}
}

func TestParseSemverErrorMessageCarriesInput(t *testing.T) {
	_, err := check.ParseSemver("garbage")
	if err == nil {
		t.Fatalf("ParseSemver(garbage): want error, got nil")
	}

	if want := "garbage"; !contains(err.Error(), want) {
		t.Errorf("error %q does not contain rejected input %q", err.Error(), want)
	}
}

func TestParseSemverLaxAccepts(t *testing.T) {
	tests := []struct {
		input string
		want  check.Version
	}{
		{"1.2.3", check.Version{Major: 1, Minor: 2, Patch: 3}},
		{"v1.2.3", check.Version{Major: 1, Minor: 2, Patch: 3}},
		{"1.2.3-pre", check.Version{Major: 1, Minor: 2, Patch: 3, PreRelease: "-pre"}},
		{"1.2", check.Version{Major: 1, Minor: 2, Patch: 0}},
		{"1", check.Version{Major: 1, Minor: 0, Patch: 0}},
		{"1.2.3.4", check.Version{Major: 1, Minor: 2, Patch: 3}},
		{"", check.Version{}},
		{"abc", check.Version{Major: 0}},

		{"1.2.3-rc.1", check.Version{Major: 1, Minor: 2, Patch: 3, PreRelease: "-rc"}},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := check.ParseSemverLax(tc.input)
			if got != tc.want {
				t.Errorf("ParseSemverLax(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b check.Version
		want int
	}{

		{check.Version{1, 2, 3, ""}, check.Version{1, 2, 3, ""}, 0},
		{check.Version{0, 0, 0, ""}, check.Version{0, 0, 0, ""}, 0},

		{check.Version{1, 2, 3, "-pre"}, check.Version{1, 2, 3, ""}, 0},
		{check.Version{1, 2, 3, ""}, check.Version{1, 2, 3, "-rc.1"}, 0},

		{check.Version{1, 0, 0, ""}, check.Version{2, 0, 0, ""}, -1},
		{check.Version{2, 0, 0, ""}, check.Version{1, 99, 99, ""}, 1},

		{check.Version{1, 2, 0, ""}, check.Version{1, 3, 0, ""}, -1},
		{check.Version{1, 5, 0, ""}, check.Version{1, 4, 99, ""}, 1},

		{check.Version{1, 2, 3, ""}, check.Version{1, 2, 4, ""}, -1},
		{check.Version{1, 2, 5, ""}, check.Version{1, 2, 4, ""}, 1},

		{mustParse(t, "0.12.5"), mustParse(t, "0.13.0"), -1},
		{mustParse(t, "0.13.0"), mustParse(t, "0.13.0"), 0},
		{mustParse(t, "0.14.2"), mustParse(t, "0.13.0"), 1},

		{check.ParseSemverLax("1.40.0"), check.ParseSemverLax("1.45.0"), -1},
		{check.ParseSemverLax("1.50.0"), check.ParseSemverLax("1.45.0"), 1},
	}
	for _, tc := range tests {
		got := check.CompareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("CompareVersions(%+v, %+v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCompareVersionsHermesContract(t *testing.T) {
	min := mustParse(t, "0.13.0")
	tests := []struct {
		version string
		wantCmp int
	}{
		{"0.12.0", -1},
		{"0.12.5", -1},
		{"0.13.0", 0},
		{"0.13.1", 1},
		{"0.14.2", 1},
		{"1.0.0", 1},
	}
	for _, tc := range tests {
		v, err := check.ParseSemver(tc.version)
		if err != nil {
			t.Fatalf("ParseSemver(%q): %v", tc.version, err)
		}
		got := check.CompareVersions(v, min)
		if (got < 0) != (tc.wantCmp < 0) || (got > 0) != (tc.wantCmp > 0) || (got == 0) != (tc.wantCmp == 0) {
			t.Errorf("Compare(%q, 0.13.0) = %d; want sign(%d)", tc.version, got, tc.wantCmp)
		}
	}
}

func mustParse(t *testing.T, s string) check.Version {
	t.Helper()
	v, err := check.ParseSemver(s)
	if err != nil {
		t.Fatalf("mustParse(%q): %v", s, err)
	}
	return v
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
