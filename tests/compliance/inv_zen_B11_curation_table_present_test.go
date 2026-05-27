package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootB11(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not find go.mod root")
		}
		root = parent
	}
}

func TestInvZenB11_CurationTablePresent(t *testing.T) {
	root := repoRootB11(t)
	path := filepath.Join(root, "docs", "operations", "bypass-changelog-curation-table.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("curation table missing at canonical path %s: %v (decisión 17-c requires this file as the per-version partition source of truth)", path, err)
	}
	if info.IsDir() {
		t.Fatalf("curation table path %s is a directory; must be a regular file", path)
	}
	if info.Size() == 0 {
		t.Fatalf("curation table at %s is empty; decisión 17-c partition must be encoded", path)
	}
}

func TestInvZenB11_CurationTableCoversAllReleases(t *testing.T) {
	root := repoRootB11(t)
	path := filepath.Join(root, "docs", "operations", "bypass-changelog-curation-table.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read curation table: %v", err)
	}
	content := string(body)

	// Every release tag MUST appear at least once (covered by a row in §2).
	// The literal `**vX.Y.Z**` form is what each row uses in the first
	// table column — anchoring on the bold-asterisk delimiter avoids
	// false positives from prose mentions in §3/§4/§5.
	requiredVersions := []string{
		"v0.17.1", "v0.17.2", "v0.17.3", "v0.17.4",
		"v0.17.5", "v0.17.6", "v0.17.7", "v0.17.8",
		"v0.17.9", "v0.17.10", "v0.17.11", "v0.17.12",
		"v0.18.0", "v0.19.0",
		"v0.20.0", "v0.20.1", "v0.20.2", "v0.20.3",
		"v0.20.4", "v0.20.5", "v0.20.6", "v0.20.7",
	}
	for _, ver := range requiredVersions {
		needle := "**" + ver + "**"
		if !strings.Contains(content, needle) {
			t.Errorf("curation table missing row for release tag %s (looked for bold-anchored %q); decisión 17-c partition must cover every release", ver, needle)
		}
	}
}

func TestInvZenB11_CurationTablePrivacyHeader(t *testing.T) {
	root := repoRootB11(t)
	path := filepath.Join(root, "docs", "operations", "bypass-changelog-curation-table.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read curation table: %v", err)
	}
	content := string(body)

	// The doc MUST self-classify as PRIVATE-ONLY + reference policy
	// in the opening preamble (first ~50 lines / privacy callout block).
	// Both anchors are required so a future edit that softens one signal
	// trips the gate.
	requiredAnchors := []string{
		"PRIVATE-ONLY",
		"decisión 17-c",
	}
	// Restrict the scan to the first ~120 lines so a future references-section
	// mention of policy" downstream does not satisfy the privacy-header
	// requirement (the header MUST be in the preamble).
	prefix := content
	if len(content) > 6000 {
		prefix = content[:6000]
	}
	for _, anchor := range requiredAnchors {
		if !strings.Contains(prefix, anchor) {
			t.Errorf("curation table preamble missing required privacy anchor %q; decisión 17-c requires PRIVATE-ONLY self-classification in the opening", anchor)
		}
	}
}
