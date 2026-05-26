package pyident

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestFromName_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello-world", "hello_world"},
		{"123abc", "_123abc"},
		{"pre.tool.call", "pre_tool_call"},
		{"", "_"},
		{"a-b-c", "a_b_c"},
		{"____", "____"},
		{"AlphaBeta", "AlphaBeta"},
		{"-leading-dash", "_leading_dash"},
		{".leading-dot", "_leading_dot"},
		{"trailing-", "trailing_"},
		{"with space", "with_space"},
		{"a/b/c", "a_b_c"},
	}
	for _, c := range cases {
		got := FromName(c.in)
		if got != c.want {
			t.Errorf("FromName(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFromName_Idempotent(t *testing.T) {
	t.Parallel()
	cases := []string{"", "x", "hello-world", "123abc", "pre.tool.call", "_____", "ABC"}
	for _, c := range cases {
		once := FromName(c)
		twice := FromName(once)
		if once != twice {
			t.Errorf("not idempotent: FromName(%q) = %q, FromName(%q) = %q",
				c, once, once, twice)
		}
	}
}

func TestFromName_ValidPyIdentifier(t *testing.T) {
	t.Parallel()
	f := func(s string) bool {
		id := FromName(s)
		if id == "" {
			return false
		}
		if !isPyIdentStart(rune(id[0])) {
			return false
		}
		for _, r := range id {
			if !isPyIdentCont(r) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Errorf("property failure: %v", err)
	}
}

func TestFromName_NoForbiddenChars(t *testing.T) {
	t.Parallel()
	hostile := "x:y;z.q-r/s\\t!u#v"
	got := FromName(hostile)
	for _, ch := range []string{":", ";", ".", "-", "/", "\\", "!", "#", " "} {
		if strings.Contains(got, ch) {
			t.Errorf("forbidden char %q survived: input %q -> %q", ch, hostile, got)
		}
	}
}

func isPyIdentStart(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func isPyIdentCont(r rune) bool {
	return isPyIdentStart(r) || (r >= '0' && r <= '9')
}
