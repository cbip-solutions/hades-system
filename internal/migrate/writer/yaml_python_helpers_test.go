package writer

import (
	"strings"
	"testing"
)

func TestYAMLScalarString_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		mustHave string
	}{
		{"", `""`},
		{"plain", "plain"},
		{"with:colon", `"with:colon"`},
		{"with#hash", `"with#hash"`},
		{"with@at", `"with@at"`},
		{"with[bracket", `"with[bracket"`},
		{" leading space", `" leading space"`},
		{"trailing ", `"trailing "`},
		{"true", `"true"`},
		{"false", `"false"`},
		{"null", `"null"`},
		{"yes", `"yes"`},
		{"on", `"on"`},
		{"42", `"42"`},
		{"1.5e10", `"1.5e10"`},
		{"-42", `"-42"`},
		{"0xff", `"0xff"`},
		{"has\nnewline", `"has\nnewline"`},
		{"has\ttab", `"has\ttab"`},
		{"-leading-dash", `"-leading-dash"`},
		{"[", `"["`},
		{"]", `"]"`},
		{"{", `"{"`},
		{"}", `"}"`},
		{",", `","`},
		{"?", `"?"`},
	}
	for _, c := range cases {
		got := yamlScalarString(c.in)
		if got != c.mustHave {
			t.Errorf("yamlScalarString(%q): got %q, want %q", c.in, got, c.mustHave)
		}
	}
}

func TestYAMLMapKey_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		mustHave string
	}{
		{"", `""`},
		{"plain", "plain"},
		{"with:colon", `"with:colon"`},
		{"@-prefix", `"@-prefix"`},
	}
	for _, c := range cases {
		got := yamlMapKey(c.in)
		if got != c.mustHave {
			t.Errorf("yamlMapKey(%q): got %q, want %q", c.in, got, c.mustHave)
		}
	}
}

func TestNeedsYAMLQuote_Cases(t *testing.T) {
	t.Parallel()
	mustQuote := []string{
		"", "true", "False", "null", "yes", "no", "on", "off", "~",
		"42", "-42", "+0", "0xff", "1.5e10",
		"plain:colon", "hash#sign", "open[bracket", "close]bracket",
		"open{brace", "close}brace", "comma,here", "amp&sand",
		"star*x", "bang!x", "pipe|x", "gt>x", "single'q", "double\"q",
		"pct%x", "back`tick", "@at",
		"\nnewline", "\rcr", "\ttab", "\x01ctrl",
		" lead", "trail ", "-dash", "?ques", "[xx", "]yy", "{aa", "}bb", ",cc",
	}
	for _, s := range mustQuote {
		if !needsYAMLQuote(s) {
			t.Errorf("needsYAMLQuote(%q): expected true", s)
		}
	}
	bareOK := []string{"alpha", "Alpha_Beta", "version_1_0", "hyphen-ok", "/path/like"}
	for _, s := range bareOK {
		if needsYAMLQuote(s) {
			t.Errorf("needsYAMLQuote(%q): expected false (bare-safe)", s)
		}
	}
}

func TestIsYAMLNumericLike_Cases(t *testing.T) {
	t.Parallel()
	numericLike := []string{"42", "-42", "+0", "0.1", "1e10", "0xff", "0xFF", "1_000"}
	for _, s := range numericLike {
		if !isYAMLNumericLike(s) {
			t.Errorf("isYAMLNumericLike(%q): expected true", s)
		}
	}
	notNumeric := []string{"", "alpha", "1g", "abc", "_lead"}
	for _, s := range notNumeric {
		if isYAMLNumericLike(s) {
			t.Errorf("isYAMLNumericLike(%q): expected false", s)
		}
	}
}

func TestYAMLStringSlice_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "[]"},
		{[]string{}, "[]"},
		{[]string{"a"}, "[a]"},
		{[]string{"a", "b", "c"}, "[a, b, c]"},
		{[]string{"@playwright/mcp"}, `["@playwright/mcp"]`},
		{[]string{"--socket", "/tmp/x.sock"}, `["--socket", /tmp/x.sock]`},
	}
	for _, c := range cases {
		got := yamlStringSlice(c.in)
		if got != c.want {
			t.Errorf("yamlStringSlice(%v): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPyStringLiteral_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", `""`},
		{"plain.sh", `"plain.sh"`},
		{`with"quote`, `"with\"quote"`},
		{`back\slash`, `"back\\slash"`},
		{"with\nnewline", `"with\nnewline"`},
		{"with\rcr", `"with\rcr"`},
		{"with\ttab", `"with\ttab"`},
		{"control\x01char", `"control\x01char"`},
	}
	for _, c := range cases {
		got := pyStringLiteral(c.in)
		if got != c.want {
			t.Errorf("pyStringLiteral(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEncodeYAMLKeyValue_NoLeadingMarker(t *testing.T) {
	t.Parallel()
	got, err := encodeYAMLKeyValue("name", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got, "---") {
		t.Errorf("encodeYAMLKeyValue should not produce leading ---: %q", got)
	}
	if !strings.Contains(got, "name") {
		t.Errorf("missing key: %q", got)
	}
}
