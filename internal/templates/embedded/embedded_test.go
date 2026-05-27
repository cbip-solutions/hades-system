package embedded

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/templates"
)

func TestEmbeddedTemplatesRegistry(t *testing.T) {
	want := []string{
		"hermes-plugin-only",
		"hermes-plugin+daemon",
		"go-cli",
		"python-cli",
		"ts-saas",
		"ml-pipeline",
	}
	got := Templates()
	if len(got) != len(want) {
		t.Fatalf("Templates() count: want %d, got %d (%v)", len(want), len(got), got)
	}
	have := map[string]bool{}
	for _, name := range got {
		have[name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("embedded template %q not registered", w)
		}
	}
}

func TestTemplateLookupReturnsTypedFS(t *testing.T) {
	tmpl, err := Template("hermes-plugin-only")
	if err != nil {
		t.Fatalf("Template(hermes-plugin-only): %v", err)
	}
	if tmpl.Name() != "hermes-plugin-only" {
		t.Errorf("Name() = %q, want hermes-plugin-only", tmpl.Name())
	}
	if tmpl.FS() == nil {
		t.Error("FS() returned nil; want non-nil embed.FS subtree")
	}
}

func TestTemplateLookupUnknownReturnsError(t *testing.T) {
	_, err := Template("does-not-exist")
	if err == nil {
		t.Fatal("Template(does-not-exist): want error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Errorf("error message %q does not mention 'unknown template'", err.Error())
	}
}

func TestRegistrySeedsAllTemplates(t *testing.T) {
	r := Registry()
	got := r.Names()
	want := Templates()
	if len(got) != len(want) {
		t.Fatalf("Registry names count: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Registry order[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMaterializeProducesRenderedFiles(t *testing.T) {
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			tmpl, err := Template(name)
			if err != nil {
				t.Fatalf("Template(%q): %v", name, err)
			}
			dst := t.TempDir()
			ctx := context.Background()
			answers := templates.Answers{
				ProjectName:       "scaffold-test",
				ProjectKind:       "plugin",
				Doctrine:          "max-scope",
				AuthorName:        "tester",
				AuthorEmail:       "test@example.com",
				HermesPluginScope: "user",
			}
			if err := tmpl.Materialize(ctx, dst, answers); err != nil {
				t.Fatalf("Materialize: %v", err)
			}

			var hasFiles bool
			err = filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				hasFiles = true
				if strings.HasSuffix(path, ".tmpl") {
					t.Errorf("file %q retains .tmpl suffix (Materialize should strip)", path)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %q: %v", dst, err)
			}
			if !hasFiles {
				t.Errorf("Materialize produced no files in %q", dst)
			}
		})
	}
}

func TestMaterializeRendersProjectName(t *testing.T) {
	tmpl, err := Template("hermes-plugin-only")
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	dst := t.TempDir()
	answers := templates.Answers{
		ProjectName: "rendered-test",
		Doctrine:    "max-scope",
	}
	if err := tmpl.Materialize(context.Background(), dst, answers); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "plugin.yaml"))
	if err != nil {
		t.Fatalf("read plugin.yaml: %v", err)
	}
	if !strings.Contains(string(body), "name: rendered-test") {
		t.Errorf("plugin.yaml missing rendered ProjectName; got:\n%s", string(body))
	}
}

// TestPostGenScriptHasNoClaudeAttribution verifies invariant propagates
// to scaffolded projects: the embedded post_gen.sh files MUST NOT contain
// AI-attribution strings. The doctrine that zen-swarm commits never carry
// AI attribution propagates to every project we scaffold, by construction
// of the template fixtures.
func TestPostGenScriptHasNoClaudeAttribution(t *testing.T) {
	forbidden := []string{
		"Co-Authored-By: prohibited assistant",
		"Co-Authored-By: claude",
		"Generated with prohibited assistant",
		"generated with claude",
		"Anthropic",
	}
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			tmpl, err := Template(name)
			if err != nil {
				t.Fatalf("Template(%q): %v", name, err)
			}
			data, err := fs.ReadFile(tmpl.FS(), "post_gen.sh")
			if err != nil {
				t.Fatalf("read post_gen.sh from %q: %v", name, err)
			}
			text := string(data)
			for _, bad := range forbidden {
				if strings.Contains(text, bad) {
					t.Errorf("template %q post_gen.sh contains forbidden attribution %q (inv-zen-004)", name, bad)
				}
			}
		})
	}
}

func TestAllTemplatesIncludePluginYaml(t *testing.T) {
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			tmpl, err := Template(name)
			if err != nil {
				t.Fatalf("Template(%q): %v", name, err)
			}
			_, err = fs.Stat(tmpl.FS(), "plugin.yaml.tmpl")
			if err != nil {
				t.Errorf("template %q missing plugin.yaml.tmpl (doctrine: every scaffold is Hermes-loadable)", name)
			}
		})
	}
}

func TestEmbeddedTemplateFS_ListsExpectedFiles(t *testing.T) {
	expectedMinFiles := map[string]int{
		"hermes-plugin-only":   8,
		"hermes-plugin+daemon": 10,
		"go-cli":               8,
		"python-cli":           8,
		"ts-saas":              8,
		"ml-pipeline":          8,
	}
	for name, minN := range expectedMinFiles {
		t.Run(name, func(t *testing.T) {
			tmpl, err := Template(name)
			if err != nil {
				t.Fatalf("Template(%q): %v", name, err)
			}
			var count int
			err = fs.WalkDir(tmpl.FS(), ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					count++
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %q: %v", name, err)
			}
			if count < minN {
				t.Errorf("template %q has %d files, want at least %d (likely embed directive dropped some)", name, count, minN)
			}
		})
	}
}

func TestMaterializeMatchesGolden(t *testing.T) {
	for _, name := range Templates() {
		t.Run(name, func(t *testing.T) {
			tmpl, err := Template(name)
			if err != nil {
				t.Fatalf("Template(%q): %v", name, err)
			}
			dst := t.TempDir()
			ctx := context.Background()
			answers := templates.Answers{
				ProjectName:       "golden-test",
				ProjectKind:       "plugin",
				Doctrine:          "max-scope",
				AuthorName:        "tester",
				AuthorEmail:       "test@example.com",
				HermesPluginScope: "user",
			}
			if err := tmpl.Materialize(ctx, dst, answers); err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			got := manifestOf(t, dst)
			goldenPath := filepath.Join("testdata", name+".want.txt")
			if os.Getenv("T_REGEN") == "1" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("regenerated golden: %s", goldenPath)
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %q: %v (regenerate via T_REGEN=1 go test)", goldenPath, err)
			}
			if got != string(want) {
				t.Errorf("golden drift for %q:\nGOT:\n%s\nWANT:\n%s", name, got, string(want))
				t.Logf("regenerate via: T_REGEN=1 go test -run TestMaterializeMatchesGolden/%s ./internal/templates/embedded/", name)
			}
		})
	}
}

func manifestOf(t *testing.T, dst string) string {
	t.Helper()
	var entries []string
	err := filepath.Walk(dst, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dst, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entries = append(entries, filepath.ToSlash(rel)+"\t"+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n") + "\n"
}

func TestMetadataForReturnsHit(t *testing.T) {
	for _, name := range Templates() {
		md, ok := MetadataFor(name)
		if !ok {
			t.Errorf("MetadataFor(%q): not found", name)
			continue
		}
		if md.Name != name {
			t.Errorf("MetadataFor(%q): Name=%q", name, md.Name)
		}
		if md.Title == "" {
			t.Errorf("MetadataFor(%q): empty Title", name)
		}
	}
}

func TestMetadataForUnknownMisses(t *testing.T) {
	_, ok := MetadataFor("does-not-exist")
	if ok {
		t.Error("MetadataFor(does-not-exist): want false")
	}
}
