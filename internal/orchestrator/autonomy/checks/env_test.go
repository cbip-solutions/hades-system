package checks

import (
	"testing"
	"time"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abcdef", 3, "abc…"},
		{"abc", -1, "abc"},
		{"abc", 0, "abc"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d): want %q got %q", c.in, c.n, c.want, got)
		}
	}
}

func TestFmtAge(t *testing.T) {
	got := fmtAge("wiki", 25*time.Hour, 24*time.Hour)
	want := "wiki 25h0m0s exceeds threshold 24h0m0s"
	if got != want {
		t.Errorf("fmtAge: want %q got %q", want, got)
	}
}

func TestDeps_HTTPTimeout(t *testing.T) {

	d := Deps{}
	if got := d.httpTimeout(); got != defaultProbeTimeout {
		t.Errorf("default timeout: want %v got %v", defaultProbeTimeout, got)
	}

	d.Probes.HTTPTimeout = 50 * time.Millisecond
	if got := d.httpTimeout(); got != 50*time.Millisecond {
		t.Errorf("override timeout: want 50ms got %v", got)
	}
}
