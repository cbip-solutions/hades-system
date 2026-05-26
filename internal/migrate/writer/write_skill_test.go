package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"gopkg.in/yaml.v3"
)

func TestWriteSkill_FrontmatterRendered(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "skills", "alpha", "SKILL.md")
	e := mapping.PlanEntry{
		Kind:        mapping.EntryKindSkill,
		BodyBytes:   []byte("# alpha\nbody\n"),
		Frontmatter: map[string]string{"name": "alpha", "description": "alpha skill", "keywords": "a, b", "version": "0.0.1", "license": "imported"},
	}
	if err := writeSkill(path, e); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing frontmatter open")
	}
	if !strings.Contains(s, "name: alpha\n") {
		t.Errorf("missing name field: %s", s)
	}
	if !strings.Contains(s, "---\n\n# alpha") {
		t.Errorf("missing frontmatter close + body junction: %s", s)
	}
}

func TestWriteSkill_EmptyBodyErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "skills", "alpha", "SKILL.md")
	err := writeSkill(path, mapping.PlanEntry{Kind: mapping.EntryKindSkill, BodyBytes: nil})
	if err == nil {
		t.Errorf("expected error on empty body")
	}
}

func TestWriteSkill_FrontmatterKeysSorted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "S.md")
	e := mapping.PlanEntry{
		BodyBytes:   []byte("body"),
		Frontmatter: map[string]string{"z": "Z", "a": "A", "m": "M"},
	}
	if err := writeSkill(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	idxA := strings.Index(s, "a: A")
	idxM := strings.Index(s, "m: M")
	idxZ := strings.Index(s, "z: Z")
	if idxA == -1 || idxM == -1 || idxZ == -1 {
		t.Fatalf("missing keys: %s", s)
	}
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("keys not sorted lex: %s", s)
	}
}

func TestRenderSkillFrontmatter_EmptyMap(t *testing.T) {
	t.Parallel()
	got := renderSkillFrontmatter(nil)
	if got != "" {
		t.Errorf("empty frontmatter: got %q, want empty", got)
	}
}

func TestWriteSkill_FrontmatterQuotesYAMLSpecial(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		desc string
	}{
		{"colon-in-description", "Plan 9: Audit infrastructure"},
		{"single-quote", "Operator's first skill"},
		{"double-quote", `says "hello"`},
		{"hash-in-description", "uses # hash"},
		{"leading-whitespace", "   leading spaces"},
		{"empty-fallback", ""},
		{"newline-in-value", "line one\nline two"},
		{"yaml-bool-like", "true"},
		{"yaml-null-like", "null"},
		{"only-special-chars", "[{}]:,&*!|>"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			path := filepath.Join(tmp, "SKILL.md")
			e := mapping.PlanEntry{
				Kind:      mapping.EntryKindSkill,
				BodyBytes: []byte("body\n"),
				Frontmatter: map[string]string{
					"name":        "alpha",
					"description": c.desc,
					"keywords":    "a, b",
					"version":     "0.0.1",
					"license":     "imported",
				},
			}
			if err := writeSkill(path, e); err != nil {
				t.Fatal(err)
			}
			body, _ := os.ReadFile(path)

			s := string(body)
			if !strings.HasPrefix(s, "---\n") {
				t.Fatalf("missing frontmatter open: %s", s)
			}
			rest := s[len("---\n"):]
			end := strings.Index(rest, "\n---\n")
			if end < 0 {
				t.Fatalf("missing frontmatter close: %s", s)
			}
			fm := rest[:end]
			var parsed map[string]string
			if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
				t.Fatalf("frontmatter not parseable as YAML: %v\nfm:\n%s", err, fm)
			}
			if parsed["description"] != c.desc {
				t.Errorf("description round-trip: got %q, want %q\nfm:\n%s",
					parsed["description"], c.desc, fm)
			}
		})
	}
}
