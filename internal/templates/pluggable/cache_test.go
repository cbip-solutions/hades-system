package pluggable

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKey_DeterministicPerURLPlusRef(t *testing.T) {
	a := CacheKey("https://github.com/foo/bar.git", "v1.2.3")
	b := CacheKey("https://github.com/foo/bar.git", "v1.2.3")
	if a != b {
		t.Errorf("CacheKey not deterministic: %q != %q", a, b)
	}
	c := CacheKey("https://github.com/foo/bar.git", "v1.2.4")
	if a == c {
		t.Errorf("CacheKey collision across refs: %q == %q", a, c)
	}
	d := CacheKey("https://github.com/foo/bar2.git", "v1.2.3")
	if a == d {
		t.Errorf("CacheKey collision across URLs: %q == %q", a, d)
	}
}

func TestCachePathSafePerURL(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	p := c.PathFor("https://github.com/foo/bar.git", "v1.2.3")
	rel, err := filepath.Rel(tmp, p)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	if filepath.IsAbs(rel) || rel == ".." || rel == "." || rel == "" {
		t.Errorf("path %q escapes cache root %q", p, tmp)
	}
	if filepath.Dir(p) != tmp {
		t.Errorf("path %q parent != cache root %q", p, tmp)
	}
}

func TestCacheEvictionLRU(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	old := filepath.Join(tmp, "evict-me")
	keep1 := filepath.Join(tmp, "keep-1")
	keep2 := filepath.Join(tmp, "keep-2")
	for _, p := range []string{old, keep1, keep2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tenDaysAgo := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, tenDaysAgo, tenDaysAgo); err != nil {
		t.Fatal(err)
	}
	evicted, err := c.Evict()
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if evicted != 1 {
		t.Errorf("evicted count: got %d want 1", evicted)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected %q evicted, still present (err=%v)", old, err)
	}
	for _, p := range []string{keep1, keep2} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q kept, got err=%v", p, err)
		}
	}
}

func TestCacheTryGet_HitFreshEntry(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	cloneURL := "https://github.com/foo/bar.git"
	ref := "v1.2.3"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, hit := c.TryGet(cloneURL, ref)
	if !hit {
		t.Fatal("expected cache hit")
	}
	if got != stub {
		t.Errorf("got %q, want %q", got, stub)
	}
}

func TestCacheTryGet_MissAbsentEntry(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	_, hit := c.TryGet("https://github.com/foo/bar.git", "v1.2.3")
	if hit {
		t.Error("expected miss on empty cache")
	}
}

func TestCacheTryGet_MissExpiredEntry(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	cloneURL := "https://github.com/foo/bar.git"
	ref := "v1.2.3"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	tenDaysAgo := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stub, tenDaysAgo, tenDaysAgo); err != nil {
		t.Fatal(err)
	}
	_, hit := c.TryGet(cloneURL, ref)
	if hit {
		t.Error("expected miss on expired entry")
	}
}

func TestTouchUpdatesModtime(t *testing.T) {
	tmp := t.TempDir()
	c := &Cache{Root: tmp, MaxAge: 7 * 24 * time.Hour}
	cloneURL := "https://github.com/foo/bar.git"
	ref := "main"
	stub := c.PathFor(cloneURL, ref)
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(stub, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := c.Touch(cloneURL, ref); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	info, _ := os.Stat(stub)
	if !info.ModTime().After(oldTime) {
		t.Errorf("Touch did not refresh modtime; got %v want > %v", info.ModTime(), oldTime)
	}
}

func TestDefaultCache_RespectsXDGCacheHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	c, err := DefaultCache()
	if err != nil {
		t.Fatalf("DefaultCache: %v", err)
	}
	want := filepath.Join(tmp, "zen-swarm", "templates")
	if c.Root != want {
		t.Errorf("Root: got %q want %q", c.Root, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("Root not created: %v", err)
	}
}
