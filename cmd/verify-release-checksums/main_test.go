// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeDist(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	var manifest bytes.Buffer
	for name, body := range entries {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		sum := sha256.Sum256(body)

		manifest.WriteString(hex.EncodeToString(sum[:]))
		manifest.WriteString("  ")
		manifest.WriteString(name)
		manifest.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), manifest.Bytes(), 0o644); err != nil {
		t.Fatalf("write checksums.txt: %v", err)
	}
	return dir
}

func TestVerify_AllChecksumsMatch(t *testing.T) {
	dir := makeDist(t, map[string][]byte{
		"zen-swarm-v1.0.0-darwin-arm64.tar.gz": []byte("darwin-arm64 payload"),
		"zen-swarm-v1.0.0-linux-amd64.tar.gz":  []byte("linux-amd64 payload"),
		"zen-swarm-v1.0.0-linux-arm64.tar.gz":  []byte("linux-arm64 payload"),
	})
	opts := defaultOptions()
	opts.distDir = dir
	opts.requireAllPlatforms = true
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	if err := verify(opts); err != nil {
		t.Fatalf("verify() failed unexpectedly: %v\nlog:\n%s", err, buf.String())
	}
}

func TestVerify_ChecksumMismatch(t *testing.T) {
	dir := makeDist(t, map[string][]byte{
		"zen-swarm-v1.0.0-darwin-arm64.tar.gz": []byte("good"),
		"zen-swarm-v1.0.0-linux-amd64.tar.gz":  []byte("good"),
		"zen-swarm-v1.0.0-linux-arm64.tar.gz":  []byte("good"),
	})

	if err := os.WriteFile(filepath.Join(dir, "zen-swarm-v1.0.0-linux-amd64.tar.gz"), []byte("CORRUPT"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	opts := defaultOptions()
	opts.distDir = dir
	opts.requireAllPlatforms = true
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("verify() error = %q; want substring 'checksum mismatch'", err.Error())
	}
}

func TestVerify_MissingArtifact(t *testing.T) {
	dir := makeDist(t, map[string][]byte{
		"zen-swarm-v1.0.0-darwin-arm64.tar.gz": []byte("good"),
	})

	if err := os.Remove(filepath.Join(dir, "zen-swarm-v1.0.0-darwin-arm64.tar.gz")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	opts := defaultOptions()
	opts.distDir = dir
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected missing-artifact error")
	}
	if !strings.Contains(err.Error(), "missing artifact") {
		t.Errorf("verify() error = %q; want substring 'missing artifact'", err.Error())
	}
}

func TestVerify_MissingChecksumsFile(t *testing.T) {
	dir := t.TempDir()
	opts := defaultOptions()
	opts.distDir = dir
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected missing checksums.txt error")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("verify() error = %q; want substring 'checksums.txt'", err.Error())
	}
}

func TestVerify_MalformedChecksumLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte("not-a-checksum-line\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := defaultOptions()
	opts.distDir = dir
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected malformed-line error")
	}
}

func TestVerify_RequireAllPlatforms(t *testing.T) {
	dir := makeDist(t, map[string][]byte{

		"zen-swarm-v1.0.0-darwin-arm64.tar.gz": []byte("a"),
		"zen-swarm-v1.0.0-linux-amd64.tar.gz":  []byte("b"),
	})
	opts := defaultOptions()
	opts.distDir = dir
	opts.requireAllPlatforms = true
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected 3-platform invariant failure")
	}
	if !strings.Contains(err.Error(), "linux-arm64") {
		t.Errorf("verify() error = %q; want substring 'linux-arm64'", err.Error())
	}
}

func TestParseChecksumsFile(t *testing.T) {
	in := strings.Join([]string{
		"0000000000000000000000000000000000000000000000000000000000000001  zen-swarm-v1.0.0-darwin-arm64.tar.gz",
		"# comment line is ignored",
		"",
		"0000000000000000000000000000000000000000000000000000000000000002  zen-swarm-v1.0.0-linux-amd64.tar.gz",
	}, "\n") + "\n"

	got, err := parseChecksumsManifest(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got["zen-swarm-v1.0.0-darwin-arm64.tar.gz"] != "0000000000000000000000000000000000000000000000000000000000000001" {
		t.Errorf("darwin-arm64 checksum mismatch: got %q", got["zen-swarm-v1.0.0-darwin-arm64.tar.gz"])
	}
}

func TestParseChecksumsFile_BadHexLength(t *testing.T) {
	in := "deadbeef  zen-swarm-v1.0.0-darwin-arm64.tar.gz\n"
	_, err := parseChecksumsManifest(strings.NewReader(in))
	if err == nil {
		t.Fatal("parseChecksumsManifest accepted short hex; want error")
	}
}

func TestParseChecksumsFile_DuplicateFilename(t *testing.T) {
	in := "0000000000000000000000000000000000000000000000000000000000000001  z.tar.gz\n" +
		"0000000000000000000000000000000000000000000000000000000000000002  z.tar.gz\n"
	_, err := parseChecksumsManifest(strings.NewReader(in))
	if err == nil {
		t.Fatal("parseChecksumsManifest accepted duplicate filename; want error")
	}
}

func TestGoldenManifest_RequiredFields(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "release-gates", "release-checksums.golden.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden manifest %s: %v", path, err)
	}
	var m goldenManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse golden manifest: %v", err)
	}
	if len(m.Platforms) == 0 {
		t.Error("golden manifest: Platforms array empty")
	}
	wantPlatforms := map[string]bool{
		"darwin-arm64": false,
		"linux-amd64":  false,
		"linux-arm64":  false,
	}
	for _, p := range m.Platforms {
		if _, ok := wantPlatforms[p.Platform]; ok {
			wantPlatforms[p.Platform] = true
		}
	}
	for plat, seen := range wantPlatforms {
		if !seen {
			t.Errorf("golden manifest missing platform %q", plat)
		}
	}
	if m.VersionSummaryRegex == "" {
		t.Error("golden manifest: VersionSummaryRegex empty")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatalf("repo root not found from %s", wd)
	return ""
}

func TestVerifyAgainstGolden(t *testing.T) {
	dir := makeDist(t, map[string][]byte{
		"zen-swarm-v1.0.0-darwin-arm64.tar.gz": []byte("a"),
	})
	opts := defaultOptions()
	opts.distDir = dir
	opts.goldenPath = filepath.Join(repoRoot(t), "scripts", "release-gates", "release-checksums.golden.json")
	opts.version = "v1.0.0"
	opts.requireAllPlatforms = true
	var buf bytes.Buffer
	opts.stdout = &buf
	opts.stderr = &buf
	err := verify(opts)
	if err == nil {
		t.Fatal("verify() succeeded; expected golden-platform-mismatch error")
	}
	if !strings.Contains(err.Error(), "linux-amd64") && !strings.Contains(err.Error(), "linux-arm64") {
		t.Errorf("verify() error = %q; want substring referencing missing linux artifact", err.Error())
	}
}

func TestVersionSummaryParse(t *testing.T) {
	good := "zen-swarm v1.0.0 commit:abc1234 date:2026-05-25T10:00:00Z go:1.25.6 platform:darwin/arm64"
	got, err := parseVersionSummary(good)
	if err != nil {
		t.Fatalf("parseVersionSummary good: %v", err)
	}
	if got.Version != "v1.0.0" || got.Commit != "abc1234" || got.Platform != "darwin/arm64" {
		t.Errorf("parseVersionSummary fields drift: %+v", got)
	}
	if got.GoVersion != "1.25.6" || got.Date != "2026-05-25T10:00:00Z" {
		t.Errorf("parseVersionSummary go/date drift: %+v", got)
	}

	bad := []string{
		"zen-swarm v1.0.0 (no commit field)",
		"",
		"completely unrelated",
		"zen-swarm v1.0.0 commit:x date:y go:z",
	}
	for _, b := range bad {
		if _, err := parseVersionSummary(b); err == nil {
			t.Errorf("parseVersionSummary(%q) succeeded; want error", b)
		}
	}
}
