// tests/compliance/inv_zen_223_daemon_recovery_consistency_test.go
//
// inv-zen-223 (v0.17.2 / ADR-0099) — daemon recovery-guidance consistency.
//
// Root cause of the v0.17.2 hot-fix: the daemon-down recovery guidance had
// drifted from deployed reality across three surfaces —
//
//   - the error catalog hints (internal/errors/codes.go: daemon.not-running,
//     daemon.unreachable) named a phantom hyphenated LaunchAgent label
//     (com.zen-swarm.ctld) that launchctl never registers — the deployed
//     label is com.zenswarm.ctld (no hyphen) — and pointed at a nonexistent
//     docs/operations/daemon.md;
//   - the `hades` wrapper (cmd/hades/main.go) emitted a Plan 18a placeholder
//     (`zen-swarm-ctld -uds &`) instead of the shipped `hades daemon`
//     command family;
//   - the operator handbook (docs/operations/hades-entry-point.md §4.2)
//     repeated the phantom label + manual incantation.
//
// inv-zen-223 pins that drift class: across the LIVE recovery surfaces, the
// guidance MUST reference the shipped `hades daemon` family + the canonical
// DEPLOYED label, and MUST NOT reference the phantom hyphenated ctld label or
// the missing doc. (Historical plan/spec docs that RECORD the old phantom as
// the bug they describe are intentionally NOT scanned here.) See ADR-0099.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var inv223RecoverySurfaces = []string{
	"internal/errors/codes.go",
	"cmd/hades/main.go",
	"docs/operations/hades-entry-point.md",
}

const phantomCtldLabel = "com.zen-swarm.ctld"

const deadDaemonDoc = "docs/operations/daemon.md"

func repoRoot223(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-223: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-223: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen223_NoPhantomLabel(t *testing.T) {
	root := repoRoot223(t)
	for _, rel := range inv223RecoverySurfaces {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("inv-zen-223: read %s: %v", rel, err)
		}
		if strings.Contains(string(body), phantomCtldLabel) {
			t.Errorf("inv-zen-223: %s references the phantom hyphenated label %q; the deployed ctld label is com.zenswarm.ctld (no hyphen). See ADR-0099.", rel, phantomCtldLabel)
		}
	}
}

func TestInvZen223_NoDeadDaemonDoc(t *testing.T) {
	root := repoRoot223(t)
	if _, err := os.Stat(filepath.Join(root, deadDaemonDoc)); err == nil {
		t.Fatalf("inv-zen-223: %s now EXISTS — the invariant premise (it is the dead-reference target) is stale; revisit ADR-0099 + the recovery hints", deadDaemonDoc)
	}
	for _, rel := range inv223RecoverySurfaces {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("inv-zen-223: read %s: %v", rel, err)
		}
		if strings.Contains(string(body), deadDaemonDoc) {
			t.Errorf("inv-zen-223: %s references nonexistent %q; point at docs/operations/hades-entry-point.md §4.2 instead. See ADR-0099.", rel, deadDaemonDoc)
		}
	}
}

func TestInvZen223_HintsReferenceShippedCommand(t *testing.T) {
	root := repoRoot223(t)
	for _, rel := range inv223RecoverySurfaces {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("inv-zen-223: read %s: %v", rel, err)
		}
		if !strings.Contains(string(body), "hades daemon") {
			t.Errorf("inv-zen-223: %s does not reference the shipped `hades daemon` recovery command family. See ADR-0099.", rel)
		}
	}
}

func TestInvZen223_CanonicalLabelIsDeployed(t *testing.T) {
	root := repoRoot223(t)
	const canonical = "com.zenswarm.ctld"
	mustContain := map[string]string{
		"cmd/hades/main.go":          canonical,
		"scripts/install-launchd.sh": canonical,
		"configs/launchd.plist.tmpl": canonical,
		"internal/cli/daemon.go":     canonical,
	}
	for rel, want := range mustContain {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("inv-zen-223: read %s: %v", rel, err)
		}
		if !strings.Contains(string(body), want) {
			t.Errorf("inv-zen-223: %s does not reference the canonical deployed label %q; recovery guidance + install assets must agree. See ADR-0099.", rel, want)
		}
		if strings.Contains(string(body), phantomCtldLabel) {
			t.Errorf("inv-zen-223: %s references the phantom hyphenated label %q (deployed is %q). See ADR-0099.", rel, phantomCtldLabel, canonical)
		}
	}
}
