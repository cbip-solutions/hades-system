package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var invZen212ForbiddenSubstrings = []string{
	"OpenClaudeBackend",
	"NewOpenClaudeBackend",
}

const invZen212ForbiddenFile = "openclaude_backend.go"

func invZen212RepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func TestInvZen212OpenClaudeBackendFileRemoved(t *testing.T) {
	root := invZen212RepoRoot(t)
	providersDir := filepath.Join(root, "internal", "providers")

	entries, err := os.ReadDir(providersDir)
	if err != nil {
		t.Fatalf("inv-zen-212: read internal/providers/: %v "+
			"(the routing-layer providers package must exist)", err)
	}

	scanned := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		scanned++

		if strings.HasPrefix(name, "openclaude_backend") {
			t.Errorf("inv-zen-212: internal/providers/%s still exists — "+
				"Plan 16 Phase C deleted the routing-layer OpenClaude backend; "+
				"this file must not be re-introduced", name)
		}
	}

	if scanned == 0 {
		t.Fatalf("inv-zen-212: sentinel failure — 0 .go files found in %s; "+
			"the providers package layout may have changed", providersDir)
	}
}

func TestInvZen212NoOpenClaudeBackendSymbols(t *testing.T) {
	root := invZen212RepoRoot(t)
	providersDir := filepath.Join(root, "internal", "providers")

	entries, err := os.ReadDir(providersDir)
	if err != nil {
		t.Fatalf("inv-zen-212: read internal/providers/: %v", err)
	}

	scanned := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		abs := filepath.Join(providersDir, name)
		b, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("inv-zen-212: read %s: %v", abs, err)
		}
		body := string(b)
		for _, forbidden := range invZen212ForbiddenSubstrings {
			if strings.Contains(body, forbidden) {
				t.Errorf("inv-zen-212: internal/providers/%s references %q — "+
					"the routing-layer OpenClaude backend was removed in Plan 16 "+
					"Phase C and must not be re-introduced", name, forbidden)
			}
		}
	}

	if scanned == 0 {
		t.Fatalf("inv-zen-212: sentinel failure — 0 non-test .go files found in %s", providersDir)
	}
}

// TestInvZen212TierOpenClaudeTombstoneKept asserts the OTHER half of
// invariant: the TierOpenClaude enum constant SURVIVES. It is the one
// permitted remnant — the string "openclaude" is persisted in
// cost_ledger.tier for pre- historical rows, and provider.go states
// the Tier String() values MUST NOT change across releases. A change that
// over-zealously deletes the enum would break decoding of historical audit
// data; this assertion catches that.
func TestInvZen212TierOpenClaudeTombstoneKept(t *testing.T) {
	root := invZen212RepoRoot(t)
	providerGo := filepath.Join(root, "internal", "providers", "provider.go")

	b, err := os.ReadFile(providerGo)
	if err != nil {
		t.Fatalf("inv-zen-212: read provider.go: %v", err)
	}
	body := string(b)

	if !strings.Contains(body, "TierOpenClaude") {
		t.Error("inv-zen-212: provider.go no longer declares TierOpenClaude — " +
			"the enum constant is a REQUIRED tombstone (persisted in " +
			"cost_ledger.tier for historical rows); it must be kept")
	}

	if !strings.Contains(body, `"openclaude"`) {
		t.Error(`inv-zen-212: provider.go no longer maps TierOpenClaude to ` +
			`"openclaude" — the canonical string MUST be kept so historical ` +
			`cost_ledger.tier rows still decode`)
	}
}
