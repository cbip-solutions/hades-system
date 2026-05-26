// Package compliance — Plan 15 Phase B Task B-13 ADR migration sister-test.
//
// Per decisión 17-b (ADRs public by default, with a 4-bypass exception list),
// the four bypass-policy ADRs MUST migrate to the private bypass-tier repo:
//
//	ADR-0101 — Bypass refresh protocol (v0.17.8)
//	ADR-0102 — Bypass v0.17.9 fingerprint coexistence
//	ADR-0103 — Bypass v0.17.10 metadata.user_id
//	ADR-0104 — Bypass v0.17.11 response decompression + schema drift
//
// AND a new internal ADR `0001-bypass-tier-publication-policy.md` MUST exist
// in the private bypass-tier repo's own ADR namespace as the canonical
// authority for decisión 17 (the BYPASS POLICY BLOCK).
//
// Gate shape (defense-in-depth, two anchors):
//
//  1. **Absence in the public dev repo** — none of the four bypass ADR
//     filenames may exist under `docs/decisions/` in this repo. The
//     `make verify-no-bypass-references` boundary scanner + the
//     `docs/public-manifest/allowlist.yml` exclude list provide additional
//     defense in depth at the snapshot perimeter; this sister-test pins
//     the source-of-truth state.
//  2. **Presence in the private bypass repo** — when the env var
//     `ZEN_BYPASS_TIER1_ROOT` points at the private repo, the four migrated
//     ADRs + the new ADR-0001 policy file MUST be present. The check is
//     skipped without the env var so the test runs cleanly in the dev
//     repo's default `make test` lane; CI / cross-repo verification runs
//     supply the env var and exercise both halves.
//
// inv-zen-NNN placeholder; concrete ID allocated at Plan 15 merge-time
// reconciliation per the renumber-on-merge playbook (the placeholder
// `B13` is the Phase B task ID, NOT the final inv-zen number).
//
// File name uses `B13` as the placeholder because `B6` is already taken
// by `inv_zen_B6_split_table_test.go` (the migration-table sister-test
// that ships Phase B-6). Both gates remain orthogonal: B-6 partitions
// 12 invariants across the two repos; B-13 partitions 4 ADRs.
package compliance

import (
	"os"
	"path/filepath"
	"testing"
)

func repoRootB13(t *testing.T) string {
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
			t.Fatal("could not find go.mod root walking up from cwd")
		}
		root = parent
	}
}

// forbiddenBypassADRs is the canonical decisión 17-b exception list of
// bypass-policy ADRs that MUST live in the private bypass-tier repo, NOT
// in this public-default dev repo.
//
// Adding a new bypass-policy ADR to the dev repo MUST instead allocate it
// in the private bypass-tier repo's namespace (0002, 0003, …) and grow
// the list below at Phase B-13 cadence.
var forbiddenBypassADRs = []string{
	"0101-bypass-refresh-protocol.md",
	"0102-bypass-v0179-fingerprint-coexistence.md",
	"0103-bypass-v01710-metadata-user-id.md",
	"0104-bypass-response-decompression-and-schema-drift.md",
}

// requiredPrivateBypassADRs lists the ADRs that MUST be present in the
// private bypass-tier repo when the cross-repo env-var is set.
//
// `0001-bypass-tier-publication-policy.md` is the NEW internal ADR
// authored at Phase B-13 documenting decisión 17 as the canonical
// authority (the in-repo mirror of the project memory entry).
//
// The four `010N` files are the migrated ADRs, retaining their dev-repo
// IDs for historical traceability per decisión 17-b.
var requiredPrivateBypassADRs = []string{
	"0001-bypass-tier-publication-policy.md",
	"0101-bypass-refresh-protocol.md",
	"0102-bypass-v0179-fingerprint-coexistence.md",
	"0103-bypass-v01710-metadata-user-id.md",
	"0104-bypass-response-decompression-and-schema-drift.md",
}

// TestInvZenB13_BypassADRsAbsentFromPublicRepo asserts the 4 bypass
// ADRs are ABSENT from this repo's `docs/decisions/` per decisión 17-b.
//
// Re-introducing any of these files MUST also re-introduce them in the
// private bypass-tier repo and update this list — this gate fires loud
// if a file silently slips back into the public-default surface.
func TestInvZenB13_BypassADRsAbsentFromPublicRepo(t *testing.T) {
	root := repoRootB13(t)
	adrDir := filepath.Join(root, "docs", "decisions")

	for _, name := range forbiddenBypassADRs {
		path := filepath.Join(adrDir, name)
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				t.Errorf("bypass-policy ADR path %s exists as a directory; decisión 17-b forbids any presence in the public dev repo", path)
				continue
			}
			t.Errorf("bypass-policy ADR %s present at %s; MUST migrate to zen-bypass-tier1/docs/decisions/ per decisión 17-b (Phase B-13)", name, path)
		} else if !os.IsNotExist(err) {
			t.Errorf("stat(%s): %v (expected ENOENT per decisión 17-b)", path, err)
		}
	}
}

func TestInvZenB13_BypassADRsPresentInPrivateRepo(t *testing.T) {
	privateRoot := os.Getenv("ZEN_BYPASS_TIER1_ROOT")
	if privateRoot == "" {
		t.Skip("ZEN_BYPASS_TIER1_ROOT env var not set; cross-repo half of inv-zen-B13 exercised only in CI / manual cross-repo runs")
	}

	info, err := os.Stat(privateRoot)
	if err != nil {
		t.Fatalf("ZEN_BYPASS_TIER1_ROOT=%q: stat error: %v", privateRoot, err)
	}
	if !info.IsDir() {
		t.Fatalf("ZEN_BYPASS_TIER1_ROOT=%q must point at a directory; got non-directory entry", privateRoot)
	}

	adrDir := filepath.Join(privateRoot, "docs", "decisions")
	for _, name := range requiredPrivateBypassADRs {
		path := filepath.Join(adrDir, name)
		fi, statErr := os.Stat(path)
		if statErr != nil {
			t.Errorf("required ADR %s missing from zen-bypass-tier1/docs/decisions/ (path=%s, err=%v); decisión 17-b + Phase B-13 require this file in the private bypass-tier repo", name, path, statErr)
			continue
		}
		if fi.IsDir() {
			t.Errorf("required ADR path %s is a directory; must be a regular file", path)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("required ADR %s in private bypass-tier repo is empty; Phase B-13 migration must preserve content verbatim", path)
		}
	}
}
