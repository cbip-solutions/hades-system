// SPDX-License-Identifier: MIT
// Package errors centralises sentinel error values and the typed
// error-code catalog used across hades-system.
//
// This file (codes.go) ships the HADES design error catalog: a
// typed table of 31 user-visible error codes per design contract, each with
// a stable identifier (Code), a short title, a body template, a
// recovery hint (concrete shell command or doc link — NEVER
// platitudes), a Severity (driving exit-code mapping), and a Category.
// The catalog is the single source of truth consumed by
// internal/cli/error_render.go at every CLI subcommand
// boundary.
//
// # Catalog composition (31 entries)
//
// - 24 enumerated codes spanning 8 categories per design contract:
// daemon (4) + provider (5) + bypass (3) + wizard (3) +
// plugin (3) + tui (2) + cli (2) + skin (2)
// - 1 internal-uncaught defense-in-depth catch-all (severity
// SeverityFatal, category CategoryCLI; consumed by
// main.go recover() block for any error that escapes the
// catalog-routing layer)
// - 6 reserved overflow slots (reserved.slot-1... reserved.slot-6;
// severity SeverityInfo) representing intentional reserved
// capacity per max-scope doctrine. NOT stubs — future point-
// releases consume a slot in-place by renaming it to a real
// code; the schema + the LOC budget stay constant.
//
// # → contract
//
// exports the following types + functions which are FROZEN
// once ships (per HADES design master plan §"Cross-stage type
// discipline"):
//
// - Code (string type alias) — stable identifier; never renamed
// once shipped; expansion adds new constants, never modifies
// existing
// - Category enum (8 constants) — fixed surface; additions allowed
// via NEW const declarations + new entries in validCategories;
// removals forbidden
// - Severity enum (4 constants) — fixed surface
// - CodedError struct — fields {Code Code; Cause error;
// Context map[string]string}; stable shape
// - CatalogEntry struct — fields {Code Code; Title string;
// BodyTemplate string; RecoveryHint string; Severity Severity;
// Category Category}; stable shape
// - Lookup(code Code) *CatalogEntry — O(1) average; returns nil on
// miss
// - New(code, cause, ctx) *CodedError — full constructor
// - Wrap(code, cause) *CodedError — no-context shorthand
//
// Render() function is the SINGLE consumer of these types.
// Future plans extend the catalog by adding NEW entries (struct
// literals) to the catalog map; they do NOT modify existing fields
// or rewrite the schema. The 6 reserved overflow slots are consumed
// in-place by future plans (the slot is renamed from
// reserved.slot-N to a real code, with title/body/recovery
// populated — same line count, no schema change).
//
// # Doctrine compliance
//
// - Max-scope: 31 entries ship day 1 (not "10 codes now, 20
// later"); reserve > scarcity = 6 overflow slots
// - No stubs: every entry has fully-populated title/body/recovery
// text — reserved slots carry placeholder-but-NOT-stub text
// identifying them as reserved capacity
// - No platitudes: recovery hints are concrete shell commands or
// doc links — never "try again later" or "contact support"
// - Privacy-by-default: catalog contains NO secrets, NO operator
// PII; pure value types with no IO
// - Defense-in-depth: the internal-uncaught entry is the catch-all
// for any error that escapes the catalog-routing layer
//
// # Companion file
//
// errors.go preserves the legacy NotImplementedError family + HADES component..
// HADES component sentinels (additive composition — does NOT modify
// errors.go).
//
// # Spec references
//
// - design records design §design choice
// - design records design §" — Error catalog"
// - design records design (this file's parent)
package errors

import (
	stderrors "errors"
	"fmt"
)

type Code string

const CodeEndpointNotFound Code = "daemon.endpoint-not-found"

type Category string

type Severity string

const (
	CategoryDaemon Category = "daemon"

	CategoryProvider Category = "provider"

	CategoryBypass Category = "bypass"

	CategoryWizard Category = "wizard"

	CategoryPlugin Category = "plugin"

	CategoryTUI Category = "tui"

	CategoryCLI Category = "cli"

	CategorySkin Category = "skin"

	CategoryMigrate Category = "migrate"
)

const (
	SeverityFatal Severity = "fatal"

	SeverityError Severity = "error"

	SeverityWarn Severity = "warn"

	SeverityInfo Severity = "info"
)

var validCategories = map[Category]bool{
	CategoryDaemon:   true,
	CategoryProvider: true,
	CategoryBypass:   true,
	CategoryWizard:   true,
	CategoryPlugin:   true,
	CategoryTUI:      true,
	CategoryCLI:      true,
	CategorySkin:     true,
	CategoryMigrate:  true,
}

var validSeverities = map[Severity]bool{
	SeverityFatal: true,
	SeverityError: true,
	SeverityWarn:  true,
	SeverityInfo:  true,
}

type CodedError struct {
	Code    Code
	Cause   error
	Context map[string]string
}

func (e *CodedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Cause.Error())
}

func (e *CodedError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	t, ok := target.(*CodedError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

func (e *CodedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type CatalogEntry struct {
	Code         Code
	Title        string
	BodyTemplate string
	RecoveryHint string
	Severity     Severity
	Category     Category
}

// catalog is the package-level map of every shipped error code to its
// CatalogEntry. Populated at package init (Go static-var init). stage
// A ships 31 entries:
//
// - 24 enumerated codes (8 categories × 2-5 codes each per design contract)
// → daemon(4) + provider(5) + bypass(3) + wizard(3) + plugin(3)
// - tui(2) + cli(2) + skin(2) = 24
// - 1 internal-uncaught defense-in-depth entry
// - 6 reserved overflow slots (reserved.slot-1... reserved.slot-6)
//
// Lookup discipline:
// - O(1) average via map[Code]*CatalogEntry (Go runtime hash map)
// - Pointer-typed values (NOT inline struct values) so consumers can
// compare for nil to detect a catalog miss without copying the
// entry
//
// Mutability discipline:
// - The map is package-private (lowercase identifier) — external
// consumers MUST use Lookup() to read entries; mutation outside
// the package is impossible
// - In-package additions go through new struct literals in this map
// declaration; runtime mutation is forbidden
// - invariant compliance test asserts no production
// code path mutates this map at runtime
var catalog = map[Code]*CatalogEntry{

	"daemon.not-running": {
		Code:         "daemon.not-running",
		Title:        "HADES daemon not running.",
		BodyTemplate: "Could not connect to the HADES daemon at the expected UDS path. The daemon is the single egress point for LLM traffic + the source of session state; the CLI cannot proceed without it.",
		RecoveryHint: "run: hades daemon start (one-shot) OR hades daemon install (persistent: launchd on macOS, systemd template on Linux); see docs/operations/hades-entry-point.md §4.2",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"daemon.unreachable": {
		Code:         "daemon.unreachable",
		Title:        "HADES daemon unreachable.",
		BodyTemplate: "The daemon process appears to be running, but the UDS socket is not responding. The socket may be stale (daemon crashed without cleanup) or the file permissions may block the current user.",
		RecoveryHint: "the socket may be stale: rm /tmp/hades-system.sock && hades daemon start; if launchd-managed: launchctl kickstart -k gui/$UID/com.hadessystem.ctld; see docs/operations/hades-entry-point.md §4.2",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"daemon.responded-with-error": {
		Code:         "daemon.responded-with-error",
		Title:        "HADES daemon returned an error response.",
		BodyTemplate: "The daemon is reachable and processed the request but returned a non-success HTTP status. This is NOT a transport failure — the daemon is up and routing correctly. Inspect the response body for the specific cause.",
		RecoveryHint: "check the wrapped error for the daemon's response body (typically includes the upstream cause); do NOT delete the socket — the daemon is up. Re-run with --verbose for the full HTTP error chain.",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"daemon.endpoint-not-found": {
		Code:         CodeEndpointNotFound,
		Title:        "HADES daemon endpoint not found.",
		BodyTemplate: "The daemon returned HTTP 404 for the requested path. The daemon process is reachable and responding, but the endpoint may have moved (route renamed across releases) or been deprecated. Common cause: the installed daemon binary is older than the CLI and predates the route.",
		RecoveryHint: "verify CLI / daemon versions match: hades --version && hades doctor --component=daemon; if a recent upgrade moved the route (e.g. HADES design moved /v1/caronte/* to /v1/mcpgateway/*), reinstall both via the same release: make install (from this repo). Do NOT delete /tmp/hades-system.sock — that is the wrong remedy for endpoint-404 (the daemon is reachable).",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"daemon.version-mismatch": {
		Code:         "daemon.version-mismatch",
		Title:        "HADES CLI / daemon version mismatch.",
		BodyTemplate: "The CLI and the running daemon report different versions. The wire protocol between them is version-pinned; mixed versions may silently corrupt state.",
		RecoveryHint: "verify: hades --version && hades doctor --component=daemon (will print daemon version); if mismatched, run: systemctl --user restart hades-ctld; if persistent, reinstall both via the same release: brew reinstall hades (macOS) or `make install` from this repo",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"daemon.auth-failed": {
		Code:         "daemon.auth-failed",
		Title:        "HADES daemon authentication failed.",
		BodyTemplate: "The CLI's session token was rejected by the daemon. The token may have rotated (daemon restarted without preserving the secrets file) or the secrets file may be corrupted.",
		RecoveryHint: "run: hades doctor --component=auth (prints token file location + validity); if invalid, run: rm ~/.config/hades-system/daemon.token && systemctl --user restart hades-ctld (daemon regenerates the token); see docs/operations/security.md §daemon-auth for the token-lifecycle reference",
		Severity:     SeverityError,
		Category:     CategoryDaemon,
	},

	"provider.auth-401": {
		Code:         "provider.auth-401",
		Title:        "Provider authentication failed (HTTP 401).",
		BodyTemplate: "The upstream LLM provider rejected the API key. The key may have expired, been rotated, or never been valid for this provider's account.",
		RecoveryHint: "verify: hades providers list (shows which providers have a key configured); to rotate, run: hades providers add <provider-name> --key=NEW_KEY (replaces the existing key in macOS Keychain / Linux secret store); see docs/operations/providers.md §api-keys for per-provider key-management",
		Severity:     SeverityError,
		Category:     CategoryProvider,
	},

	"provider.quota-429": {
		Code:         "provider.quota-429",
		Title:        "Provider rate-limited (HTTP 429).",
		BodyTemplate: "The upstream LLM provider returned a 429 (Too Many Requests). Either the request rate exceeded the provider's per-minute / per-day cap, or the account's monthly token budget is exhausted.",
		RecoveryHint: "inspect: hades providers list --verbose (shows last-known rate-limit headers + remaining quota); to switch providers temporarily, run: hades cascade promote-fallback (next provider in the cascade chain becomes primary); see docs/operations/providers.md §rate-limits for backoff guidance",
		Severity:     SeverityWarn,
		Category:     CategoryProvider,
	},

	"provider.network-timeout": {
		Code:         "provider.network-timeout",
		Title:        "Provider network timeout.",
		BodyTemplate: "The TCP connection to the provider's HTTPS endpoint timed out. Common causes: corporate proxy blocking, transient ISP issue, or the provider's regional endpoint experiencing degraded availability.",
		RecoveryHint: "diagnose: hades doctor --component=network (probes each cascade endpoint); if local network is OK, the provider's status page may show the issue; try: hades cascade promote-fallback to switch providers in the meantime",
		Severity:     SeverityWarn,
		Category:     CategoryProvider,
	},

	"provider.tls-fail": {
		Code:         "provider.tls-fail",
		Title:        "Provider TLS handshake failed.",
		BodyTemplate: "The TLS handshake with the provider's HTTPS endpoint failed (certificate expired, untrusted CA, or TLS version mismatch). System CA roots may be missing or outdated.",
		RecoveryHint: "verify: openssl s_client -connect <provider-host>:443 -servername <provider-host> (shows the certificate chain); on macOS, run: brew install ca-certificates && brew link ca-certificates; on Linux, run: sudo apt-get install --reinstall ca-certificates",
		Severity:     SeverityError,
		Category:     CategoryProvider,
	},

	"provider.model-unavailable": {
		Code:         "provider.model-unavailable",
		Title:        "Requested model not available on provider.",
		BodyTemplate: "The provider does not advertise the requested model (e.g., a deprecated model ID, or a model not in this account's tier). The cascade may have selected an unsupported (provider, model) pair.",
		RecoveryHint: "list available models: hades providers list --models (shows each provider's advertised model IDs); to update the cascade chain, run: hades cascade configure (interactive); see docs/operations/providers.md §model-roster for the per-provider model matrix",
		Severity:     SeverityError,
		Category:     CategoryProvider,
	},

	"bypass.config-missing": {
		Code:         "bypass.config-missing",
		Title:        "Bypass config not extracted yet.",
		BodyTemplate: "The Anthropic Tier 1 integration requires a one-time interactive extraction of OAuth credentials from a logged-in upstream client. No config has been extracted; Tier 1 is disabled until this completes.",
		RecoveryHint: "run interactively in a TTY: hades bypass extract-config (prompts for the upstream OAuth token; writes ~/.config/hades-system/bypass-config.json); see CONFIGURATION.md Sidecar Config for the optional loopback integration contract",
		Severity:     SeverityWarn,
		Category:     CategoryBypass,
	},

	"bypass.tier-degraded": {
		Code:         "bypass.tier-degraded",
		Title:        "Bypass tier degraded.",
		BodyTemplate: "The bypass module's 24-hour success rate dropped below the configured floor (default 95%). Recent requests are routing through fallback providers; investigate before the floor is breached entirely.",
		RecoveryHint: "diagnose: hades doctor --component=bypass (shows recent error breakdown + token-refresh history); inspect last failures: hades bypass tail-errors --limit=20; if the token has expired, run: hades bypass extract-config (re-extracts from a fresh upstream client session)",
		Severity:     SeverityWarn,
		Category:     CategoryBypass,
	},

	"bypass.schema-invalid": {
		Code:         "bypass.schema-invalid",
		Title:        "Bypass config schema invalid.",
		BodyTemplate: "The bypass-config.json file failed schema validation (corrupt JSON, missing required field, or schema-version mismatch with the current binary). The file may have been hand-edited or partially written.",
		RecoveryHint: "verify schema: hades bypass validate-config (prints the first schema violation); to regenerate, run: hades bypass extract-config (overwrites with a fresh extraction); see CONFIGURATION.md Sidecar Config for the schema overview",
		Severity:     SeverityError,
		Category:     CategoryBypass,
	},

	"wizard.config-corrupt": {
		Code:         "wizard.config-corrupt",
		Title:        "Onboarding wizard found corrupt config.",
		BodyTemplate: "The config file at ~/.config/hades-system/config.toml exists but failed parsing (invalid TOML, missing required section, or partial write). The wizard cannot continue without a valid config.",
		RecoveryHint: "inspect: cat ~/.config/hades-system/config.toml | head -50 (check for syntax errors); to start fresh, run: mv ~/.config/hades-system/config.toml ~/.config/hades-system/config.toml.bak && hades (re-launches the wizard with a clean slate); see docs/operations/onboarding.md §config-schema for the expected layout",
		Severity:     SeverityError,
		Category:     CategoryWizard,
	},

	"wizard.migrate-incomplete": {
		Code:         "wizard.migrate-incomplete",
		Title:        "Migration step did not complete cleanly.",
		BodyTemplate: "The wizard's migrate step (e.g., importing local agent memory/ artifacts) returned a partial-success status. Some artifacts were imported; some were skipped due to schema mismatch or permission issues.",
		RecoveryHint: "review skipped items: hades migrate HADES design --dry-run (lists what would still change); to retry, run: hades migrate HADES design --apply (idempotent — safe to invoke multiple times); see docs/operations/hades-entry-point.md §migration-tooling for the per-source allowlist",
		Severity:     SeverityWarn,
		Category:     CategoryWizard,
	},

	"wizard.mcp-spawn-fail": {
		Code:         "wizard.mcp-spawn-fail",
		Title:        "MCP server spawn failed during wizard.",
		BodyTemplate: "The wizard tried to spawn one of the 4 hades-system MCP servers (research, budget, audit, ssh-exec) to verify Hermes plugin integration; the spawn failed (binary not on PATH, permission denied, or transport handshake error).",
		RecoveryHint: "diagnose: hades doctor --component=mcp (probes each MCP server with verbose output); verify PATH: which hades-system-mcp-research && which hades-system-mcp-budget; if missing, run: make install (rebuilds + installs all binaries); see docs/operations/mcps.md for MCP server-specific install steps",
		Severity:     SeverityError,
		Category:     CategoryWizard,
	},

	"plugin.load-error": {
		Code:         "plugin.load-error",
		Title:        "Hermes failed to load the HADES plugin.",
		BodyTemplate: "Hermes Agent attempted to load the plugin at ~/.hermes/plugins/hades/ but encountered an error during discovery (import error in plugin/__init__.py, missing manifest, or Python version mismatch).",
		RecoveryHint: "inspect: hermes plugins list --verbose (shows per-plugin load status + last error); reinstall: rm -rf ~/.hermes/plugins/hades && hades --install-plugin (re-deploys the plugin from this binary); verify Python: python3 --version (Hermes v0.13+ requires 3.11+); see docs/operations/plugin.md §load-error for the import-chain debug recipe",
		Severity:     SeverityError,
		Category:     CategoryPlugin,
	},

	"plugin.command-not-found": {
		Code:         "plugin.command-not-found",
		Title:        "Slash command not registered.",
		BodyTemplate: "Hermes received a slash command (e.g., /hades:status, /hades:dashboard) that the HADES plugin did not register. The plugin may have loaded with a partial command set, or the slash name may be typo'd.",
		RecoveryHint: "list registered commands: hermes plugins commands hades (shows every /hades:* surface); verify plugin integrity: hades doctor --component=plugin; reload Hermes: hermes reload --plugin=hades; valid /hades:* commands include /hades:start, /hades:handoff, /hades:status, /hades:dashboard, /hades:panel, /hades:install-mcps",
		Severity:     SeverityError,
		Category:     CategoryPlugin,
	},

	"plugin.mcp-handshake-fail": {
		Code:         "plugin.mcp-handshake-fail",
		Title:        "MCP handshake with Hermes plugin failed.",
		BodyTemplate: "The HADES plugin tried to register MCP servers with Hermes' MCP host, but the handshake failed (protocol version mismatch, transport error, or the writer's mcp_servers translator emitted a malformed manifest — see invariant for the writer constraint).",
		RecoveryHint: "diagnose: hades doctor --component=mcp (probes each MCP transport with verbose output); reinstall MCPs: hades plugin install-mcps (re-emits the manifests via the invariant-compliant writer); see docs/operations/mcps.md §handshake-protocol and ADR-0094 + ADR-0095 for the transport contract",
		Severity:     SeverityError,
		Category:     CategoryPlugin,
	},

	"tui.panel-data-unavailable": {
		Code:         "tui.panel-data-unavailable",
		Title:        "TUI panel data unavailable.",
		BodyTemplate: "A dashboard panel (workforce, cost, audit, hra, confirmations, memory, skills, doctrine, codegraph, inbox, crossproject, help) tried to fetch its data from the daemon but the endpoint returned 503 or no payload. The panel rendered in degraded mode.",
		RecoveryHint: "diagnose: hades doctor --component=daemon (shows endpoint availability matrix); for a specific panel, run: hades doctor --panel=<name>; see docs/operations/tui.md §degraded-mode for the per-panel data-source map",
		Severity:     SeverityWarn,
		Category:     CategoryTUI,
	},

	"tui.dashboard-incompatible-terminal": {
		Code:         "tui.dashboard-incompatible-terminal",
		Title:        "Terminal does not support TUI dashboard.",
		BodyTemplate: "The bubbletea dashboard requires a terminal that advertises at least 256 colors + alt-screen support. The current terminal does not (TERM is missing or set to a non-color value like dumb / linux).",
		RecoveryHint: "verify TERM: echo $TERM (expected: xterm-256color, alacritty, kitty, screen-256color, tmux-256color, or similar); set: export TERM=xterm-256color (temporary fix); for tmux: ensure the outer TERM is 256-color before tmux launches; see docs/operations/tui.md §terminal-compat for the support matrix",
		Severity:     SeverityError,
		Category:     CategoryTUI,
	},

	"cli.unknown-subcommand": {
		Code:  "cli.unknown-subcommand",
		Title: "Unknown subcommand.",

		BodyTemplate: "{{suggestion_line}}The HADES CLI does not recognize the subcommand. It may be a typo, a deprecated command, or a command that is unavailable in this release.",
		RecoveryHint: "list valid subcommands: hades --help (top-level command tree); to find a specific subcommand by keyword: hades --help | grep -i <keyword>; the legacy `hades` subcommand surface is still available — see docs/operations/hades-entry-point.md §legacy-cli-passthrough for the full alias map",
		Severity:     SeverityError,
		Category:     CategoryCLI,
	},

	"cli.arg-validation-fail": {
		Code:         "cli.arg-validation-fail",
		Title:        "Argument validation failed.",
		BodyTemplate: "One of the flags or positional arguments failed validation (wrong type, out of range, conflicting flags, or a required flag missing). The subcommand did not run.",
		RecoveryHint: "show usage: hades <subcommand> --help (lists every flag with its constraints); common errors: --apply requires --dry-run=false; --panel requires a value from the 12-panel allowlist (workforce/cost/audit/hra/confirmations/memory/skills/doctrine/codegraph/inbox/crossproject/help)",
		Severity:     SeverityError,
		Category:     CategoryCLI,
	},

	"skin.skin-not-registered": {
		Code:         "skin.skin-not-registered",
		Title:        "HADES skin not registered with Hermes.",
		BodyTemplate: "Hermes did not find a skin manifest at ~/.hermes/skins/hades.toml. The `hades` wrapper sets HERMES_SKIN=hades but Hermes loaded its default skin instead because the manifest is missing or unreadable.",
		RecoveryHint: "verify: ls -la ~/.hermes/skins/hades.toml (expected: regular file, readable by current user); reinstall the skin: hades --install-skin (re-deploys from binary); see docs/operations/hades-entry-point.md §skin-engine and ADR-0094 for the skin module closure contract (invariant)",
		Severity:     SeverityError,
		Category:     CategorySkin,
	},

	"skin.skin-load-fail": {
		Code:         "skin.skin-load-fail",
		Title:        "Hermes failed to load the HADES skin.",
		BodyTemplate: "Hermes found ~/.hermes/skins/hades.toml but failed to parse it (invalid TOML, missing required keys, or schema-version mismatch with the current Hermes binary). Hermes fell back to its default skin.",
		RecoveryHint: "inspect: cat ~/.hermes/skins/hades.toml | head -30 (check for syntax errors); validate against schema: hades doctor --component=skin (prints first schema violation); reinstall fresh: hades --install-skin --force (overwrites the manifest); see ADR-0094 for the skin manifest schema",
		Severity:     SeverityError,
		Category:     CategorySkin,
	},

	"internal-uncaught": {
		Code:         "internal-uncaught",
		Title:        "HADES internal error (uncaught).",
		BodyTemplate: "An error escaped the catalog-routing layer (raw error chain or unrecovered panic). The CLI rendered this via the defense-in-depth fallback. The panic trace + error chain were printed above; please include them in a bug report.",
		RecoveryHint: "report at: https://github.com/cbip-solutions/hades-system/issues/new (paste the panic trace + the command that triggered it); for an immediate workaround, re-run with: hades --verbose <subcommand> (verbose mode often surfaces the underlying error code that was missed)",
		Severity:     SeverityFatal,
		Category:     CategoryCLI,
	},

	"reserved.slot-1": {
		Code:         "reserved.slot-1",
		Title:        "Reserved overflow slot 1 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. If you are seeing this in production output, a downstream consumer is referencing a reserved slot — verify the consumer's intent or upgrade to a release that has consumed the slot with a real code.",
		RecoveryHint: "see: design records release design §\"Doctrine applied\" §\"No tech debt\" (reserved-capacity contract); to file a request for slot-1 consumption, open an issue referencing reserved.slot-1 and the real code that would land there",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"reserved.slot-2": {
		Code:         "reserved.slot-2",
		Title:        "Reserved overflow slot 2 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. Future point-releases consume slots in-place; the slot becomes a real surface (with full title/body/recovery) at that time. stage ships 6 reserved slots; this is slot 2.",
		RecoveryHint: "see: design records release design §design choice (catalog enumeration + reserved-capacity rationale); the HADES design master plan §\"Doctrine applied\" documents the reserve > scarcity contract",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"reserved.slot-3": {
		Code:         "reserved.slot-3",
		Title:        "Reserved overflow slot 3 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. Reserved slots are NOT stubs — they are production-shape catalog entries that happen to be reserved for future consumption. stage ships 6 reserved slots; this is slot 3.",
		RecoveryHint: "see: HADES design master plan §\"Doctrine applied\" — the reserved-capacity contract; consult design records release design §design choice for the catalog schema",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"reserved.slot-4": {
		Code:         "reserved.slot-4",
		Title:        "Reserved overflow slot 4 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. stage ships 6 reserved slots; this is slot 4. The slot will be renamed to a real code at the point a future plan needs additional catalog headroom.",
		RecoveryHint: "see: design records release design §\"Doctrine applied\" §\"No stubs, código completo\" (reserved-slot definition); spec §design choice (full catalog enumeration including this slot)",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"reserved.slot-5": {
		Code:         "reserved.slot-5",
		Title:        "Reserved overflow slot 5 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. The 6 reserved slots are a load-bearing application of the doctrine: future plans inherit catalog headroom without a schema migration. stage ships 6 reserved slots; this is slot 5.",
		RecoveryHint: "see: HADES design master plan §\"Doctrine applied\" (full reserved-capacity rationale); spec §design choice (per-slot ID assignment); consult local agent memory/projects/-path-to-projects-hades-system/memory/feedback_no_stubs_complete_code.md for the doctrine reference",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"reserved.slot-6": {
		Code:         "reserved.slot-6",
		Title:        "Reserved overflow slot 6 (catalog capacity).",
		BodyTemplate: "This code is reserved capacity per the HADES design max-scope doctrine. stage ships 6 reserved slots; this is the last slot (slot 6). If all 6 slots are consumed and a new code is needed, the catalog grows by adding new struct literals (additive expansion); no schema change required.",
		RecoveryHint: "see: design records release design §\"Doctrine applied\" + §\"No tech debt\" (reserved-capacity is doctrine-compliant, NOT tech debt); the doctrine memory at ~/local agent config/projects/-path-to-projects-hades-system/memory/feedback_no_stubs_complete_code.md is the load-bearing reference",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"cli.no-op": {
		Code:         "cli.no-op",
		Title:        "No changes needed.",
		BodyTemplate: "The operation found no pending changes in the scanned surfaces. Either the migration has already been applied, or no matching references were present in the allowlisted paths.",
		RecoveryHint: "verify: re-run with --dry-run to confirm there is truly nothing to do (output will show <no changes; already migrated>); if you expected changes, check --home points to the correct home dir and the target files are within the allowlist scope (see hades migrate HADES design --help)",
		Severity:     SeverityInfo,
		Category:     CategoryCLI,
	},

	"migrate.allowlist-violation": {
		Code:         "migrate.allowlist-violation",
		Title:        "File is outside the migration allowlist.",
		BodyTemplate: "The path {{.rel}} is not in the set of operator home-dir surfaces that `hades migrate HADES design` is allowed to read or write. The tool NEVER touches files outside the allowlist (shell RC files, ~/.config/hades-system/, ~/.hades/, ~/.hermes/plugins/hades-system/, specific local agent memory/ files).",
		RecoveryHint: "verify the file path is within the allowlist: hades migrate HADES design --help (prints the full scope); if you believe the path should be in scope, open an issue at https://github.com/cbip-solutions/hades-system/issues/new referencing migrate.allowlist-violation + the rejected path",
		Severity:     SeverityError,
		Category:     CategoryMigrate,
	},

	"migrate.symlink-out-of-scope": {
		Code:         "migrate.symlink-out-of-scope",
		Title:        "Symlink target is outside the migration allowlist scope.",
		BodyTemplate: "A symlink at {{.rel}} resolves to a target outside the operator home dir or outside the allowlisted path scope. The tool skips out-of-scope symlink targets to prevent writing to unintended locations (defense-in-depth: the allowlist boundary is enforced at symlink resolution time, not just at path construction time).",
		RecoveryHint: "inspect the symlink: readlink -f {{.rel}} (shows the resolved target); if the target should be migrated, follow the symlink manually and re-run the tool with --home pointing to the target's home dir; see docs/operations/hades-entry-point.md §migration-tooling §safety-guarantees",
		Severity:     SeverityWarn,
		Category:     CategoryMigrate,
	},

	"migrate.write-failed": {
		Code:         "migrate.write-failed",
		Title:        "Migration atomic write failed.",
		BodyTemplate: "The atomic write (temp + fsync + rename) for {{.path}} failed. The file was NOT modified; the backup (if created) is intact at the timestamped subdir. Common causes: disk full, permission denied on the parent directory, or a concurrent migration invocation holding the backup-dir lock.",
		RecoveryHint: "diagnose: ls -la {{.path}} && df -h {{.path}} (check permissions + disk space); verify no concurrent migration is running: ls ~/.local/share/hades-system/migrate-HADES design (if present + stale, remove it manually); then re-run: hades migrate HADES design --from-hades-system-aliases --apply (idempotent — already-migrated files skipped)",
		Severity:     SeverityError,
		Category:     CategoryMigrate,
	},

	"migrate.dry-run-required": {
		Code:         "migrate.dry-run-required",
		Title:        "Migration requires explicit --apply flag.",
		BodyTemplate: "The migration tool is dry-run-by-default. No files were modified. To perform the actual migration, pass --apply explicitly (this is the operator-explicit safety gate per design contract— the tool refuses to mutate files without an affirmative --apply flag).",
		RecoveryHint: "re-run with the apply flag: hades migrate HADES design --from-hades-system-aliases --apply (this will perform the atomic per-file writes + create a timestamped backup under ~/.local/share/hades-system/migrate-HADES design); review the dry-run diff first if you haven't: hades migrate HADES design --from-hades-system-aliases (no --apply = dry-run)",
		Severity:     SeverityWarn,
		Category:     CategoryMigrate,
	},
}

func Lookup(code Code) *CatalogEntry {
	if code == "" {
		return nil
	}
	entry, ok := catalog[code]
	if !ok {
		return nil
	}
	return entry
}

// New constructs a *CodedError with the given Code, optional Cause,
// and optional Context map. All arguments after the Code are
// permitted-nil; nil arguments are stored as-is (no defaulting to
// empty values).
//
// The most common consumer pattern is Wrap(code, cause) for cases
// without context. Use New only when context vars must be attached
// at construction:
//
// return errors.New(codes.ProviderAuth401, httpErr, map[string]string{
// "provider": "anthropic-paygo",
// "endpoint": "/v1/messages",
// })
//
// Context-isolation discipline: the ctx map is stored by reference;
// callers MUST NOT mutate it after construction. TestConstructorContextIsolated
// documents this contract.
func New(code Code, cause error, ctx map[string]string) *CodedError {
	return &CodedError{
		Code:    code,
		Cause:   cause,
		Context: ctx,
	}
}

func Wrap(code Code, cause error) *CodedError {
	return &CodedError{
		Code:  code,
		Cause: cause,
	}
}

func IsCode(err error, code Code) bool {
	if err == nil || code == "" {
		return false
	}
	var ce *CodedError
	if stderrors.As(err, &ce) {
		return ce.Code == code
	}
	return false
}
