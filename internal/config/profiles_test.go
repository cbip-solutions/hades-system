package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func writeProfilesTOML(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "profiles.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write profiles.toml: %v", err)
	}
	return p
}

func TestLoadProfilesValid(t *testing.T) {
	dir := t.TempDir()
	path := writeProfilesTOML(t, dir, `
[profiles.worker-code]
description = "DeepSeek-V3 direct then aggregator fallback"
cascade     = ["deepseek-direct", "siliconflow-deepseek", "openrouter-deepseek"]

[profiles.orchestrator]
cascade = ["bypass", "gemini-pro"]
`)
	got, err := config.LoadProfiles(path)
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	wc, ok := got["worker-code"]
	if !ok {
		t.Fatal("missing worker-code profile")
	}
	if wc.Name != "worker-code" {
		t.Errorf("Name = %q, want worker-code (populated from table key)", wc.Name)
	}
	if len(wc.Cascade) != 3 || wc.Cascade[0] != "deepseek-direct" || wc.Cascade[2] != "openrouter-deepseek" {
		t.Errorf("cascade wrong: %v", wc.Cascade)
	}
	if wc.Description != "DeepSeek-V3 direct then aggregator fallback" {
		t.Errorf("description wrong: %q", wc.Description)
	}
}

func TestLoadProfilesRejectsEmptyCascade(t *testing.T) {
	dir := t.TempDir()
	path := writeProfilesTOML(t, dir, `
[profiles.broken]
cascade = []
`)
	_, err := config.LoadProfiles(path)
	if err == nil {
		t.Fatal("LoadProfiles accepted a profile with an empty cascade")
	}
}

func TestLoadProfilesMissingFile(t *testing.T) {
	_, err := config.LoadProfiles(filepath.Join(t.TempDir(), "absent.toml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}
