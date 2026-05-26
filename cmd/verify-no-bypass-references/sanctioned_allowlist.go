// SPDX-License-Identifier: MIT
// cmd/verify-no-bypass-references — sanctioned-path allowlist for the
// 5-surface boundary scanner.
//
// PUBLIC SNAPSHOT IMPACT: every entry here flags a path that the dev repo
// LEGITIMATELY retains a "bypass" reference for. The Phase C-13 sync
// filter (scripts/build_public_snapshot.sh) consumes the same conceptual
// boundary by EXCLUDING the unsanctioned-public paths from the snapshot
// manifest (docs/public-manifest/allowlist.yml). Both gates enforce the
// same property: public-snapshot has zero unsanctioned bypass mentions.
//
// Allowlist rationale (decisión 17-a EXTENDED):
//
//  1. Daemon-side HTTP client to the localhost sidecar is NOT a bypass
//     implementation; it forwards via HTTP (decisión 17-d frozen contract).
//  2. The full private-tier1-module/** subtree is retained in the
//     dev repo per Stage-0 correction #4; Phase C-13 sync filter strips it
//     from the public snapshot.
//  3. `tests/` paths under `tests/compliance/bypass_*` or `tests/realworld/
//     bypass_*` or `tests/chaos/bypass_*` cover bypass tier residual
//     invariants; same Phase C-13 filtering applies for the public side.
//  4. `docs/operations/bypass-sidecar-recipe.md` is the public-facing
//     community recipe (decisión 17-i) documenting the HTTP API contract,
//     NOT the bypass implementation.
//  5. `docs/operations/bypass.md` (Plan 2 in-tree module handbook) +
//     `docs/operations/bypass-sidecar.md` (Plan 15 sidecar operator
//     handbook) are PRIVATE-only (Phase B-12 confirms both are excluded
//     from the public manifest; only `bypass-sidecar-recipe.md` is public).
//  6. ADRs 0101-0104 + 0118 are bypass-policy ADRs — private per
//     decisión 17-b (exception list); excluded by Phase C-13 sync filter.
//
// The allowlist is CONSERVATIVE: each entry has an explicit rationale
// string; reviewers MUST justify adding a new entry, never silently
// expand the surface.
package main

type AllowEntry struct {
	Path      string
	Rationale string
}

func defaultAllowlist() []AllowEntry {
	return []AllowEntry{

		{Path: "private-tier1-module/**",
			Rationale: "retained bypass tier subtree (Stage-0 correction #4; stripped by Phase C-13 sync)"},
		{Path: "internal/providers/sidecar_backend.go",
			Rationale: "daemon-side HTTP client to localhost sidecar (decisión 17-d frozen contract)"},
		{Path: "internal/providers/sidecar_backend_test.go",
			Rationale: "test pair for above"},
		{Path: "internal/providers/errors_sidecar.go",
			Rationale: "typed errors for sidecar fallback (inv-zen-280)"},
		{Path: "internal/daemon/dispatcheradapter/sidecar_registration.go",
			Rationale: "daemon-startup probe + register (inv-zen-066)"},
		{Path: "internal/daemon/dispatcheradapter/sidecar_registration_test.go",
			Rationale: "test pair"},
		{Path: "internal/config/sidecars.go",
			Rationale: "sidecars.toml loader (configs surface)"},
		{Path: "internal/config/sidecars_test.go",
			Rationale: "test pair"},
		{Path: "internal/cli/init.go",
			Rationale: "CLI --with-sidecars-example flag seeds sidecars.toml"},
		{Path: "internal/cli/init_test.go",
			Rationale: "test pair"},

		{Path: "internal/cli/bypass.go",
			Rationale: "CLI bypass extract-config command (decisión 17-h public-facing framing)"},
		{Path: "internal/cli/bypass_test.go",
			Rationale: "test pair"},
		{Path: "internal/cli/bypass_extract.go",
			Rationale: "CLI bypass extract-config helper"},
		{Path: "internal/cli/bypass_extract_test.go",
			Rationale: "test pair"},
		{Path: "internal/cli/bypass_cross_validate.go",
			Rationale: "CLI bypass cross-validate helper"},

		{Path: "internal/client/bypass.go",
			Rationale: "Go client mirror types for daemon bypass admin endpoints"},

		{Path: "internal/daemon/bypassadapter/**",
			Rationale: "daemon-side adapter that bridges store to bypass module (inv-zen-031 boundary)"},
		{Path: "internal/store/bypass_audit.go",
			Rationale: "daemon-only store ownership of bypass audit table (inv-zen-282)"},
		{Path: "internal/store/bypass_audit_test.go",
			Rationale: "test pair"},
		{Path: "internal/daemon/handlers/bypass_admin.go",
			Rationale: "daemon-side bypass admin endpoints (Plan 2 boundary; private surface)"},
		{Path: "internal/daemon/handlers/bypass_admin_test.go",
			Rationale: "test pair"},

		{Path: "internal/daemon/server.go",
			Rationale: "daemon-side wiring imports private-tier1-module per Stage-0 correction #4"},
		{Path: "internal/daemon/server_test.go",
			Rationale: "test pair"},
		{Path: "internal/daemon/handlers/bypass.go",
			Rationale: "daemon-side bypass admin endpoints (Plan 2 retained surface)"},
		{Path: "internal/daemon/handlers/bypass_test.go",
			Rationale: "test pair"},
		{Path: "internal/daemon/notifications.go",
			Rationale: "daemon notifications integration with bypass module"},
		{Path: "internal/daemon/notifications_test.go",
			Rationale: "test pair"},

		{Path: "internal/providers/bypass_backend.go",
			Rationale: "BypassBackend wraps private-tier1-module (inv-zen-066 frozen contract)"},
		{Path: "internal/providers/bypass_backend_test.go",
			Rationale: "test pair"},

		{Path: "cmd/zen-swarm-ctld/**",
			Rationale: "daemon-side wiring still imports private-tier1-module per Stage-0 correction #4"},

		{Path: "cmd/verify-no-bypass-references/**",
			Rationale: "the scanner's own source documents the forbidden tokens it catches"},

		{Path: "tests/testhelpers/**",
			Rationale: "test helpers spin daemon with bypass module under test"},
		{Path: "tests/testharness/**",
			Rationale: "test harness fakes mention bypass profile/path identifiers"},
		{Path: "tests/compliance/**",
			Rationale: "compliance tests scan repo for bypass-tier tokens as part of other invariants (visible strings, single egress, etc.) — boundary-scan only sanctioned via this scanner itself"},
		{Path: "tests/integration/**",
			Rationale: "integration tests legitimately invoke bypass profiles + onboarding flows"},
		{Path: "tests/realworld/**",
			Rationale: "real-world tests cover bypass tier smoke paths (private; not in public manifest)"},
		{Path: "tests/chaos/**",
			Rationale: "chaos tests exercise bypass tier resilience + sidecar boundary"},
		{Path: "tests/adversarial/**",
			Rationale: "adversarial tests reference bypass concepts (budget bypass, egress bypass) as different semantic uses"},
		{Path: "tests/testdata/**",
			Rationale: "test fixture corpora reference bypass in ADR/spec text"},
		{Path: "tests/orchestrator_chaos/**",
			Rationale: "orchestrator chaos tests reference bypass in chaos helpers"},
		{Path: "tests/replay/**",
			Rationale: "replay tests reference bypass profile names in scheduler scenarios"},
		{Path: "tests/release/**",
			Rationale: "release smoke tests verify bypass-tier surface stability"},
		{Path: "tests/property/**",
			Rationale: "property tests reference bypass-tier quota invariants"},
		{Path: "tests/doctrine/**",
			Rationale: "doctrine reconciliation tests reference bypass invariants"},
		{Path: "tests/timeaccel/**",
			Rationale: "time-accelerated tests reference bypass in quiet-hours scenarios"},
		{Path: "tests/property/quota_invariants_test.go",
			Rationale: "quota invariant tests reference bypass scenarios"},

		{Path: "configs/sidecars.toml.example",
			Rationale: "sidecars discovery config example (decisión 17-d sidecar surface)"},
		{Path: "configs/bypass-config.json.example",
			Rationale: "bypass-config.json schema example (private; not in public manifest)"},
		{Path: "configs/personal-references-allowlist.yaml",
			Rationale: "operator allowlist may reference bypass paths for J-4 sanitisation"},
		{Path: "configs/projects.toml.example",
			Rationale: "examples may reference bypass profile names (e.g., opus-bypass-in-house)"},

		{Path: "docs/operations/**",
			Rationale: "ops docs reference bypass-tier in dev-repo context; public-snapshot inclusion is selective via docs/public-manifest/allowlist.yml"},

		{Path: "docs/sbom/**",
			Rationale: "SBOM artifacts enumerate bypass dependency surface"},

		{Path: "docs/quality/**",
			Rationale: "quality audit artifacts reference bypass-tier (private dev artifact)"},
		{Path: "docs/operations/bypass-sidecar-recipe.md",
			Rationale: "decisión 17-i PUBLIC community recipe (HTTP API contract; explicit even within docs/operations/** subtree)"},
		{Path: "docs/operations/bypass-sidecar.md",
			Rationale: "Plan 15 sidecar operator handbook — PRIVATE per decisión 17-a + 17-i (excluded from public manifest; Phase B-12 author)"},
		{Path: "docs/operations/bypass.md",
			Rationale: "Plan 2 in-tree module handbook — PRIVATE per decisión 17-a (Phase B-12 confirms private-only; explicit)"},
		{Path: "docs/operations/bypass-changelog-curation-table.md",
			Rationale: "Plan 15 Phase B Task B-11 CHANGELOG curation table — PRIVATE per decisión 17-c (excluded from public manifest)"},

		{Path: "docs/decisions/0101-bypass-refresh-protocol.md",
			Rationale: "ADR-0101 bypass refresh protocol (private per 17-b exception list)"},
		{Path: "docs/decisions/0102-bypass-v0179-fingerprint-coexistence.md",
			Rationale: "ADR-0102 bypass v0.17.9 fingerprint coexistence (private)"},
		{Path: "docs/decisions/0103-bypass-v01710-metadata-user-id.md",
			Rationale: "ADR-0103 bypass v0.17.10 metadata user-id (private)"},
		{Path: "docs/decisions/0104-bypass-response-decompression-and-schema-drift.md",
			Rationale: "ADR-0104 bypass response decompression + schema drift (private)"},
		{Path: "docs/decisions/0118-bypass-tier-private-org.md",
			Rationale: "ADR-0118 bypass tier private-org canonical (decisión 14 sub-b)"},
		{Path: "docs/decisions/0100-hermes-llm-egress-tcp-listener-and-default-profile.md",
			Rationale: "ADR-0100 hermes egress mentions bypass for context (sanctioned public ADR)"},

		{Path: "docs/superpowers/**",
			Rationale: "plans + specs document Plan 2/15 bypass policy (private dev artifacts)"},
		{Path: "docs/release/**",
			Rationale: "release notes may reference bypass closure in J-* triage (private)"},
		{Path: "docs/decisions/**",
			Rationale: "ADRs may reference bypass for context (decisión 17-b default public; per-file exceptions covered above)"},

		{Path: "docs/METHODOLOGY.md",
			Rationale: "methodology doc references bypass as a working example"},
		{Path: "docs/public-manifest/allowlist.yml",
			Rationale: "manifest itself enumerates bypass exclude paths"},
	}
}

func testsAllowlist(all []AllowEntry) []AllowEntry {
	return filterAllowlist(all, "tests/")
}

func docsAllowlist(all []AllowEntry) []AllowEntry {
	return filterAllowlist(all, "docs/")
}

func configsAllowlist(all []AllowEntry) []AllowEntry {
	return filterAllowlist(all, "configs/")
}

func sqlMigrationsAllowlist(_ []AllowEntry) []AllowEntry {
	return nil
}

func filterAllowlist(all []AllowEntry, prefix string) []AllowEntry {
	out := make([]AllowEntry, 0, len(all))
	for _, e := range all {
		if len(e.Path) >= len(prefix) && e.Path[:len(prefix)] == prefix {
			out = append(out, e)
		}
	}
	return out
}
