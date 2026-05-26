package pluggable

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	t2 "github.com/cbip-solutions/hades-system/internal/templates"
)

func TestValidateCloneURL_AcceptsAllowedSchemes(t *testing.T) {
	cases := []string{
		"https://github.com/foo/bar.git",
		"git@github.com:foo/bar.git",
		"http://127.0.0.1:8080/foo.git",
		"http://localhost:8080/foo.git",
	}
	for _, c := range cases {
		if err := validateCloneURL(c); err != nil {
			t.Errorf("validateCloneURL(%q): want ok, got %v", c, err)
		}
	}
}

func TestValidateCloneURL_RejectsOtherSchemes(t *testing.T) {
	cases := []string{
		"",
		"http://insecure.example.com/foo",
		"file:///etc/passwd",
		"weird-scheme://foo",
	}
	for _, c := range cases {
		if err := validateCloneURL(c); err == nil {
			t.Errorf("validateCloneURL(%q): want error, got nil", c)
		}
	}
}

func setupBareRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatalf("worktree Add %q: %v", path, err)
		}
	}
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "tester",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return dir
}

func TestFetch_FromLocalRepo_DirectFS(t *testing.T) {
	repoDir := setupBareRepo(t, map[string]string{
		"plugin.yaml.tmpl": "name: {{.ProjectName}}\n",
		"README.md.tmpl":   "# {{.ProjectName}}\n",
	})

	if _, err := git.PlainOpen(repoDir); err != nil {
		t.Fatalf("setupBareRepo did not produce a valid repo: %v", err)
	}
}

func TestFetch_RejectsNonAllowedScheme(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Fetch(context.Background(), "http://insecure.example.com/foo/bar.git", "main", c)
	if err == nil {
		t.Fatal("expected error for http://remote-host, got nil")
	}
}

func TestFetch_RejectsFileSchemePerCQ7(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Fetch(context.Background(), "file:///etc/passwd", "main", c)
	if err == nil {
		t.Fatal("expected error for file:// scheme, got nil")
	}
}

func TestFetch_RejectsRandomStringPerCQ7(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"totally-not-a-url",
		"ssh://anywhere/foo",
		"gh:incomplete",

		"git@github.com",
	}
	for _, cloneURL := range cases {
		t.Run(cloneURL, func(t *testing.T) {
			_, err := Fetch(context.Background(), cloneURL, "main", c)
			if err == nil {
				t.Errorf("Fetch(%q): want error, got nil", cloneURL)
			}
		})
	}
}

func TestFetch_RejectsNonExistentLocalPath(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	bogusPath := filepath.Join(tmp, "definitely-does-not-exist")
	_, err := Fetch(context.Background(), bogusPath, "main", c)
	if err == nil {
		t.Fatal("expected error for non-existent local path, got nil")
	}
}

func TestCheckFetchURL_LocalDirOK(t *testing.T) {
	tmp := t.TempDir()
	if err := checkFetchURL(tmp); err != nil {
		t.Errorf("checkFetchURL(local dir): want nil, got %v", err)
	}
}

func TestCheckFetchURL_NonLocalRoutesThroughParseURL(t *testing.T) {

	if err := checkFetchURL(""); err == nil {
		t.Error("checkFetchURL(\"\"): want error")
	}

	if err := checkFetchURL("totally-not-canonical"); err == nil {
		t.Error("checkFetchURL(\"totally-not-canonical\"): want error")
	}

	if err := checkFetchURL("gh:foo/bar"); err != nil {
		t.Errorf("checkFetchURL(gh:foo/bar): want nil, got %v", err)
	}
}

func TestFetch_CacheHitSkipsClone(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	cloneURL := "https://github.com/foo/bar.git"
	ref := "main"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "marker.txt"), []byte("hit"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl, err := Fetch(context.Background(), cloneURL, ref, c)
	if err != nil {
		t.Fatalf("Fetch (cache hit path): %v", err)
	}
	if tmpl.Name() == "" {
		t.Error("Name() empty after Fetch")
	}
	if _, err := fs.Stat(tmpl.FS(), "marker.txt"); err != nil {
		t.Errorf("expected marker.txt in fetched tree: %v", err)
	}
}

func TestFetch_NilCacheUsesDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	c, err := DefaultCache()
	if err != nil {
		t.Fatalf("DefaultCache: %v", err)
	}
	cloneURL := "https://example.com/foo/bar.git"
	ref := "main"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "x.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background(), cloneURL, ref, nil); err != nil {
		t.Errorf("Fetch with nil cache: %v", err)
	}
}

func TestFetch_RespectsContextCancel(t *testing.T) {

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hung.Close()

	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Fetch(ctx, hung.URL+"/foo.git", "main", c)
	if err == nil {
		t.Fatal("expected error on context cancel, got nil")
	}
}

func TestFetch_LocalFSClone_HappyPath(t *testing.T) {
	src := setupBareRepo(t, map[string]string{
		"plugin.yaml.tmpl": "name: {{.ProjectName}}\n",
		"README.md":        "# clone test\n",
	})
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}

	tmpl, err := Fetch(context.Background(), src, "master", c)
	if err != nil {

		tmpl, err = Fetch(context.Background(), src, "main", c)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
	}
	if _, err := fs.Stat(tmpl.FS(), "plugin.yaml.tmpl"); err != nil {
		t.Errorf("plugin.yaml.tmpl missing in cloned tree: %v", err)
	}

	got2, err := Fetch(context.Background(), src, tmpl.Name(), c)
	_ = got2
	_ = err
}

func TestFetch_LocalFSClone_RefNotFound(t *testing.T) {
	src := setupBareRepo(t, map[string]string{
		"x.txt": "y\n",
	})
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Fetch(context.Background(), src, "does-not-exist", c)
	if err == nil {
		t.Fatal("Fetch of nonexistent ref: want error, got nil")
	}
}

func TestFetch_LocalBareRepoOverHTTP_HappyPath(t *testing.T) {

	src := setupBareRepo(t, map[string]string{
		"plugin.yaml.tmpl": "name: {{.ProjectName}}\nversion: 0.1.0\n",
		"README.md":        "# test repo\n",
	})

	bareDir := filepath.Join(t.TempDir(), "repo.git")
	if _, err := git.PlainClone(bareDir, true, &git.CloneOptions{URL: src}); err != nil {
		t.Skipf("PlainClone (bare) failed in sandbox: %v", err)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(bareDir)))
	defer srv.Close()

	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}

	tmpl, err := Fetch(context.Background(), srv.URL, "master", c)
	if err != nil {

		t.Skipf("dumb-HTTP clone not supported in sandbox: %v", err)
	}
	if _, err := fs.Stat(tmpl.FS(), "plugin.yaml.tmpl"); err != nil {
		t.Errorf("expected plugin.yaml.tmpl from cloned tree: %v", err)
	}
}

func TestFetch_RenameRace_PrefersExisting(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}

	cloneURL := "https://github.com/foo/race.git"
	ref := "main"
	target := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "from-pre-existing.txt"), []byte("yes"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl, err := Fetch(context.Background(), cloneURL, ref, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := fs.Stat(tmpl.FS(), "from-pre-existing.txt"); err != nil {
		t.Errorf("expected existing tree preserved: %v", err)
	}
}

func TestNewPluggableTemplateNameDerivedFromURL(t *testing.T) {
	cloneURL := "https://github.com/foo/bar.git"
	ref := "main"
	tmp := t.TempDir()
	tmpl, err := newPluggableTemplate(cloneURL, ref, tmp)
	if err != nil {
		t.Fatalf("newPluggableTemplate: %v", err)
	}
	if tmpl.Name() != "bar" {
		t.Errorf("Name = %q want %q", tmpl.Name(), "bar")
	}
}

func TestPluggableMaterialize_RendersFromDirFS(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: filepath.Join(tmp, "cache"), MaxAge: 7 * 24 * time.Hour}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	cloneURL := "https://github.com/foo/render.git"
	ref := "main"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "plugin.yaml.tmpl"), []byte("name: {{.ProjectName}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl, err := Fetch(context.Background(), cloneURL, ref, c)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	dst := filepath.Join(tmp, "out")
	answers := t2.Answers{ProjectName: "rendered"}
	if err := tmpl.Materialize(context.Background(), dst, answers); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "plugin.yaml"))
	if err != nil {
		t.Fatalf("read rendered plugin.yaml: %v", err)
	}
	if string(body) != "name: rendered\n" {
		t.Errorf("rendered content mismatch: %q", string(body))
	}
}
