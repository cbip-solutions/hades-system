package golden

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGoldenFixturesAll(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runFixture(t, name)
		})
	}
}

func TestGoldenFixturesCoverEntireMappingTable(t *testing.T) {
	t.Parallel()
	required := []string{"skill", "command", "hook", "doctrine", "memory", "mcp"}
	entries, err := os.ReadDir("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		for _, r := range required {
			if strings.Contains(name, r) {
				covered[r] = true
			}
		}

		if strings.Contains(name, "permission") {
			covered["doctrine"] = true
		}

		if strings.Contains(name, "pretooluse") || strings.Contains(name, "postllmcall") ||
			strings.Contains(name, "-bash") || strings.Contains(name, "-python") {
			covered["hook"] = true
		}
	}
	for _, r := range required {
		if !covered[r] {
			t.Errorf("no golden fixture covers EntryKind %q (add one under internal/migrate/golden/fixtures/)", r)
		}
	}
}

func TestGoldenFixturesNoClaudeAttribution(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`(?i)(co-authored-by:\s*claude|generated\s+(with|by)\s+claude(\s|$|\.)|claude\s+assistant)`)
	err := filepath.Walk("fixtures", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if re.Match(body) {
			t.Errorf("Claude attribution detected in fixture %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFileSHA256_BasicWiring(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if sum != want {
		t.Errorf("got %s, want %s", sum, want)
	}
}

func TestNormalizeFixtureBody_PlaceholderReplaced(t *testing.T) {
	t.Parallel()
	expected := []byte(`# Raw source: <SOURCE>
body`)
	actual := []byte(`# Raw source: /some/path/.claude
body`)
	got := normalizeFixtureBody(expected, actual)
	if strings.Contains(string(got), "<SOURCE>") {
		t.Errorf("placeholder not replaced: %s", got)
	}
	if !strings.Contains(string(got), "/some/path/.claude") {
		t.Errorf("missing actual source path in normalized expected: %s", got)
	}
}

func TestNormalizeFixtureBody_NoPlaceholder(t *testing.T) {
	t.Parallel()
	expected := []byte("plain expected body")
	got := normalizeFixtureBody(expected, []byte("any actual"))
	if string(got) != "plain expected body" {
		t.Errorf("no-placeholder must pass through: %s", got)
	}
}
