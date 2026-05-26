// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootInvZen294(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-294: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-294: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

type invZen294Build struct {
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

type invZen294Config struct {
	Version int              `yaml:"version"`
	Project string           `yaml:"project_name"`
	Builds  []invZen294Build `yaml:"builds"`
}

func TestInvZen294_GoReleaserConfigPresent(t *testing.T) {
	root := repoRootInvZen294(t)
	configPath := filepath.Join(root, ".goreleaser.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("inv-zen-294 VIOLATED: .goreleaser.yml missing: %v", err)
	}
	var cfg invZen294Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-294 VIOLATED: .goreleaser.yml parse: %v", err)
	}
	if cfg.Version != 2 {
		t.Errorf("inv-zen-294 VIOLATED: .goreleaser.yml version=%d, want 2", cfg.Version)
	}
	if cfg.Project != "zen-swarm" {
		t.Errorf("inv-zen-294 VIOLATED: .goreleaser.yml project_name=%q, want zen-swarm", cfg.Project)
	}
}

func TestInvZen294_ThreePlatformMatrix(t *testing.T) {
	root := repoRootInvZen294(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-294: read config: %v", err)
	}
	var cfg invZen294Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-294: parse config: %v", err)
	}
	wantPlatforms := []string{"darwin/arm64", "linux/amd64", "linux/arm64"}
	wantBuilds := map[string]string{
		"zen":            "./cmd/zen",
		"zen-swarm-ctld": "./cmd/zen-swarm-ctld",
	}
	if len(cfg.Builds) != len(wantBuilds) {
		t.Fatalf("inv-zen-294 VIOLATED: build count=%d, want %d (one per binary)",
			len(cfg.Builds), len(wantBuilds))
	}
	for _, build := range cfg.Builds {
		wantMain, ok := wantBuilds[build.ID]
		if !ok {
			t.Errorf("inv-zen-294 VIOLATED: unexpected build id %q", build.ID)
			continue
		}
		if build.Main != wantMain {
			t.Errorf("inv-zen-294 VIOLATED: build %q main=%q, want %q",
				build.ID, build.Main, wantMain)
		}
		ignored := make(map[string]bool, len(build.Ignore))
		for _, ig := range build.Ignore {
			ignored[ig.Goos+"/"+ig.Goarch] = true
		}
		var got []string
		for _, os_ := range build.Goos {
			for _, arch := range build.Goarch {
				key := os_ + "/" + arch
				if ignored[key] {
					continue
				}
				got = append(got, key)
			}
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(wantPlatforms, ",") {
			t.Errorf("inv-zen-294 VIOLATED: build %q platforms=%v, want %v",
				build.ID, got, wantPlatforms)
		}
	}
}

func TestInvZen294_ReproducibilityLDFlags(t *testing.T) {
	root := repoRootInvZen294(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-294: read config: %v", err)
	}

	var cfg struct {
		Builds []struct {
			ID      string   `yaml:"id"`
			LDFlags []string `yaml:"ldflags"`
			Flags   []string `yaml:"flags"`
		} `yaml:"builds"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-294: parse config: %v", err)
	}
	requiredLDFlag := []string{
		"-X main.version=",
		"-X main.commit=",
		"-X main.date=",
		"-buildid=",
	}
	requiredFlag := []string{"-trimpath"}
	for _, build := range cfg.Builds {
		joined := strings.Join(build.LDFlags, " ")
		for _, want := range requiredLDFlag {
			if !strings.Contains(joined, want) {
				t.Errorf("inv-zen-294/297 VIOLATED: build %q ldflags missing %q in %q",
					build.ID, want, joined)
			}
		}
		flagsJoined := strings.Join(build.Flags, " ")
		for _, want := range requiredFlag {
			if !strings.Contains(flagsJoined, want) {
				t.Errorf("inv-zen-294/297 VIOLATED: build %q flags missing %q in %q",
					build.ID, want, flagsJoined)
			}
		}
	}
}
