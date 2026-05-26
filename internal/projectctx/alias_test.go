package projectctx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAliasStringMatchesValue(t *testing.T) {
	a := Alias("internal-platform-x")
	if a.String() != "internal-platform-x" {
		t.Errorf("String = %q, want internal-platform-x", a.String())
	}
}

func TestAliasValidateValid(t *testing.T) {
	cases := []string{
		"internal-platform-x",
		"zen-swarm",
		"a",
		"my.project",
		"Test_123",
		"x" + strings.Repeat("y", 63),
	}
	for _, c := range cases {
		if err := Alias(c).Validate(); err != nil {
			t.Errorf("Validate(%q): %v", c, err)
		}
	}
}

func TestAliasValidateInvalid(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantSentinel error
	}{
		{"empty", "", ErrAliasEmpty},
		{"too-long", strings.Repeat("x", 65), ErrAliasTooLong},
		{"contains-space", "my project", ErrAliasInvalidChar},
		{"contains-slash", "my/project", ErrAliasInvalidChar},
		{"contains-dollar", "my$project", ErrAliasInvalidChar},
		{"contains-backtick", "my`project", ErrAliasInvalidChar},
		{"contains-semicolon", "my;project", ErrAliasInvalidChar},
		{"reserved-archived", "__archived", ErrAliasReserved},
		{"reserved-deleted", "__deleted", ErrAliasReserved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Alias(tc.input).Validate()
			if err == nil {
				t.Fatalf("Validate(%q): expected error", tc.input)
			}
			if !errors.Is(err, ErrAliasInvalid) {
				t.Errorf("Validate(%q): expected umbrella ErrAliasInvalid, got %v", tc.input, err)
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Errorf("Validate(%q): expected specific %v in chain, got %v", tc.input, tc.wantSentinel, err)
			}
		})
	}
}

func TestAliasValidateRejectsUnicode(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"latin-supplement", "café"},
		{"chinese", "项目"},
		{"emoji", "proj-🚀"},
		{"latin-with-accent-cap", "PROJÉCT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Alias(tc.input).Validate()
			if err == nil {
				t.Errorf("Validate(%q): expected error for non-ASCII alias", tc.input)
			}
			if err != nil && !errors.Is(err, ErrAliasInvalidChar) {
				t.Errorf("Validate(%q): expected ErrAliasInvalidChar, got %v", tc.input, err)
			}
		})
	}
}

func TestResolveAliasFromTOML(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`[project]
id = "my-project"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if got != "my-project" {
		t.Errorf("got %q, want my-project", got)
	}
}

func TestResolveAliasFallbackWhenNoTOML(t *testing.T) {
	dir := t.TempDir()

	id, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	want := filepath.Base(dir) + "-" + id.Short()
	got, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if got != Alias(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveAliasFallbackWhenTOMLMissingProjectID(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`# no [project] table
[unrelated]
key = "value"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	id, _ := ResolveProjectID(dir)
	want := Alias(filepath.Base(dir) + "-" + id.Short())
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveAliasFallbackWhenTOMLProjectMissingID(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`[project]
# id intentionally missing
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	id, _ := ResolveProjectID(dir)
	want := Alias(filepath.Base(dir) + "-" + id.Short())
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveAliasMalformedTOMLReturnsErr(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`this is = not valid toml = at all
because = of multiple equals signs
[project]
id = "broken
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ResolveAlias(dir)
	if err == nil {
		t.Error("expected error for malformed TOML")
	}
	if !errors.Is(err, ErrZenswarmTOMLMalformed) {
		t.Errorf("expected ErrZenswarmTOMLMalformed, got %v", err)
	}
}

func TestResolveAliasInvalidIDInTOMLReturnsErr(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`[project]
id = "has spaces"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ResolveAlias(dir)
	if err == nil {
		t.Error("expected error for invalid alias")
	}
	if !errors.Is(err, ErrAliasInvalid) {
		t.Errorf("expected ErrAliasInvalid, got %v", err)
	}
}

func TestResolveAliasNonexistentDirErrs(t *testing.T) {
	_, err := ResolveAlias("/nonexistent/zen-swarm-test-dir/never-existed")
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestResolveAliasFallbackDeterministic(t *testing.T) {
	dir := t.TempDir()
	a1, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	a2, err := ResolveAlias(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a1 != a2 {
		t.Errorf("nondeterministic: %s vs %s", a1, a2)
	}
}

func TestComputeFallbackAliasForKnownInputs(t *testing.T) {
	id := ProjectID("abcdef0123456789" + strings.Repeat("0", 48))
	got := computeFallbackAlias("internal-platform-x-project", id)
	want := Alias("internal-platform-x-project-abcdef01")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeFallbackAliasSanitizesNonAliasChars(t *testing.T) {
	id := ProjectID("abcdef0123456789" + strings.Repeat("0", 48))
	cases := []struct {
		name    string
		dirname string
		want    Alias
	}{
		{"space", "my project", "my-project-abcdef01"},
		{"slash", "a/b", "a-b-abcdef01"},
		{"dollar", "x$y", "x-y-abcdef01"},
		{"mixed", "a b/c$d", "a-b-c-d-abcdef01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeFallbackAlias(tc.dirname, id)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveAliasReadErrorNonNotExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test relies on file mode bits; root bypasses them")
	}
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	if err := os.WriteFile(tomlPath, []byte(`[project]
id = "ok"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(tomlPath, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(tomlPath, 0o644) })

	_, err := ResolveAlias(dir)
	if err == nil {
		t.Fatal("expected read error, got nil")
	}

	if errors.Is(err, ErrZenswarmTOMLMalformed) {
		t.Errorf("did not expect ErrZenswarmTOMLMalformed; got %v", err)
	}

	if !strings.Contains(err.Error(), "zenswarm.toml") {
		t.Errorf("error message does not mention zenswarm.toml: %v", err)
	}
}
