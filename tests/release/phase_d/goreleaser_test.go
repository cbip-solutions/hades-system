// SPDX-License-Identifier: MIT

package phase_d_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

type goreleaserBuild struct {
	ID     string   `yaml:"id"`
	Main   string   `yaml:"main"`
	Binary string   `yaml:"binary"`
	Goos   []string `yaml:"goos"`
	Goarch []string `yaml:"goarch"`
	Ignore []struct {
		Goos   string `yaml:"goos"`
		Goarch string `yaml:"goarch"`
	} `yaml:"ignore"`
}

type goreleaserConfig struct {
	Version int               `yaml:"version"`
	Project string            `yaml:"project_name"`
	Builds  []goreleaserBuild `yaml:"builds"`
}

func (b goreleaserBuild) enumeratedPlatforms() []string {
	ignored := make(map[string]bool, len(b.Ignore))
	for _, ig := range b.Ignore {
		ignored[ig.Goos+"/"+ig.Goarch] = true
	}
	var out []string
	for _, os_ := range b.Goos {
		for _, arch := range b.Goarch {
			key := os_ + "/" + arch
			if ignored[key] {
				continue
			}
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func loadGoreleaserConfig(t *testing.T, path string) goreleaserConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cfg
}

func TestGoreleaserBuildsCoverAllPlatforms(t *testing.T) {
	root := repoRoot(t)
	cfg := loadGoreleaserConfig(t, filepath.Join(root, ".goreleaser.yml"))
	if cfg.Version != 2 {
		t.Errorf("goreleaser config version: got %d, want 2", cfg.Version)
	}
	if cfg.Project != "zen-swarm" {
		t.Errorf("project_name: got %q, want zen-swarm", cfg.Project)
	}
	wantPlatforms := []string{
		"darwin/arm64",
		"linux/amd64",
		"linux/arm64",
	}
	wantBuildIDs := map[string]string{
		"zen":            "./cmd/zen",
		"zen-swarm-ctld": "./cmd/zen-swarm-ctld",
	}
	if len(cfg.Builds) != len(wantBuildIDs) {
		t.Fatalf("builds count: got %d, want %d (one per binary)", len(cfg.Builds), len(wantBuildIDs))
	}
	for _, build := range cfg.Builds {
		wantMain, ok := wantBuildIDs[build.ID]
		if !ok {
			t.Errorf("unexpected build ID: %q", build.ID)
			continue
		}
		if build.Main != wantMain {
			t.Errorf("build %q main: got %q, want %q", build.ID, build.Main, wantMain)
		}
		if build.Binary != build.ID {
			t.Errorf("build %q binary: got %q, want %q (binary name matches build id by convention)",
				build.ID, build.Binary, build.ID)
		}
		got := build.enumeratedPlatforms()
		if strings.Join(got, ",") != strings.Join(wantPlatforms, ",") {
			t.Errorf("build %q platforms: got %v, want %v", build.ID, got, wantPlatforms)
		}
	}
}

func TestGoreleaserConfigCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("requires goreleaser binary; skip in -short mode")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skip("goreleaser not in PATH; install via brew install goreleaser")
	}
	root := repoRoot(t)
	cmd := exec.Command("goreleaser", "check", "-f", filepath.Join(root, ".goreleaser.yml"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser check failed: %v\noutput:\n%s", err, out)
	}
}

func TestGoreleaserSnapshotTree(t *testing.T) {
	if testing.Short() {
		t.Skip("requires goreleaser + syft binaries + snapshot build; skip in -short mode")
	}
	if os.Getenv("GORELEASER_SNAPSHOT_TEST") != "1" {
		t.Skip("set GORELEASER_SNAPSHOT_TEST=1 to run the full goreleaser snapshot (slow)")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skip("goreleaser not in PATH")
	}
	if _, err := exec.LookPath("syft"); err != nil {
		t.Skip("syft not in PATH; install via brew install syft")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("unsupported runtime.GOOS %q for goreleaser snapshot", runtime.GOOS)
	}
	root := repoRoot(t)
	cmd := exec.Command("goreleaser", "release",
		"--snapshot", "--clean",
		"--skip=publish", "--skip=sign", "--skip=docker",
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GORELEASER_CURRENT_TAG=v0.0.0-test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser release --snapshot failed: %v\noutput:\n%s", err, out)
	}
	distDir := filepath.Join(root, "dist")
	var got []string
	werr := filepath.Walk(distDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(distDir, path)
		if rel == "." {
			return nil
		}
		got = append(got, rel)
		return nil
	})
	if werr != nil {
		t.Fatalf("walk dist: %v", werr)
	}
	goldenPath := filepath.Join("testdata", "goreleaser-snapshot-tree.want.txt")
	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}

	var wantLines []string
	for _, line := range strings.Split(string(wantBytes), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		wantLines = append(wantLines, trim)
	}
	sort.Strings(wantLines)
	gotNormalized := normalizeSnapshotPaths(got)
	if strings.Join(gotNormalized, "\n") != strings.Join(wantLines, "\n") {
		t.Errorf("dist tree drift:\nGOT:\n%s\nWANT:\n%s",
			strings.Join(gotNormalized, "\n"),
			strings.Join(wantLines, "\n"))
	}
}

var snapshotVersionRE = regexp.MustCompile(`\d+\.\d+\.\d+(?:-(?:snapshot-[a-f0-9]+|test))?`)

func normalizeSnapshotPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		s := snapshotVersionRE.ReplaceAllString(p, "<SNAPSHOT_VERSION>")
		out[i] = s
	}
	sort.Strings(out)
	return out
}
