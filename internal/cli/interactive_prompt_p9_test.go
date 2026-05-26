package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptYN_Grid(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"YeS\n", true},
		{"n\n", false},
		{"N\n", false},
		{"no\n", false},
		{"NO\n", false},
		{"\n", false},
		{"", false},
		{"maybe\n", false},
		{"  y  \n", true},
	}
	for _, tc := range cases {
		tc := tc
		label := strings.TrimSpace(tc.input)
		if label == "" {
			label = "(empty/EOF)"
		}
		t.Run("input="+label, func(t *testing.T) {
			got, err := promptYN(strings.NewReader(tc.input), &bytes.Buffer{}, "Continue?")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("input %q: want %v, got %v", tc.input, tc.want, got)
			}
		})
	}
}

func TestPromptString_Grid(t *testing.T) {
	cases := []struct {
		label string
		input string
		want  string
	}{
		{"simple", "hello\n", "hello"},
		{"trim_whitespace", "  spaces  \n", "spaces"},
		{"empty_line", "\n", ""},
		{"first_line_only", "line1\nline2\n", "line1"},
		{"eof_no_newline", "abc", "abc"},
		{"empty_eof", "", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			got, err := promptString(strings.NewReader(tc.input), &bytes.Buffer{}, "Field")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("input %q: want %q, got %q", tc.input, tc.want, got)
			}
		})
	}
}

func TestPromptYN_Grid_WritesPromptToOut(t *testing.T) {
	out := &bytes.Buffer{}
	_, _ = promptYN(strings.NewReader("y\n"), out, "Test prompt")
	got := out.String()
	if !strings.Contains(got, "Test prompt") {
		t.Errorf("prompt text missing from output: %q", got)
	}
	if !strings.Contains(got, "[y/N]") {
		t.Errorf("[y/N] indicator missing from output: %q", got)
	}
}

func TestPromptString_WritesPromptToOut(t *testing.T) {
	out := &bytes.Buffer{}
	_, _ = promptString(strings.NewReader("value\n"), out, "Enter field")
	got := out.String()
	if !strings.Contains(got, "Enter field") {
		t.Errorf("prompt text missing from output: %q", got)
	}
}
