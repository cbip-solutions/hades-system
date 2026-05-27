// tests/compliance/inv_zen_219_visible_strings_hades_test.go
//
// invariant — Visible-strings-HADES compliance scan.
//
// Doctrine: per spec §Q3 + master §"Invariant additions" + master §,
// the post- world MUST contain ZERO `zen-swarm` brand strings in
// user-visible output surfaces, with surgically-narrow borderline carve-outs
// per spec §Q3 BORDERLINE (config paths, socket, keychain prefix, env vars,
// daemon binary name, Go module path in imports only, historical CHANGELOG
// version entries, "(formerly zen-swarm)" first-mention footnote).
//
// Surfaces scanned:
//
// 1. Go files (AST-aware — distinguishes import statements from user-visible
// string literals):
// - internal/tui/views/*.go (13 panel views)
// - internal/tui/dashboard.go
// - internal/tui/styles.go
// - internal/tui/components/*.go (header glyph)
// - internal/cli/*.go (cobra Short/Long/Use/Example + printers)
// - cmd/zen-swarm-ctld/*.go (log format strings)
// - internal/onboard/qna/*.go (wizard prompts)
// 2. Markdown / YAML / Python files (regex-with-path-prefix-allowlist):
// - plugin/hades/**/*.{py,md,yaml}
// - README.md, AGENTS.md, CHANGELOG.md, llms.txt
// - docs/operations/*.md
//
// Forbidden patterns (case variants per spec §Q3 IN table):
// - "zen-swarm" (canonical hyphenated form)
// - "zen_swarm" (Python module underscore form)
// - "ZenSwarm" (Go-identifier CamelCase form — out-of-scope per
// spec §Q3 OUT for type names, but if it appears
// in a string-literal, that's a user-visible
// brand leak)
// - "Zen-Swarm" (Title-Case hyphenated form)
//
// Borderline allowlist (spec §Q3 BORDERLINE — these STAY in the codebase
// per the deferred-to-+N migration roadmap):
// 1. File path strings: "~/.config/zen-swarm/", "/tmp/zen-swarm.sock"
// — operator-script-compat; needs migration helper tooling.
// 2. Keychain service prefix: "zen-swarm/..." — re-provisioning all API
// keys is high friction; coordinated migration.
// 3. Env var names: "ZEN_*" (e.g., "ZEN_SKIP_*_HOOK", "ZEN_BYPASS_*") —
// operator-side environment files.
// 4. Daemon binary name: "zen-swarm-ctld" — process supervision configs
// (launchd, systemd) reference this.
// 5. Go module path: "github.com/cbip-solutions/hades-system/..." — IN IMPORT
// STATEMENTS ONLY. If the same string appears in a user-visible
// print call, that's a brand leak (caught by the AST visitor).
// 6. Historical CHANGELOG version entries: lines under `### [v0.X.Y]`
// headers BEFORE the current `[Unreleased]` section. Revisionist
// edits to historical narrative are forbidden.
// 7. "(formerly zen-swarm)" first-mention footnote in README: spec §Q3
// explicitly preserves this for search-engine legacy resolution.
// 8. ADR-0080 substrate-pivot narrative referencing the legacy product
// name as history: "zen-swarm migrated from OpenCode to Hermes" —
// the historical story IS about zen-swarm; rewriting is dishonest.
// 9. Plan/spec/HANDOFF historical pointers: internal design record +
// internal design record + HANDOFF.md historical sections.
//
// Test methodology:
// - Go files: go/ast + go/parser + go/token visit each BasicLit. Filter
// out import-statement BasicLits (those are spec §Q3 OUT Go-module-path
// carve-outs). Assert no remaining BasicLit contains a forbidden pattern.
// -.md/.yaml/.py files: regex scan line-by-line with path-prefix allowlist
// filtering. Each occurrence reports file:line + offending string +
// remediation hint.
//
// Test failure message format:
//
// "invariant violation: file X line Y contains forbidden brand string
// 'zen-swarm' outside borderline allowlist. Either rebrand or add
// explicit allowlist entry with rationale."
//
// Companion ADR: architecture records
// .
package compliance

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var forbiddenBrandPatterns_inv219 = []string{
	"zen-swarm",
	"zen_swarm",
	"Zen-Swarm",
	"ZenSwarm",
}

var goSourcePaths_inv219 = []string{
	"internal/tui/views",
	"internal/tui/dashboard.go",
	"internal/tui/styles.go",
	"internal/tui/components",
	"internal/cli",
	"cmd/zen-swarm-ctld",
	"internal/onboard/qna",
}

var docSourcePaths_inv219 = []string{
	"plugin/hades",
	"README.md",
	"AGENTS.md",
	"CHANGELOG.md",
	"llms.txt",
	"docs/operations",
}

var pathPrefixAllowlist_inv219 = []struct {
	prefix    string
	rationale string
}{
	{
		prefix:    "docs/superpowers/plans/",
		rationale: "plan/spec docs intentionally reference legacy product name as history (spec §Q3 OUT)",
	},
	{
		prefix:    "docs/superpowers/specs/",
		rationale: "spec docs intentionally reference legacy product name as history (spec §Q3 OUT)",
	},
	{
		prefix:    "docs/decisions/",
		rationale: "ADRs reference legacy product name in historical narrative (ADR-0080 substrate pivot story)",
	},
	{
		prefix:    "HANDOFF.md",
		rationale: "session-handoff doc references legacy product name in historical TL;DR sections (not user-visible product surface)",
	},
	{
		prefix:    "plugin/hades/",
		rationale: "spec §Q3 BORDERLINE: plugin source tree in active migration; Python files retain old module/logger/doc-string references that are internal plumbing, not operator-visible UI strings; full rebrand deferred to Plan 18c (coordinate with Hermes plugin contract spike)",
	},
}

var fileLineAllowlist_inv219 = []struct {
	path      string
	startLine int
	endLine   int
	rationale string
}{

	{
		path:      "internal/cli/adr_migrate.go",
		startLine: 30,
		endLine:   44,
		rationale: "spec §Q3 BORDERLINE: cobra Long help text says 'zen-swarm repo' referencing project repo name; operator-visible help but not brand-display surface",
	},

	{
		path:      "internal/cli/migrate_claude_code.go",
		startLine: 88,
		endLine:   90,
		rationale: "spec §Q3 BORDERLINE: cobra flag help strings reference $CWD/plugin/zen-swarm (legacy plugin path, deferred rename) and ~/.config/zen-swarm (XDG config path, deferred migration)",
	},
	{
		path:      "internal/cli/migrate_claude_code.go",
		startLine: 122,
		endLine:   141,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path components returning XDG state dir / legacy plugin dir paths; standalone \"zen-swarm\" is the dir-name segment",
	},

	{
		path:      "internal/cli/providers.go",
		startLine: 34,
		endLine:   36,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path component for ~/.config/zen-swarm/providers/ config dir",
	},
	{
		path:      "internal/cli/providers.go",
		startLine: 125,
		endLine:   185,
		rationale: "spec §Q3 BORDERLINE: TOML template string contains api_key_keychain = \"zen-swarm/<provider>\" keychain service path entries; keychain service names are frozen per keychain migration constraint",
	},

	{
		path:      "internal/cli/providers_extra.go",
		startLine: 76,
		endLine:   82,
		rationale: "spec §Q3 BORDERLINE: diagnostic error message references keychain service path zen-swarm/<provider>; same keychain-migration constraint",
	},

	{
		path:      "internal/cli/providers_keychain_darwin.go",
		startLine: 18,
		endLine:   42,
		rationale: "spec §Q3 BORDERLINE: SetAccount(\"zen-swarm\") is the macOS Keychain account field used to scope API key lookup; changing it invalidates all operator-provisioned API keys",
	},

	{
		path:      "internal/cli/recap.go",
		startLine: 161,
		endLine:   165,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path component for ~/.config/zen-swarm recap archive dir",
	},

	{
		path:      "internal/cli/state_xdg.go",
		startLine: 38,
		endLine:   52,
		rationale: "spec §Q3 BORDERLINE: cobra Long help text lists $XDG_STATE_HOME/zen-swarm subsystem paths; same XDG path migration constraint",
	},
	{
		path:      "internal/cli/state_xdg.go",
		startLine: 135,
		endLine:   140,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path components returning XDG state + cache dir paths",
	},

	{
		path:      "cmd/zen-swarm-ctld/bootstrap.go",
		startLine: 264,
		endLine:   268,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path component for ~/.config/zen-swarm/bypass-config.json",
	},

	{
		path:      "cmd/zen-swarm-ctld/main.go",
		startLine: 50,
		endLine:   53,
		rationale: "spec §Q3 BORDERLINE: flag default help string for --sqlite-path referencing ~/.local/share/zen-swarm/state.db XDG path",
	},

	{
		path:      "README.md",
		startLine: 78,
		endLine:   82,
		rationale: "spec §Q3 BORDERLINE: README dual-repo Modelo B sub-section references the private authoritative workspace at github.com/cbip-solutions/hades-system; the private repo name is the legacy identifier per decisión 14 sub-b",
	},
	{
		path:      "README.md",
		startLine: 190,
		endLine:   220,
		rationale: "spec §Q3 BORDERLINE: README Plan 11 historical narrative section references ZenSwarmTransport class name + Caronte cross-reference + ZenSwarm* internal type names",
	},
	{
		path:      "README.md",
		startLine: 260,
		endLine:   280,
		rationale: "spec §Q3 BORDERLINE: README Plan 9 audit chain quick-start uses zen-swarm as a project example in the --project flag arguments",
	},
	{
		path:      "README.md",
		startLine: 378,
		endLine:   385,
		rationale: "spec §Q3 BORDERLINE: README Plan 7 section introduction describes the plan shipping zen-swarm's ergonomics layer (historical narrative)",
	},
	{
		path:      "README.md",
		startLine: 420,
		endLine:   515,
		rationale: "spec §Q3 BORDERLINE: README Plan 7 substrate description references ~/.cache/zen-swarm/knowledge-index/index.db cache dir + ~/.config/zen-swarm/doctrines/ config dir (XDG path migration constraint)",
	},
	{
		path:      "README.md",
		startLine: 665,
		endLine:   710,
		rationale: "spec §Q3 BORDERLINE: README master plan spec link uses historical 2026-04-30-zen-swarm-plan-3-orchestrator filename + binary names zen + zen-swarm-ctld + slash command historical names",
	},
	{
		path:      "README.md",
		startLine: 720,
		endLine:   735,
		rationale: "spec §Q3 OUT: README Plans 1-20 status list cites historical spec/plan filenames using zen-swarm-* prefix; links are historical record cross-references",
	},
	{
		path:      "README.md",
		startLine: 736,
		endLine:   755,
		rationale: "spec §Q3 OUT: README Plan 18 deprecation roadmap (lines covering trilogy ship + v0.20.0+ borderline boundary + ZenSwarmDispatcher OUT-of-scope items per spec §10)",
	},
	{
		path:      "README.md",
		startLine: 756,
		endLine:   780,
		rationale: "spec §Q3 BORDERLINE: README build/install sections reference bin/zen + bin/zen-swarm-ctld binary names (frozen carve-outs per spec §Q3 BORDERLINE)",
	},
	{
		path:      "README.md",
		startLine: 840,
		endLine:   890,
		rationale: "spec §Q3 BORDERLINE: README plugin + develop sections reference plugin/zen-swarm/ legacy filesystem path (renamed in Plan 18b but cross-referenced for history) + zen-swarm Go module path",
	},

	{
		path:      "AGENTS.md",
		startLine: 1,
		endLine:   5,
		rationale: "spec §Q3 BORDERLINE: AGENTS.md YAML frontmatter project field + preamble; repository metadata",
	},
	{
		path:      "AGENTS.md",
		startLine: 8,
		endLine:   12,
		rationale: "spec §Q3 BORDERLINE: AGENTS.md formerly footnote + GitHub org narrative in repo identity section",
	},
	{
		path:      "AGENTS.md",
		startLine: 20,
		endLine:   35,
		rationale: "spec §Q3 BORDERLINE: AGENTS.md continuation surface names the repository cwd and legacy Hermes slash-command namespace; private operator memory must preserve these exact identifiers",
	},
	{
		path:      "AGENTS.md",
		startLine: 42,
		endLine:   45,
		rationale: "spec §Q3 BORDERLINE: AGENTS.md private/public repository identity map references canonical GitHub repo names; repo rename is a separate coordinated decision",
	},
	{
		path:      "AGENTS.md",
		startLine: 88,
		endLine:   97,
		rationale: "spec §Q3 BORDERLINE: AGENTS.md memory section records Codex/Claude project-memory paths; private traceability doctrine requires preserving exact local identifiers",
	},
	// ── CHANGELOG.md ──────────────────────────────────────────────────────
	// (v0.17.3) CHANGELOG historical entries are now exempted DYNAMICALLY by
	// scanDocSurface_inv219: every line at/after the 2nd `## [vX.Y.Z]` release
	// header is a shipped-release entry (historical narrative) and is skipped.
	// This replaces the per-line CHANGELOG carve-outs that used to live here —
	// they shifted on every release (verify-invariants shipped RED on v0.17.1
	// when the [v0.17.1] section moved the intros out of range).
	// Do NOT re-add line-anchored CHANGELOG entries; author the NEWEST release
	// entry brand-clean (substring carve-outs like `zen-swarm-ctld`,
	// `/tmp/zen-swarm.sock`, `com.zen-swarm.` still apply via the substring
	// allowlist). Gated by TestInvZen219_ChangelogDynamicHistoricalExemption.
	// ── llms.txt ─────────────────────────────────────────────────────────
	{
		path:      "llms.txt",
		startLine: 1,
		endLine:   15,
		rationale: "spec §Q3 BORDERLINE: llms.txt header section with formerly footnote and spec file cross-references; operator-facing product description page",
	},

	{
		path:      "docs/operations/cli-help-snapshots/v0_7_0_root.txt",
		startLine: 40,
		endLine:   44,
		rationale: "spec §Q3 OUT: historical CLI help snapshot at v0.7.0; frozen artifact not updated (operator reference for version drift detection)",
	},
	{
		path:      "docs/operations/cli-help-snapshots/v0_7_0_schedule.txt",
		startLine: 1,
		endLine:   3,
		rationale: "spec §Q3 OUT: historical CLI help snapshot at v0.7.0; frozen artifact",
	},

	{
		path:      "docs/operations/audit.md",
		startLine: 163,
		endLine:   182,
		rationale: "spec §Q3 BORDERLINE: audit.md CLI recovery walkthrough example uses zen-swarm as sample project ID; CLI output mockup",
	},

	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 55,
		endLine:   62,
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md comparison table row for legacy zen-swarm direct entry (still supported path)",
	},
	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 110,
		endLine:   118,
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md describes legacy ~/.hermes/plugins/zen-swarm/ paths (backward compat note)",
	},
	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 143,
		endLine:   147,
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md Plan 18a historical note about pre-rebrand dashboard strings",
	},
	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 294,
		endLine:   313,
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md Plan 18b scope description listing slash-command namespace rebrand and compliance invariant (range +15 after v0.17.2 §4.2 rewrite, ADR-0099)",
	},
	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 323,
		endLine:   329,
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md future migration command flag --from-zen-swarm-aliases (range +15 after v0.17.2 §4.2 rewrite, ADR-0099)",
	},

	{
		path:      "docs/operations/phase-b-empirical-baseline.md",
		startLine: 1,
		endLine:   180,
		rationale: "spec §Q3 BORDERLINE: phase-b-empirical-baseline.md is a Phase B-1 read-only audit deliverable that documents the dev-repo URL `cbip-solutions/hades-system` (decisión 14 sub-b Modelo B authoritative-private-dev preserves the name) + bypass-tier-1 org drift cbip-solutions vs cbip-solutions; references are factual identity, not brand surface (range widened to 180 to cover the post-ADR-0118 RESOLVED annotation)",
	},

	{
		path:      "docs/operations/bypass-changelog-curation-table.md",
		startLine: 1,
		endLine:   280,
		rationale: "spec §Q3 BORDERLINE: bypass-changelog-curation-table.md is a Plan 15 Phase B Task B-11 PRIVATE-ONLY deliverable (decisión 17-c) that documents the per-version KEEP-summarized/STRIP/OMIT partition + necessarily references the dev-repo URL `cbip-solutions/hades-system` (decisión 14 sub-b Modelo B preserves the name) to explain Phase C-13 sync-filter source + dual-repo topology; references are factual identity, not brand surface",
	},

	{
		path:      "docs/decisions/0118-bypass-tier-private-org.md",
		startLine: 1,
		endLine:   150,
		rationale: "spec §Q3 BORDERLINE: ADR-0118 documents the bypass-tier-private-org decision and necessarily references the dev-repo URL `cbip-solutions/hades-system` to explain why the bypass-tier deliberately diverges; same allowlist rationale pattern as the Phase B-1 baseline doc",
	},

	{
		path:      "docs/operations/plan-7.md",
		startLine: 87,
		endLine:   92,
		rationale: "spec §Q3 BORDERLINE: plan-7.md CLI example `zen attach zen-swarm` using project alias",
	},
	{
		path:      "docs/operations/plan-7.md",
		startLine: 193,
		endLine:   198,
		rationale: "spec §Q3 BORDERLINE: plan-7.md TUI session table row with zen-swarm project alias",
	},
	{
		path:      "docs/operations/plan-7.md",
		startLine: 317,
		endLine:   322,
		rationale: "spec §Q3 BORDERLINE: plan-7.md inbox table row with zen-swarm project alias",
	},
	{
		path:      "docs/operations/plan-7.md",
		startLine: 378,
		endLine:   384,
		rationale: "spec §Q3 BORDERLINE: plan-7.md morning digest example with zen-swarm project alias",
	},
	{
		path:      "docs/operations/plan-7.md",
		startLine: 495,
		endLine:   500,
		rationale: "spec §Q3 BORDERLINE: plan-7.md doctor command example using zen-swarm project",
	},
	{
		path:      "docs/operations/plan-7.md",
		startLine: 642,
		endLine:   647,
		rationale: "spec §Q3 BORDERLINE: plan-7.md `zen project priority --boost zen-swarm` example CLI command",
	},

	{
		path:      "docs/operations/plugin-hermes.md",
		startLine: 103,
		endLine:   108,
		rationale: "spec §Q3 BORDERLINE: plugin-hermes.md debugging example tailing /tmp/zen-swarm.log",
	},

	{
		path:      "docs/operations/research.md",
		startLine: 57,
		endLine:   62,
		rationale: "spec §Q3 BORDERLINE: research.md CLI example `zen research dispatch \"zen-swarm architecture patterns\"` using project as query topic",
	},

	{
		path:      "docs/operations/doctrine.md",
		startLine: 345,
		endLine:   350,
		rationale: "spec §Q3 BORDERLINE: doctrine.md Go template example comment showing zen-swarm as sample ProjectAlias value",
	},

	{
		path:      "docs/operations/gitnexus.md",
		startLine: 14,
		endLine:   20,
		rationale: "spec §Q3 BORDERLINE: gitnexus.md architecture section describing ZenSwarmTransport class (Phase B deliverable)",
	},
	{
		path:      "docs/operations/gitnexus.md",
		startLine: 79,
		endLine:   85,
		rationale: "spec §Q3 BORDERLINE: gitnexus.md smoke-test checklist item for ZenSwarmTransport",
	},
	{
		path:      "docs/operations/gitnexus.md",
		startLine: 227,
		endLine:   232,
		rationale: "spec §Q3 BORDERLINE: gitnexus.md describes zen_swarm_transport.py Python module filename",
	},

	{
		path:      "docs/operations/hades-entry-point.md",
		startLine: 396,
		endLine:   406,
		rationale: "spec §Q3 BORDERLINE: migration tooling --include-aliases section must show examples of bare zen-swarm alias references to explain the broader scope opt-in; source-path reference, plan 18c Phase F (range +15 after v0.17.2 §4.2 rewrite, ADR-0099)",
	},
	// ── internal/cli/migrate_plan18.go ────────────────────────────────────
	// The migrate plan-18 tool inherently contains zen-swarm references:
	// (1) regex detection patterns that identify legacy /zen-swarm: references;
	// (2) replacement target strings ("zen-swarm" → "hades");
	// (3) default backup path component (XDG data dir for backups);
	// (4) flag name "from-zen-swarm-aliases" + help strings naming what is migrated.
	// All are spec §Q3 BORDERLINE: the migration tool MUST know what it is
	// replacing; these strings are the source-path reference per plan §F
	// "BORDERLINE-stays per spec §Q3".
	{
		path:      "internal/cli/migrate_plan18.go",
		startLine: 140,
		endLine:   147,
		rationale: "spec §Q3 BORDERLINE: regex patterns aliasRefRegex + aliasUnquotedRegex must contain the legacy zen-swarm token as detection target; migration tool cannot function without knowing what to detect (source-path ref, plan 18c Phase F)",
	},
	{
		path:      "internal/cli/migrate_plan18.go",
		startLine: 514,
		endLine:   529,
		rationale: "spec §Q3 BORDERLINE: replaceLine function body uses literal string 'zen-swarm' as the replacement source token in ReplaceAll calls; the migration tool cannot rewrite zen-swarm→hades without naming the source",
	},
	{
		path:      "internal/cli/migrate_plan18.go",
		startLine: 619,
		endLine:   623,
		rationale: "spec §Q3 BORDERLINE: filepath.Join path component for ~/.local/share/zen-swarm/migrate-plan-18-backup/ default backup root; XDG data dir migration deferred to Plan 18+N per spec §10 OUT-list",
	},
	{
		path:      "internal/cli/migrate_plan18.go",
		startLine: 905,
		endLine:   922,
		rationale: "spec §Q3 BORDERLINE: cobra flag registrations — 'from-zen-swarm-aliases' flag name + help strings explicitly name the legacy slash-command namespace being migrated; operator-explicit acknowledgement per spec §Q4; plan 18c Phase F BORDERLINE-stays decision",
	},

	{
		path:      "docs/operations/hermes-compat.md",
		startLine: 95,
		endLine:   95,
		rationale: "spec §Q3 BORDERLINE: documents Modelo B dual-repo flow (decisión 14 sub-b) where private dev repo IS `cbip-solutions/hades-system` (zero-migration); legitimate dev-repo reference for hot-fix workflow",
	},

	{
		path:      "docs/operations/chaos-engineering.md",
		startLine: 394,
		endLine:   394,
		rationale: "spec §Q3 BORDERLINE: historical spec file path reference `2026-05-15-zen-swarm-plan-15-release-polish-design.md` — file path is immutable once authored; doc cites canonical source for traceability",
	},
}

var substringAllowlist_inv219 = []struct {
	substring string
	rationale string
}{
	{
		substring: "~/.config/zen-swarm/",
		rationale: "spec §Q3 BORDERLINE: config dir path; operator-script-compat; deferred to Plan 18+N migration",
	},
	{
		substring: ".config/zen-swarm/",
		rationale: "spec §Q3 BORDERLINE: config dir path (relative-to-HOME form); operator-script-compat",
	},
	{
		substring: "~/.config/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: config dir path (bare form, no trailing slash); appears in cobra flag-default help strings like \"default: ~/.config/zen-swarm\"; placed after slash forms so directory-prefix variants strip first",
	},
	{
		substring: "/tmp/zen-swarm.sock",
		rationale: "spec §Q3 BORDERLINE: UDS socket path; hardcoded in operator workflows + external tooling",
	},
	{
		substring: "zen-swarm-ctld",
		rationale: "spec §Q3 BORDERLINE: daemon binary name; process supervision configs (launchd, systemd)",
	},
	{
		substring: "buildZenSwarmCtldBinary",
		rationale: "spec §Q3 BORDERLINE: Go helper-function symbol name in tests/integration/plan18b/helpers_test.go; legitimate identifier reference in archaeological CHANGELOG narrative (inv-zen-286 fix history); function-name rename deferred to Plan 15 Phase C public-flip",
	},
	{
		substring: "ZEN_",
		rationale: "spec §Q3 BORDERLINE: env var name prefix; operator-side environment files (ZEN_SKIP_*_HOOK, ZEN_BYPASS_*, etc.)",
	},
	{
		substring: "github.com/cbip-solutions/hades-system",
		rationale: "spec §Q3 OUT: Go module path; rename is major refactor (Plan 22+)",
	},
	{
		substring: "(formerly zen-swarm)",
		rationale: "spec §Q3 BORDERLINE preservation: first-mention footnote in README; search-engine legacy resolution",
	},
	{
		substring: "Keychain service prefix `zen-swarm/",
		rationale: "spec §Q3 BORDERLINE: keychain prefix migration deferred (re-provisioning friction)",
	},
	{
		substring: "keychain service prefix `zen-swarm/",
		rationale: "same as above; lower-case sentence-position variant",
	},
	{
		substring: "zen-swarm/*",
		rationale: "spec §Q3 BORDERLINE: keychain glob form referenced in docs",
	},
	{
		substring: "(zen-swarm)",
		rationale: "borderline carve-out in handbook narrative + docstrings (binary-name-in-parens for grep-ability per master §Phase F decision)",
	},

	{
		substring: "/.local/share/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: XDG_DATA_HOME dir path; operator data migration required (Plan 18+N); changing live paths mid-session destroys operator data",
	},
	{
		substring: "/.local/state/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: XDG_STATE_HOME dir path; operator state migration required; renaming live state dir risks data loss",
	},
	{
		substring: "/.cache/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: XDG_CACHE_HOME dir path; operator cache migration deferred; non-critical but coordinated rename required",
	},
	{
		substring: "/.zen-swarm/",
		rationale: "spec §Q3 BORDERLINE: legacy dot-dir (~/.zen-swarm/) used by safetynet post-tag hook and ssh-guard deployment; filesystem migration deferred",
	},
	{
		substring: ".zen-swarm/prev-binary",
		rationale: "spec §Q3 BORDERLINE: safetynet post-tag hook dot-dir path .zen-swarm/prev-binary-sha; same dot-dir migration constraint",
	},

	{
		substring: "/homebrew/share/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: Homebrew formula install prefix for daemon scripts; process supervision (launchd) references this path at install time",
	},
	{
		substring: "/usr/local/share/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: system install prefix for daemon scripts; process supervision compatibility path",
	},

	{
		substring: "zen-swarm-bypass",
		rationale: "spec §Q3 BORDERLINE: macOS Keychain entry name for bypass config; re-provisioning requires coordinated operator action",
	},
	{
		substring: "\"zen-swarm/<provider>\"",
		rationale: "spec §Q3 BORDERLINE: Keychain service path template in provider init/verify CLI help strings; same keychain migration fence as zen-swarm/* glob",
	},
	{
		substring: "zen-swarm/<provider>",
		rationale: "spec §Q3 BORDERLINE: Keychain service path template (unquoted form) in CLI help strings",
	},
	{
		substring: "SetAccount(\"zen-swarm\")",
		rationale: "spec §Q3 BORDERLINE: macOS Keychain account field literal used as fixed-string discriminator; changing it invalidates all provisioned API keys",
	},
	{
		substring: "account: zen-swarm",
		rationale: "spec §Q3 BORDERLINE: Keychain account field in doc examples; same migration constraint",
	},

	{
		substring: "dev.zen-swarm.ctld.",
		rationale: "spec §Q3 BORDERLINE: macOS CFBundleIdentifier prefix for zen:// URL scheme registration; OS-level identifier requiring org-cert re-registration",
	},
	{
		substring: "com.zen-swarm.",
		rationale: "spec §Q3 BORDERLINE: launchd plist label prefix (com.zen-swarm.ctld, com.zen-swarm.docs-cron); changing requires re-install of launchd agents",
	},

	{
		substring: "plugin-zen-swarm-loaded",
		rationale: "spec §Q3 BORDERLINE: Hermes doctor probe name and result key; external Hermes config/API identifier requires Hermes contract renegotiation",
	},
	{
		substring: "mcp_servers.zen-swarm",
		rationale: "spec §Q3 BORDERLINE: Hermes config.yaml MCP server key; external Hermes config identifier",
	},
	{
		substring: "mcp_zen-swarm_",
		rationale: "spec §Q3 BORDERLINE: MCP tool name prefix registered in daemon RBAC; external identifier referenced by Hermes config",
	},

	{
		substring: "plugin/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: legacy plugin path referenced in doctor diagnostics, CHANGELOG historical entries, and operator coordination docs; migration documented",
	},

	{
		substring: "--project zen-swarm",
		rationale: "spec §Q3 BORDERLINE: cobra Example strings use zen-swarm as sample project name (operator tooling docs; project IDs are arbitrary strings)",
	},
	{
		substring: "project zen-swarm",
		rationale: "spec §Q3 BORDERLINE: cobra Example and doc output using zen-swarm as sample project name in tabular display",
	},

	{
		substring: "zen-swarm-system-design",
		rationale: "spec §Q3 OUT: historical spec filename referenced in CLI help strings and docs cross-references; filenames are historical records (not renamed)",
	},
	{
		substring: "zen-swarm-plan-",
		rationale: "spec §Q3 OUT: historical spec/plan filenames referenced in docs cross-references; legacy filenames preserved",
	},
	{
		substring: "zen-swarm-spike-",
		rationale: "spec §Q3 OUT: historical spike spec filename referenced in docs and plugin code; legacy filename preserved",
	},
	{
		substring: "zen-swarm-design",
		rationale: "spec §Q3 OUT: historical base design spec filename (2026-04-29-zen-swarm-design.md) referenced in docs",
	},
	{
		substring: "zen-swarm-bootstrap",
		rationale: "spec §Q3 OUT: cobra Example string using zen-swarm-bootstrap as sample spec name",
	},
	{
		substring: "zen-swarm-sha256",
		rationale: "spec §Q3 BORDERLINE: test fixture project_id used in AFK/aggregator test conftest/fixtures; test infrastructure not operator-visible output",
	},

	{
		substring: "s3://zen-swarm-audit",
		rationale: "spec §Q3 BORDERLINE: S3 bucket name prefix for audit cold archive; bucket rename requires data migration + AWS IAM policy updates",
	},
	{
		substring: "zen-swarm-audit-s3",
		rationale: "spec §Q3 BORDERLINE: macOS Keychain entry name for S3 credentials; part of Plan 2 Keychain pattern",
	},
	{
		substring: "zen-swarm-litestream",
		rationale: "spec §Q3 BORDERLINE: temporary litestream config filename prefix in /tmp/; daemon-internal path not operator-visible",
	},

	{
		substring: "zen-swarm-p",
		rationale: "spec §Q3 OUT: historical git worktree directory names (zen-swarm-p3, zen-swarm-p5, etc.) in parallel-execution-coordination.md; historical operational narrative",
	},

	{
		substring: "zen-swarm/0.",
		rationale: "spec §Q3 BORDERLINE: HTTP User-Agent header using product/version form; external protocol identifier",
	},

	{
		substring: "/projects/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: operator filesystem path to project checkout referenced in operational docs and skill SKILL.md files",
	},
	{
		substring: "cwd_starts_with: /path/to/projects/hades-system",
		rationale: "spec §Q3 BORDERLINE: Hermes SKILL.md activation condition referencing project path; internal skill routing config",
	},
	{
		substring: "cwd_contains: zen-swarm",
		rationale: "spec §Q3 BORDERLINE: Hermes SKILL.md activation condition; internal skill routing config",
	},

	{
		substring: "github.com/cbip-solutions/hades-system",
		rationale: "spec §Q3 OUT: GitHub repo URL in docs/README; repo rename is a separate decision (Plan 22+)",
	},
	{
		substring: "zen-swarm org",
		rationale: "spec §Q3 BORDERLINE: GitHub org narrative in README (org does not exist; L-1 fallback note)",
	},
	{
		substring: "zen-swarm repos",
		rationale: "spec §Q3 BORDERLINE: GitHub org narrative in README about private repos",
	},

	{
		substring: "[zen-swarm]",
		rationale: "spec §Q3 BORDERLINE: TOML section header in system-state.toml; changing breaks operator-authored pin files and CI gate scripts",
	},

	{
		substring: "zen-swarm: ",
		rationale: "spec §Q3 BORDERLINE: Python logger prefix and hook warning prefix (e.g. 'zen-swarm hook: ...'); logger names are internal plumbing",
	},
	{
		substring: "brew install zen-swarm",
		rationale: "spec §Q3 BORDERLINE: Homebrew install command in README; brew tap rename is a separate release decision",
	},
	{
		substring: "hermes-config-snippet",
		rationale: "doc filename reference — does not itself contain the forbidden brand; kept as no-op guard",
	},
	{
		substring: "zen-swarm-bypass entry",
		rationale: "spec §Q3 BORDERLINE: diagnostic message pointing operator to Keychain bypass entry; same as zen-swarm-bypass carve-out above",
	},

	{
		substring: "ZenSwarmTransport",
		rationale: "spec §Q3 BORDERLINE: Python class name for Hermes transport plugin; external Hermes plugin API identifier preserved for backward compat",
	},

	{
		substring: "/zen-swarm:",
		rationale: "spec §Q3 BORDERLINE: legacy slash command namespace referenced in migration narrative and CHANGELOG historical entries; transition documented",
	},

	{
		substring: "project: zen-swarm",
		rationale: "spec §Q3 BORDERLINE: AGENTS.md frontmatter field naming the repository; changing requires coordinated CI + operator tooling updates",
	},

	{
		substring: "zen_swarm_transport.py",
		rationale: "spec §Q3 BORDERLINE: Python transport module filename; changing file breaks Hermes plugin contract and import chain",
	},

	{
		substring: "/tmp/zen-swarm.log",
		rationale: "spec §Q3 BORDERLINE: daemon log file path in /tmp; operator tooling scripts tail this path",
	},

	{
		substring: "--from-zen-swarm-aliases",
		rationale: "spec §Q3 BORDERLINE: future migration command flag name that explicitly describes what it migrates; flag name is self-documenting",
	},

	{
		substring: "zen attach zen-swarm",
		rationale: "spec §Q3 BORDERLINE: cobra Example string in multi-project UX doc; zen-swarm used as sample project alias",
	},

	{
		substring: "cli-help-snapshots",
		rationale: "doc path tag — the actual filtering happens via fileLineAllowlist_inv219 entries below",
	},

	{
		substring: "zen-swarm core",
		rationale: "spec §Q3 BORDERLINE: license narrative in README/CHANGELOG referencing the core library by its package name",
	},

	{
		substring: "no `zen-swarm` string in HADES",
		rationale: "spec §Q3 BORDERLINE: CHANGELOG Plan 18b section describes the compliance invariant in narrative; meta-reference to the forbidden string",
	},
	{
		substring: "→ `/hades:*`",
		rationale: "spec §Q3 BORDERLINE: CHANGELOG migration description of slash-command namespace cutover; historical narrative of what changed",
	},
	{
		substring: "/hades:*` hard cutover",
		rationale: "spec §Q3 BORDERLINE: CHANGELOG Plan 18b section describes hard cutover; same transition narrative",
	},

	{
		substring: "using-zen-swarm",
		rationale: "spec §Q3 BORDERLINE: skill file name in CHANGELOG historical entry; skill filenames are not renamed (operator skill references)",
	},

	{
		substring: "zen-swarm/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: GitHub repo identifier in AGENTS.md and README; repo rename is a separate decision",
	},

	{
		substring: "zen-swarm doctor",
		rationale: "spec §Q3 BORDERLINE: plan-7.md TUI mockup example showing zen doctor command for the zen-swarm project; CLI example output",
	},
	{
		substring: "zen-swarm architecture patterns",
		rationale: "spec §Q3 BORDERLINE: cobra Example in research.md using zen-swarm as sample research query topic",
	},

	{
		substring: "Legacy zen-swarm direct entry",
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md comparison table row describing legacy access path",
	},
	{
		substring: "pre-rebrand \"zen-swarm\" strings",
		rationale: "spec §Q3 BORDERLINE: hades-entry-point.md historical note about Plan 18a state; refers to the pre-rebrand brand as a label",
	},

	{
		substring: "\"internal-platform-x\" / \"zen-swarm\"",
		rationale: "spec §Q3 BORDERLINE: doctrine.md Go template example comment using zen-swarm as sample project alias value",
	},

	{
		substring: "~/.hermes/plugins/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: legacy Hermes plugin install path still supported; migration path documented in hades-entry-point.md",
	},

	{
		substring: "STATE_HOME/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: $XDG_STATE_HOME/zen-swarm dir form in docs; same XDG state path migration constraint as /.local/state/zen-swarm entry",
	},
	{
		substring: "CACHE_HOME/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: $XDG_CACHE_HOME/zen-swarm dir form in docs; same XDG cache path migration constraint",
	},
	{
		substring: "DATA_HOME/zen-swarm",
		rationale: "spec §Q3 BORDERLINE: $XDG_DATA_HOME/zen-swarm dir form in docs; same XDG data path migration constraint",
	},

	{
		substring: "zen-swarm-gitnexus-integration-design",
		rationale: "spec §Q3 OUT: historical spec filename for gitnexus integration design; spec filenames are historical records preserved as-is",
	},
}

func collectImportLitPositions_inv219(t *testing.T, fset *token.FileSet, f *ast.File) map[token.Pos]struct{} {
	t.Helper()
	out := make(map[token.Pos]struct{})
	for _, imp := range f.Imports {
		if imp.Path != nil {
			out[imp.Path.Pos()] = struct{}{}
		}
	}
	return out
}

func collectFilepathJoinZenSwarmArgs_inv219(f *ast.File) map[token.Pos]struct{} {
	out := make(map[token.Pos]struct{})
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "filepath" || sel.Sel.Name != "Join" {
			return true
		}

		for _, arg := range call.Args {
			bl, ok := arg.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				continue
			}

			if bl.Value == `"zen-swarm"` {
				out[bl.Pos()] = struct{}{}
			}
		}
		return true
	})
	return out
}

func allowedBySubstring_inv219(body string) (substring, rationale string, ok bool) {
	for _, entry := range substringAllowlist_inv219 {
		if strings.Contains(body, entry.substring) {
			return entry.substring, entry.rationale, true
		}
	}
	return "", "", false
}

func allowedByPathPrefix_inv219(relPath string) (prefix, rationale string, ok bool) {
	for _, entry := range pathPrefixAllowlist_inv219 {
		if strings.HasPrefix(relPath, entry.prefix) {
			return entry.prefix, entry.rationale, true
		}
	}
	return "", "", false
}

func allowedByFileLine_inv219(relPath string, lineno int) (rationale string, ok bool) {
	for _, entry := range fileLineAllowlist_inv219 {
		if entry.path == relPath && lineno >= entry.startLine && lineno <= entry.endLine {
			return entry.rationale, true
		}
	}
	return "", false
}

func containsForbiddenBrand_inv219(body string) (string, bool) {
	for _, pat := range forbiddenBrandPatterns_inv219 {
		if strings.Contains(body, pat) {
			return pat, true
		}
	}
	return "", false
}

func scanGoSurface_inv219(t *testing.T, root, relPath string) []string {
	t.Helper()
	fullPath := filepath.Join(root, relPath)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("inv-zen-219: parse %s: %v", relPath, err)
	}
	importPositions := collectImportLitPositions_inv219(t, fset, f)

	filepathJoinZenArgs := collectFilepathJoinZenSwarmArgs_inv219(f)

	var violations []string
	ast.Inspect(f, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}

		if _, isImport := importPositions[bl.Pos()]; isImport {
			return true
		}

		if _, _, allowed := allowedBySubstring_inv219(bl.Value); allowed {
			return true
		}

		if _, isJoinArg := filepathJoinZenArgs[bl.Pos()]; isJoinArg {
			return true
		}

		pos := fset.Position(bl.Pos())
		if _, allowed := allowedByFileLine_inv219(relPath, pos.Line); allowed {
			return true
		}

		if pat, found := containsForbiddenBrand_inv219(bl.Value); found {
			violations = append(violations,
				"  - "+relPath+":"+itoa_inv219(pos.Line)+
					": forbidden=\""+pat+"\" in literal="+truncateForReport_inv219(bl.Value, 80)+
					" | rebrand or add allowlist entry with rationale")
		}
		return true
	})
	return violations
}

// TestInvZen219_ChangelogDynamicHistoricalExemption gates the v0.17.3 dynamic
// CHANGELOG historical exemption that replaces the fragile line-anchored
// carve-outs (every release shifted them — verify-invariants shipped RED on
//
// Rule: lines at/after the SECOND `## [vX.Y.Z]` release header are immutable
// historical narrative (a shipped release entry) → exempt. Only the preamble
// + the NEWEST release entry are brand-scanned. This test gates BOTH
// directions: a leak in the newest entry MUST flag; a "zen-swarm" in an older
// entry MUST be exempt.
func TestInvZen219_ChangelogDynamicHistoricalExemption(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		"# Changelog",
		"",
		"## [v0.2.0] — 2026 — newest (NEW content)",
		"newest body mentions zen-swarm here", // 4 → MUST be flagged
		"",
		"## [v0.1.0] — 2026 — older (historical)",
		"older body mentions zen-swarm and is exempt", // 7 → MUST NOT be flagged
	}
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations := scanDocSurface_inv219(t, dir, "CHANGELOG.md")
	var newestFlagged, olderFlagged bool
	for _, v := range violations {
		if strings.Contains(v, "CHANGELOG.md:4") {
			newestFlagged = true
		}
		if strings.Contains(v, "CHANGELOG.md:7") {
			olderFlagged = true
		}
	}
	if !newestFlagged {
		t.Errorf("newest-entry brand leak (line 4) not flagged — the NEWEST release entry MUST be brand-scanned\nviolations: %v", violations)
	}
	if olderFlagged {
		t.Errorf("older historical entry (line 7) flagged — lines at/after the 2nd release header MUST be exempt (dynamic v0.17.3 rule)\nviolations: %v", violations)
	}
}

var changelogReleaseHeaderRe_inv219 = regexp.MustCompile(`^##\s+\[v`)

func scanDocSurface_inv219(t *testing.T, root, relPath string) []string {
	t.Helper()
	fullPath := filepath.Join(root, relPath)
	f, err := os.Open(fullPath)
	if err != nil {
		t.Fatalf("inv-zen-219: open %s: %v", relPath, err)
	}
	defer f.Close()

	var violations []string
	sc := bufio.NewScanner(f)

	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	lineno := 0
	isChangelog := relPath == "CHANGELOG.md"
	changelogReleaseHeaders := 0
	for sc.Scan() {
		lineno++
		line := sc.Text()

		if isChangelog && changelogReleaseHeaderRe_inv219.MatchString(line) {
			changelogReleaseHeaders++
		}
		if isChangelog && changelogReleaseHeaders >= 2 {
			continue
		}

		if _, _, allowed := allowedBySubstring_inv219(line); allowed {
			continue
		}

		if _, allowed := allowedByFileLine_inv219(relPath, lineno); allowed {
			continue
		}
		if pat, found := containsForbiddenBrand_inv219(line); found {
			violations = append(violations,
				"  - "+relPath+":"+itoa_inv219(lineno)+
					": forbidden=\""+pat+"\" in line="+truncateForReport_inv219(line, 100)+
					" | rebrand or add allowlist entry with rationale")
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("inv-zen-219: scan %s: %v", relPath, err)
	}
	return violations
}

func walkGoDir_inv219(t *testing.T, root, relDir string) []string {
	t.Helper()
	var violations []string
	fullDir := filepath.Join(root, relDir)
	err := filepath.Walk(fullDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		violations = append(violations, scanGoSurface_inv219(t, root, relPath)...)
		return nil
	})
	if err != nil {
		t.Fatalf("inv-zen-219: walk %s: %v", relDir, err)
	}
	return violations
}

func walkDocDir_inv219(t *testing.T, root, relRoot string) []string {
	t.Helper()
	var violations []string
	fullRoot := filepath.Join(root, relRoot)
	info, err := os.Stat(fullRoot)
	if err != nil {
		t.Fatalf("inv-zen-219: stat %s: %v", relRoot, err)
	}
	scanOne := func(relPath string) {

		if _, _, allowed := allowedByPathPrefix_inv219(relPath); allowed {
			return
		}
		violations = append(violations, scanDocSurface_inv219(t, root, relPath)...)
	}
	if !info.IsDir() {

		scanOne(relRoot)
		return violations
	}
	err = filepath.Walk(fullRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".md", ".yaml", ".yml", ".py", ".txt":

		default:
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		scanOne(relPath)
		return nil
	})
	if err != nil {
		t.Fatalf("inv-zen-219: walk %s: %v", relRoot, err)
	}
	return violations
}

func TestInvZen219_GoSurfacesVisibleStringsHADES(t *testing.T) {
	root := repoRoot(t)
	var allViolations []string
	for _, rel := range goSourcePaths_inv219 {
		fullPath := filepath.Join(root, rel)
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Fatalf("inv-zen-219: stat %s: %v", rel, err)
		}
		if info.IsDir() {
			allViolations = append(allViolations, walkGoDir_inv219(t, root, rel)...)
		} else {
			allViolations = append(allViolations, scanGoSurface_inv219(t, root, rel)...)
		}
	}
	if len(allViolations) > 0 {
		t.Errorf("inv-zen-219 (visible strings HADES — Go surface) violated. "+
			"%d offending user-visible string literal(s) outside borderline allowlist:\n%s\n\n"+
			"Remediation: Either (a) rebrand the offending literal to HADES (preferred — surfaces "+
			"a Phase A-I miss; fix at source phase), OR (b) if it's a genuine borderline carve-out "+
			"per spec §Q3 BORDERLINE, add an entry to substringAllowlist_inv219 (or "+
			"fileLineAllowlist_inv219 for narrower per-file ranges) with a documented rationale "+
			"pointing to the spec §Q3 BORDERLINE table row that authorizes the carve-out.",
			len(allViolations), strings.Join(allViolations, "\n"))
	}
}

func TestInvZen219_DocSurfacesVisibleStringsHADES(t *testing.T) {
	root := repoRoot(t)
	var allViolations []string
	for _, rel := range docSourcePaths_inv219 {
		allViolations = append(allViolations, walkDocDir_inv219(t, root, rel)...)
	}
	if len(allViolations) > 0 {
		t.Errorf("inv-zen-219 (visible strings HADES — doc surface) violated. "+
			"%d offending doc-body occurrence(s) outside borderline allowlist:\n%s\n\n"+
			"Remediation: Either (a) rebrand the offending line to HADES (preferred — surfaces "+
			"a Phase I miss for top-level docs / Phase E miss for plugin docs; fix at source phase), "+
			"OR (b) if it's a genuine borderline carve-out per spec §Q3 BORDERLINE, add an entry to "+
			"substringAllowlist_inv219 (or fileLineAllowlist_inv219 for narrower per-file ranges) "+
			"with a documented rationale pointing to the spec §Q3 BORDERLINE table row that "+
			"authorizes the carve-out.",
			len(allViolations), strings.Join(allViolations, "\n"))
	}
}

func TestInvZen219_AllowlistEntriesHaveRationale(t *testing.T) {
	for i, entry := range substringAllowlist_inv219 {
		if strings.TrimSpace(entry.rationale) == "" {
			t.Errorf("substringAllowlist_inv219[%d] (%q) missing rationale; "+
				"per project doctrine, every allowlist entry MUST document its "+
				"spec §Q3 BORDERLINE justification.", i, entry.substring)
		}
	}
	for i, entry := range pathPrefixAllowlist_inv219 {
		if strings.TrimSpace(entry.rationale) == "" {
			t.Errorf("pathPrefixAllowlist_inv219[%d] (%q) missing rationale",
				i, entry.prefix)
		}
	}
	for i, entry := range fileLineAllowlist_inv219 {
		if strings.TrimSpace(entry.rationale) == "" {
			t.Errorf("fileLineAllowlist_inv219[%d] (%s:%d-%d) missing rationale",
				i, entry.path, entry.startLine, entry.endLine)
		}
	}
}

func TestInvZen219_SpecPointerDocLink(t *testing.T) {
	root := repoRoot(t)
	specPath := filepath.Join(root,
		"docs/superpowers/specs/2026-05-20-zen-swarm-plan-18-hades-system-unified-ux-design.md")
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("inv-zen-219: spec file missing at %s: %v", specPath, err)
	}
	masterPath := filepath.Join(root,
		"docs/superpowers/plans/2026-05-20-plan-18b-surfaces-master.md")
	if _, err := os.Stat(masterPath); err != nil {
		t.Fatalf("inv-zen-219: Plan 18b master file missing at %s: %v", masterPath, err)
	}
}

func itoa_inv219(i int) string {
	switch {
	case i == 0:
		return "0"
	case i < 0:
		return "-" + itoa_inv219(-i)
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+(i%10))) + digits
		i /= 10
	}
	return digits
}

func truncateForReport_inv219(s string, maxLen int) string {
	if len([]rune(s)) <= maxLen {
		return s
	}
	r := []rune(s)
	return string(r[:maxLen]) + "..."
}

var _ = regexp.MustCompile
