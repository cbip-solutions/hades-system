package ci_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

func TestCache_RoundTrip(t *testing.T) {

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	commit := ci.CommitStatus{
		SHA:    "deadbeef00000000000000000000000000000000",
		Status: "success",
		Bucket: "success",
		Reason: "",
		URL:    "https://github.com/test/repo/runs/1",
		Date:   time.Now().UTC().Truncate(time.Second),
	}
	if err := ci.CacheStore(commit); err != nil {
		t.Fatalf("CacheStore: %v", err)
	}
	got, ok := ci.CacheLoad(commit.SHA)
	if !ok {
		t.Fatalf("CacheLoad miss after CacheStore")
	}
	if got.SHA != commit.SHA {
		t.Errorf("round-trip SHA: got %s; want %s", got.SHA, commit.SHA)
	}
	if got.Status != commit.Status {
		t.Errorf("round-trip Status: got %s; want %s", got.Status, commit.Status)
	}
	if got.Bucket != commit.Bucket {
		t.Errorf("round-trip Bucket: got %s; want %s", got.Bucket, commit.Bucket)
	}
	if got.URL != commit.URL {
		t.Errorf("round-trip URL: got %s; want %s", got.URL, commit.URL)
	}
	if !got.Date.Equal(commit.Date) {
		t.Errorf("round-trip Date: got %v; want %v", got.Date, commit.Date)
	}
}

func TestCache_MissOnUnknownSHA(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	_, ok := ci.CacheLoad("nonexistent0000000000000000000000000000")
	if ok {
		t.Errorf("expected miss for nonexistent SHA")
	}
}

func TestCache_MissOnVersionMismatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Manually write a cache entry stamped with a bogus version; CacheLoad
	// MUST reject and return miss (forces re-classify when ClassifierVersion
	// bumps).
	dir, err := ci.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	sha := "deadbeef00000000000000000000000000000000"
	path := filepath.Join(dir, sha+".json")
	stale := map[string]interface{}{
		"ClassifierVersion": "0.0-bogus-old-version",
		"Commit": map[string]interface{}{
			"SHA": sha, "Status": "success", "Bucket": "success",
		},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	_, ok := ci.CacheLoad(sha)
	if ok {
		t.Errorf("expected miss on version mismatch; got hit (cache invalidation broken)")
	}
}

func TestCacheDir_Created(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir, err := ci.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	want := filepath.Join(tmp, ".cache", "hades", "ci")
	if dir != want {
		t.Errorf("CacheDir: got %s; want %s (hades rename per master §2.6)", dir, want)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Errorf("CacheDir not created: %v", err)
	}
	if st != nil && !st.IsDir() {
		t.Errorf("CacheDir path is not a directory")
	}
}

func TestCache_CorruptJSONMissesGracefully(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir, err := ci.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	sha := "corrupt0000000000000000000000000000000a"
	path := filepath.Join(dir, sha+".json")
	if err := os.WriteFile(path, []byte("not valid json{{"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	_, ok := ci.CacheLoad(sha)
	if ok {
		t.Errorf("expected miss on corrupt cache JSON; got hit")
	}
}

func TestCacheDir_MkdirAllFailure(t *testing.T) {

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cachePath := filepath.Join(tmp, ".cache")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	hadesPath := filepath.Join(cachePath, "hades")
	if err := os.WriteFile(hadesPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("setup write blocker: %v", err)
	}
	_, err := ci.CacheDir()
	if err == nil {
		t.Fatal("expected error when ~/.cache/hades is a file (not dir); got nil")
	}
}

func TestCacheLoad_PropagatesCacheDirFailure(t *testing.T) {

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cachePath := filepath.Join(tmp, ".cache")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "hades"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, ok := ci.CacheLoad("anysha")
	if ok {
		t.Errorf("expected miss when CacheDir fails; got hit")
	}
}

func TestCacheStore_PropagatesCacheDirFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cachePath := filepath.Join(tmp, ".cache")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "hades"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := ci.CacheStore(ci.CommitStatus{SHA: "abc"})
	if err == nil {
		t.Error("expected error when CacheDir fails; got nil")
	}
}

func TestCacheStore_WriteFailure(t *testing.T) {

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir, err := ci.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)
	err = ci.CacheStore(ci.CommitStatus{SHA: "writefailtest"})
	if err == nil {
		t.Error("expected write error on read-only dir; got nil")
	}
}

func TestCache_StampedWithClassifierVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	commit := ci.CommitStatus{
		SHA:    "abc0000000000000000000000000000000000001",
		Status: "success",
		Bucket: "success",
	}
	if err := ci.CacheStore(commit); err != nil {
		t.Fatalf("CacheStore: %v", err)
	}
	dir, _ := ci.CacheDir()
	data, err := os.ReadFile(filepath.Join(dir, commit.SHA+".json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	version, ok := raw["ClassifierVersion"].(string)
	if !ok || version != ci.ClassifierVersion {
		t.Errorf("ClassifierVersion stamp: got %v; want %q", raw["ClassifierVersion"], ci.ClassifierVersion)
	}
}
