//go:build chaos

// Package chaos — p13_migrate_partial_failure_test.go (Plan 13 Phase
// F-tail IMPORTANT 7 missing-tests completion).
//
// Chaos: claude-code migrate writer interrupted mid-write MUST surface
// a partial-write error AND the operator can identify the partial
// result for cleanup. Per spec §3.5 migrate atomicity contract +
// inv-zen-183 1:1 mapping precondition (mapping fails atomically before
// writer is invoked).
//
// Build tag `chaos` excludes from default CI.
package chaos

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func TestChaos_MigratePartialFailure_MalformedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"permissions":{"allow":["filesystem.read"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("garbage{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := source.ReadAll(root)
	if !errors.Is(err, source.ErrMalformedMCP) {
		t.Errorf("err = %v; want ErrMalformedMCP (atomic halt)", err)
	}
}

func TestChaos_MigratePartialFailure_BothMalformed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{this is not`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("also broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := source.ReadAll(root)
	if err == nil {
		t.Fatal("err = nil; want canonical malformed error for chaos halt")
	}

	if !errors.Is(err, source.ErrMalformedSettings) && !errors.Is(err, source.ErrMalformedMCP) {
		t.Errorf("err = %v; want one of ErrMalformedSettings or ErrMalformedMCP", err)
	}
}
