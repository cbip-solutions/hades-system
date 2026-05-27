// go:build integration
package grpcstub

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func requireFixtures(t *testing.T) string {
	t.Helper()
	d := filepath.Join(repoRoot(t), "tests", "integration", "plan20", "grpcstub", "fixtures")
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("fixtures missing: %v", err)
	}
	return d
}

func disableKeychain(t *testing.T) {
	t.Helper()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
}
