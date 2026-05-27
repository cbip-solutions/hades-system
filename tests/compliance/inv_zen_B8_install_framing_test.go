// SPDX-License-Identifier: MIT

// Package compliance — task B-14 compliance test for
// inv-zen-B8 (INSTALL.md framing discipline policy +
// 4 + 11).
//
// Purpose: the public-snapshot INSTALL.md is the first surface a new
// operator reads after the README. Per v1.0 release decisions,
// it MUST:
//
// - lead with the provider cascade as the default;
// - frame the Tier 1 sidecar as "for advanced Anthropic configurations"
// , NOT raw "bypass" terminology;
// - link the community-recipe HTTP API contract doc
// (`docs/operations/bypass-sidecar-recipe.md`) policy;
// - omit references to private repos (`cbip-solutions/zen-bypass-tier1`,
// `cbip-solutions/homebrew-private-tap`) policy
//
// The four sub-tests below pin each property independently so a future
// drift surfaces a precise failure rather than a single boolean.
//
// Companion ADR-free ( B-14 ships test+doc only; no ADR allocation
// per spec line 2398). Composes into `make verify-invariants` via the
// standard compliance suite.

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZenB8_InstallMDPaygoCascadeDefault(t *testing.T) {
	t.Parallel()
	root := repoRootInvZenB8(t)
	body, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	content := string(body)

	if !strings.Contains(content, "Plan 16 cascade") && !strings.Contains(content, "provider cascade") {
		t.Error("INSTALL.md missing provider cascade default section (decisión 17-i): expect 'Plan 16 cascade' or 'provider cascade' literal")
	}
	if !strings.Contains(content, "default") {
		t.Error("INSTALL.md missing 'default' framing for paygo cascade (decisión 17-i)")
	}
}

func TestInvZenB8_InstallMDFramedTerminology(t *testing.T) {
	t.Parallel()
	root := repoRootInvZenB8(t)
	body, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	content := string(body)

	if !strings.Contains(content, "Tier 1 sidecar") {
		t.Error("INSTALL.md missing 'Tier 1 sidecar' framed terminology (decisión 17-h)")
	}
	if !strings.Contains(content, "advanced") {
		t.Error("INSTALL.md missing 'advanced' Anthropic-configurations framing (decisión 17-h)")
	}

	bypassCount := strings.Count(strings.ToLower(content), "bypass")
	if bypassCount > 6 {
		t.Errorf("INSTALL.md contains %d mentions of 'bypass'; want ≤ 6 (only sanctioned filename references + framing context per decisiones 17-h + 17-i)", bypassCount)
	}
}

func TestInvZenB8_InstallMDNoPrivateRepoRefs(t *testing.T) {
	t.Parallel()
	root := repoRootInvZenB8(t)
	body, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	content := string(body)

	forbidden := []string{
		"cbip-solutions/zen-bypass-tier1",
		"cbip-solutions/homebrew-private-tap",
		"brew tap cbip-solutions/private-tap",
		"brew install cbip-solutions/private-tap",
		"brew install cbip-solutions/homebrew-private-tap",
		"cbip-solutions/hades-system",
	}
	for _, ref := range forbidden {
		if strings.Contains(content, ref) {
			t.Errorf("INSTALL.md contains forbidden private/dev-repo reference %q (decisiones 4 + 11)", ref)
		}
	}
}

func TestInvZenB8_InstallMDCommunityRecipeLink(t *testing.T) {
	t.Parallel()
	root := repoRootInvZenB8(t)
	body, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	content := string(body)

	if !strings.Contains(content, "bypass-sidecar-recipe.md") {
		t.Error("INSTALL.md missing link to bypass-sidecar-recipe.md community recipe (decisión 17-i)")
	}
	if !strings.Contains(content, "community recipe") {
		t.Error("INSTALL.md missing 'community recipe' framing for the HTTP API contract doc (decisión 17-i)")
	}
}

func TestInvZenB8_InstallMDSidecarsTOMLDocumented(t *testing.T) {
	t.Parallel()
	root := repoRootInvZenB8(t)
	body, err := os.ReadFile(filepath.Join(root, "INSTALL.md"))
	if err != nil {
		t.Fatalf("read INSTALL.md: %v", err)
	}
	content := string(body)

	if !strings.Contains(content, "sidecars.toml") {
		t.Error("INSTALL.md missing 'sidecars.toml' discovery surface (decisión 17-d sidecar contract; decisión 17-i Tier 1 self-help UX)")
	}
	if !strings.Contains(content, "--with-sidecars-example") {
		t.Error("INSTALL.md missing '--with-sidecars-example' init flag reference (Phase B-5 operator-convenience seeder)")
	}
}

func repoRootInvZenB8(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := wd
	for {
		if fileExistsInvZenB8(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", wd)
		}
		dir = parent
	}
}

func fileExistsInvZenB8(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
