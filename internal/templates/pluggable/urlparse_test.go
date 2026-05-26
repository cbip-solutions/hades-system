package pluggable

import (
	"strings"
	"testing"
)

func TestParseURL_AcceptsCanonicalForms(t *testing.T) {
	cases := []struct {
		input    string
		wantHost string
		wantPath string
	}{
		{"gh:foo/bar", "github.com", "foo/bar"},
		{"gh:cbip-solutions/hades-system-templates", "github.com", "cbip-solutions/hades-system-templates"},
		{"https://github.com/foo/bar.git", "github.com", "foo/bar"},
		{"https://gitlab.com/foo/bar", "gitlab.com", "foo/bar"},
		{"git@github.com:foo/bar.git", "github.com", "foo/bar"},
		{"git@github.com:foo/bar", "github.com", "foo/bar"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got, err := ParseURL(c.input)
			if err != nil {
				t.Fatalf("ParseURL(%q): %v", c.input, err)
			}
			if got.Host != c.wantHost {
				t.Errorf("host: got %q want %q", got.Host, c.wantHost)
			}
			if got.Path != c.wantPath {
				t.Errorf("path: got %q want %q", got.Path, c.wantPath)
			}
		})
	}
}

func TestParseURL_RejectsMalformedInputs(t *testing.T) {
	bad := []string{
		"",
		"foo",
		"gh:",
		"gh:foo",
		"https://",
		"https://github.com/",
		"git@",
		"http://insecure.example.com/foo/bar",
		"file:///etc/passwd",
		"ssh://anywhere",
	}
	for _, b := range bad {
		t.Run(b, func(t *testing.T) {
			_, err := ParseURL(b)
			if err == nil {
				t.Errorf("ParseURL(%q): want error, got nil", b)
				return
			}
			if !strings.Contains(err.Error(), "template URL") {
				t.Errorf("error message %q does not mention 'template URL' hint", err.Error())
			}
		})
	}
}

func TestParseURL_NoVersionEmbedded(t *testing.T) {
	got, err := ParseURL("gh:foo/bar")
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	if got.Version != "" {
		t.Errorf("Version: got %q, want empty (no #ref in URL form)", got.Version)
	}
}

func TestParseURL_HTTPSDotGitStripped(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar.git": "foo/bar",
		"https://github.com/foo/bar":     "foo/bar",
	}
	for input, wantPath := range cases {
		got, err := ParseURL(input)
		if err != nil {
			t.Errorf("ParseURL(%q): %v", input, err)
			continue
		}
		if got.Path != wantPath {
			t.Errorf("ParseURL(%q): path=%q want %q", input, got.Path, wantPath)
		}
	}
}

func TestParseURL_CloneURLForGh(t *testing.T) {
	got, err := ParseURL("gh:foo/bar")
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	want := "https://github.com/foo/bar.git"
	if got.CloneURL != want {
		t.Errorf("CloneURL: got %q want %q", got.CloneURL, want)
	}
}
