package mapping

import "testing"

func TestGenerateFrontmatter_Fields(t *testing.T) {
	t.Parallel()
	body := []byte("# research-cheap\n\nA cheap research helper.\n")
	fm := generateFrontmatter("research-cheap", body)
	if fm["name"] != "research-cheap" {
		t.Errorf("name: %s", fm["name"])
	}
	if fm["description"] != "research-cheap" {
		t.Errorf("description: %s", fm["description"])
	}
	if fm["version"] != "0.0.1" {
		t.Errorf("version: %s", fm["version"])
	}
	if fm["license"] != "imported" {
		t.Errorf("license: %s", fm["license"])
	}
	if fm["keywords"] == "" {
		t.Errorf("keywords empty")
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"# Hello", "Hello"},
		{"## Subhead\nbody", "Subhead"},
		{"plain", "plain"},
		{"\n\n# Eventually", "Eventually"},
		{"", ""},
		{"### Many hash", "Many hash"},
		{"#  spaced", "spaced"},
		{"#", ""},
		{"#  #  ", "#"},
	}
	for _, c := range cases {
		got := firstLine([]byte(c.in))
		if got != c.want {
			t.Errorf("firstLine(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizePyIdent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"hello-world", "hello_world"},
		{"hello_world", "hello_world"},
		{"123abc", "_123abc"},
		{"pre.tool.call", "pre_tool_call"},
		{"", "_"},
		{"FOO", "FOO"},
		{"a1b2", "a1b2"},
	}
	for _, c := range cases {
		got := sanitizePyIdent(c.in)
		if got != c.want {
			t.Errorf("sanitizePyIdent(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGenerateFrontmatter_KeywordsDeterministic(t *testing.T) {
	t.Parallel()
	body := []byte("# alpha skill\n\nThis skill performs deterministic operations on data.")
	fm1 := generateFrontmatter("alpha", body)
	fm2 := generateFrontmatter("alpha", body)
	if fm1["keywords"] != fm2["keywords"] {
		t.Errorf("non-deterministic keywords:\n%q\n%q", fm1["keywords"], fm2["keywords"])
	}
}
