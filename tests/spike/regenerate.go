// SPDX-License-Identifier: MIT
// Package main implements the Phase 0 spike re-verify CLI.
//
// Per spec §2.6 Q6=C + §4.5 + §8.10 invariant: this binary regenerates
// the canonical spike artifact (internal design record
// swarm-plan-13-spike-hermes-mcp-contract.md) by querying Hermes head
// SHA via api.github.com + extracting VALID_HOOKS + PluginManifest schema
// from hermes_cli/plugins.py. If drift detected vs committed artifact,
// the CI gate (Makefile target verify-spike-current) halts.
//
// Modes (post review M-1 fix — explicit, mutually exclusive):
//
// (no flag) defaults to --check (fetch Hermes head + diff against artifact)
// --check fetch Hermes head + diff against artifact; exit 0=ok 1=drift 2=network-fail
// --check --offline only verify artifact file exists (no network); CI fast-path
// --regenerate fetch Hermes head + overwrite artifact (operator-driven; commits artifact via single docs(plan-13) commit)
//
// Drift gate semantics (post review C-3 fix): the check mode compares the
// full rendered artifact bytes (not just SHA) against the committed file.
// renderArtifact is deterministic — no timestamp embedded — so two probes
// against the same Hermes head produce byte-identical output.
//
// Per spec §0.4 doctrine + methodology §13 + feedback_plan_template_drift:
// drift detection in CI is the canonical "halt + investigate" trigger; the
// operator decides whether to amend spec inline (single commit) or
// to coordinate cross-plan implications first.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const SpikeAuditEventType = "evt.spike.hermes_contract.regenerated"

const (
	hermesRepoOwner = "NousResearch"

	hermesRepoName = "hermes-agent"
	pluginsPyPath  = "hermes_cli/plugins.py"
	artifactGlob   = "docs/superpowers/specs/*plan-13-spike-hermes-mcp-contract*.md"
	apiTimeout     = 30 * time.Second

	defaultGitHubAPIBase = "https://api.github.com"
	defaultRawGitHubBase = "https://raw.githubusercontent.com"
)

type hermesHeadInfo struct {
	HeadSHA      string
	ValidHooks   []string
	ManifestKeys []string
	FetchedAt    time.Time
}

type spikeOpts struct {
	check        bool
	regenerate   bool
	offline      bool
	apiBase      string
	rawBase      string
	auditEmitURL string
}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(runMain(ctx, os.Args[1:], os.Stderr))
}

func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("spike", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "verify spike artifact matches Hermes head")
	regenerate := fs.Bool("regenerate", false, "regenerate spike artifact (operator-driven)")
	offline := fs.Bool("offline", false, "offline mode: only verify artifact file exists")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "spike: flag parse: %v\n", err)
		return 1
	}

	if !*check && !*regenerate && !*offline {
		*check = true
	}

	opts := spikeOpts{
		check:      *check,
		regenerate: *regenerate,
		offline:    *offline,

		auditEmitURL: os.Getenv("ZEN_AUDIT_EMIT_URL"),
	}

	if err := runSpike(ctx, opts); err != nil {
		fmt.Fprintf(stderr, "spike error: %v\n", err)
		return 1
	}
	return 0
}

func runSpike(ctx context.Context, opts spikeOpts) error {
	if opts.regenerate && opts.offline {
		return errors.New("--regenerate and --offline are mutually exclusive")
	}

	if opts.offline {
		path, err := findArtifact()
		if err != nil {
			return fmt.Errorf("offline check: %w", err)
		}
		fmt.Printf("spike artifact present: %s\n", path)
		return nil
	}

	apiBase := opts.apiBase
	if apiBase == "" {
		apiBase = defaultGitHubAPIBase
	}
	rawBase := opts.rawBase
	if rawBase == "" {
		rawBase = defaultRawGitHubBase
	}

	info, err := fetchHermesHead(ctx, apiBase, rawBase)
	if err != nil {
		return fmt.Errorf("fetch hermes head: %w", err)
	}

	artifactPath, err := findArtifact()
	if err != nil {
		if opts.regenerate {
			artifactPath = filepath.Join("docs", "superpowers", "specs",
				fmt.Sprintf("%s-zen-swarm-plan-13-spike-hermes-mcp-contract.md",
					time.Now().Format("2006-01-02")))
		} else {
			return fmt.Errorf("artifact missing: %w", err)
		}
	}

	wantBytes := renderArtifact(info)

	if opts.regenerate {

		if err := os.WriteFile(artifactPath, wantBytes, 0o644); err != nil {
			return fmt.Errorf("write artifact: %w", err)
		}
		fmt.Printf("spike artifact regenerated: %s (Hermes head=%s)\n", artifactPath, info.HeadSHA)

		if opts.auditEmitURL != "" {
			emitSpikeAuditEvent(ctx, opts.auditEmitURL, artifactPath, info, len(wantBytes))
		}
		return nil
	}

	if opts.check {

		gotBytes, err := os.ReadFile(artifactPath)
		if err != nil {
			return fmt.Errorf("read artifact: %w", err)
		}
		if !bytes.Equal(gotBytes, wantBytes) {
			return fmt.Errorf("spike drift detected: artifact bytes differ from current Hermes head=%s; run `go run ./tests/spike --regenerate` and commit the new artifact via `docs(plan-13): spike re-verify Hermes contract`", info.HeadSHA)
		}
		fmt.Printf("spike artifact current: head=%s\n", info.HeadSHA)
	}
	return nil
}

func findArtifact() (string, error) {
	matches, err := filepath.Glob(artifactGlob)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no spike artifact found matching %s", artifactGlob)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple spike artifacts found: %v; expected exactly one", matches)
	}
	return matches[0], nil
}

func fetchHermesHead(ctx context.Context, apiBase, rawBase string) (*hermesHeadInfo, error) {
	client := &http.Client{Timeout: apiTimeout}

	headSHA, err := fetchHeadSHA(ctx, client, apiBase)
	if err != nil {
		return nil, err
	}

	pluginsSrc, err := fetchPluginsPy(ctx, client, rawBase, headSHA)
	if err != nil {
		return nil, err
	}

	hooks := extractValidHooks(pluginsSrc)
	manifestKeys := extractManifestKeys(pluginsSrc)

	return &hermesHeadInfo{
		HeadSHA:      headSHA,
		ValidHooks:   hooks,
		ManifestKeys: manifestKeys,
		FetchedAt:    time.Now().UTC(),
	}, nil
}

func fetchHeadSHA(ctx context.Context, client *http.Client, apiBase string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/HEAD", apiBase, hermesRepoOwner, hermesRepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build HEAD request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github HEAD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github HEAD: status=%d", resp.StatusCode)
	}
	var body struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode HEAD response: %w", err)
	}
	if body.SHA == "" {
		return "", errors.New("empty SHA in github HEAD response")
	}
	return body.SHA, nil
}

func fetchPluginsPy(ctx context.Context, client *http.Client, rawBase, sha string) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/%s/%s", rawBase, hermesRepoOwner, hermesRepoName, sha, pluginsPyPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build plugins.py request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch plugins.py: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plugins.py: status=%d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var (
	hookRe              = regexp.MustCompile(`(?ms)VALID_HOOKS\s*(?::\s*\S+)?\s*=\s*[\(\{]([^\)\}]+)[\)\}]`)
	hookItemRe          = regexp.MustCompile(`['"]([a-z_]+)['"]`)
	manifestPluginRe    = regexp.MustCompile(`(?ms)class\s+PluginManifest\s*:\s*\n((?:[ \t]+[^\n]*\n|[ \t]*\n)+)`)
	manifestFieldNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func stripPythonComments(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			out.WriteString("\n")
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func extractValidHooks(src string) []string {

	clean := stripPythonComments(src)
	m := hookRe.FindStringSubmatch(clean)
	if len(m) < 2 {
		return nil
	}
	var hooks []string
	for _, hm := range hookItemRe.FindAllStringSubmatch(m[1], -1) {
		hooks = append(hooks, hm[1])
	}
	return hooks
}

func extractManifestKeys(src string) []string {
	m := manifestPluginRe.FindStringSubmatch(src)
	if len(m) < 2 {
		return nil
	}
	var required []string
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		if !manifestFieldNameRe.MatchString(name) {
			continue
		}
		rest := line[colonIdx+1:]
		if !strings.Contains(rest, "=") {
			required = append(required, name)
		}
	}
	return required
}

func extractCommittedHeadSHA(artifactSrc string) string {
	for _, line := range strings.Split(artifactSrc, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "**Hermes head SHA**:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), "`")
			}
		}
	}
	return ""
}

func renderArtifact(info *hermesHeadInfo) []byte {
	var sb strings.Builder
	sb.WriteString("# Plan 13 Phase 0 spike — Hermes contract + MCP ecosystem + project-scope loading\n\n")
	sb.WriteString(fmt.Sprintf("**Hermes head SHA**: `%s`\n\n", info.HeadSHA))
	sb.WriteString("---\n\n## §1 Hermes contract\n\n")
	sb.WriteString("### VALID_HOOKS (extracted from `hermes_cli/plugins.py`)\n\n")
	for _, h := range info.ValidHooks {
		sb.WriteString(fmt.Sprintf("- `%s`\n", h))
	}
	sb.WriteString("\n### PluginManifest required keys\n\n")
	for _, k := range info.ManifestKeys {
		sb.WriteString(fmt.Sprintf("- `%s`\n", k))
	}
	sb.WriteString(mcpEcosystemSection)
	sb.WriteString(projectScopeSection)
	sb.WriteString("\n---\n\n**End of Plan 13 Phase 0 spike artifact.**\n")
	return []byte(sb.String())
}

const mcpEcosystemSection = "\n---\n\n## §2 MCP ecosystem\n\n" +
	"13-row curated table; canonical static data per Phase A plan §A-0. Live\n" +
	"verification (npm / pip / GitHub registry probes) is operator-driven via\n" +
	"`zen mcp catalog audit` (Plan 14+ tooling) out-of-band from this artifact\n" +
	"so the inv-zen-180 byte-compare drift gate stays deterministic across\n" +
	"`--regenerate` cycles.\n\n" +
	"| MCP | Tier | Availability | Hermes-compat | Notes |\n" +
	"|---|---|---|---|---|\n" +
	"| zen-swarm-ctld | 1 (mandatory) | local | yes | Q1=B aggregator; caronte in-process (Plan 19) |\n" +
	"| Playwright | 2 (universal) | npm `@microsoft/playwright-mcp` | yes | replaces ambiguous \"Browser MCP\" (SOTA-5) |\n" +
	"| Filesystem | 2 (universal) | `modelcontextprotocol/servers` reference | yes | |\n" +
	"| GitHub official | 2 (universal) | `modelcontextprotocol/servers` | yes | |\n" +
	"| Prisma Postgres | 3 (smart) | `@prisma/mcp` | yes | replaces archived Postgres MCP |\n" +
	"| Sentry | 3 (smart) | community | yes | |\n" +
	"| Linear | 3 (smart) | community | yes | |\n" +
	"| Memory MCP | 3 (smart) | `modelcontextprotocol/servers` | yes | default-off when Plan 9 substrate present |\n" +
	"| Sequential Thinking | 3 (smart) | community | yes | |\n" +
	"| SQLite | 4 (catalog) | community (post-archive) | yes | |\n" +
	"| GraphQL | 4 (catalog) | community | yes | |\n" +
	"| MySQL | 4 (catalog) | `benborla/mcp-server-mysql` | yes | |\n" +
	"| OpenAPI | 4 (catalog) | community | yes | |\n"

const projectScopeSection = "\n---\n\n## §3 Project-scope `.hermes/plugins/` loading\n\n" +
	"**Test fixture path**: `tests/spike/fixtures/hermes-project-scope/.hermes/plugins/zen-swarm/`\n\n" +
	"**Verdict**: `PASS-CONDITIONAL` — the runtime plugin loader DOES\n" +
	"discover project-scope plugins from `<cwd>/.hermes/plugins/<name>/`,\n" +
	"gated by the `HERMES_ENABLE_PROJECT_PLUGINS` env var (opt-in). The\n" +
	"`hermes plugins list` CLI command does NOT surface project-scope\n" +
	"entries (separate code path that only walks bundled + user dirs);\n" +
	"operator verification requires actually running an agent session in\n" +
	"the project directory rather than relying on the list command.\n\n" +
	"**Source citations**:\n\n" +
	"Runtime loader (PASS path):\n\n" +
	"```\n" +
	"hermes_cli/plugins.py:671-674\n" +
	"\n" +
	"    # 3. Project plugins (./.hermes/plugins/)\n" +
	"    if _env_enabled(\"HERMES_ENABLE_PROJECT_PLUGINS\"):\n" +
	"        project_dir = Path.cwd() / \".hermes\" / \"plugins\"\n" +
	"        manifests.extend(self._scan_directory(project_dir, source=\"project\"))\n" +
	"```\n\n" +
	"CLI list command (CAVEAT — does not include project):\n\n" +
	"```\n" +
	"hermes_cli/plugins_cmd.py:687-720 (_discover_all_plugins)\n" +
	"# walks bundled + user dirs only; no project-scope branch.\n" +
	"```\n\n" +
	"Hermes module-level docstring (`hermes_cli/plugins.py:5-14`) describes\n" +
	"the four plugin sources with explicit precedence: bundled < user <\n" +
	"project < pip. Project source overrides user/bundled on name\n" +
	"collision in the runtime loader.\n\n" +
	"**Strategy implications**:\n\n" +
	"- `internal/onboard/plugin/location.go::ResolveLocation` honours\n" +
	"  Q13=D: prefer project-scope when `HERMES_ENABLE_PROJECT_PLUGINS=1`\n" +
	"  (operator opts in via wizard or `.envrc`), otherwise fall back to\n" +
	"  user-scope (`~/.hermes/plugins/zen-swarm-<slug>/`).\n" +
	"- Onboarding wizard surfaces the env-var requirement in the project-\n" +
	"  scope install path (Phase D customize step + recommended-mode\n" +
	"  preflight hint). Wizard messaging notes the `hermes plugins list`\n" +
	"  caveat so operators don't expect a CLI confirmation; runtime use\n" +
	"  is the canonical signal.\n" +
	"- Fixture at `tests/spike/fixtures/hermes-project-scope/` documents\n" +
	"  the layout: `.hermes/plugins/zen-swarm/plugin.yaml` +\n" +
	"  `__init__.py`. Operator runtime verification (canonical signal):\n" +
	"  ```\n" +
	"  cd tests/spike/fixtures/hermes-project-scope/\n" +
	"  HERMES_ENABLE_PROJECT_PLUGINS=1 hermes chat -m \"List loaded plugins\"\n" +
	"  # expect: agent reports zen-swarm plugin available (runtime loader\n" +
	"  # discovered it; bare `hermes plugins list` would NOT show it).\n" +
	"  ```\n"

// emitSpikeAuditEvent posts evt.spike.hermes_contract.regenerated to the
// daemon's /v1/audit/emit endpoint after a successful --regenerate write.
// Best-effort: any network / decode / 4xx-5xx surface becomes a stderr
// warning so the operator knows forensic trace dropped, but the spike
// itself exits 0 because the artifact write is the load-bearing effect.
//
// We use a plain net/http POST rather than internal/client.Client so the
// spike binary stays a tiny standalone tool with no daemon-runtime
// dependency. The payload mirrors the spec §3.7 + invariant contract:
// artifact_path / hermes_head_sha / artifact_bytes / valid_hooks_count /
// manifest_required_keys_count.
func emitSpikeAuditEvent(ctx context.Context, baseURL, artifactPath string, info *hermesHeadInfo, artifactBytes int) {
	if baseURL == "" {
		return
	}
	payload := map[string]any{
		"artifact_path":                artifactPath,
		"hermes_head_sha":              info.HeadSHA,
		"artifact_bytes":               artifactBytes,
		"valid_hooks_count":            len(info.ValidHooks),
		"manifest_required_keys_count": len(info.ManifestKeys),

		"regenerated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	body := map[string]any{
		"type":    SpikeAuditEventType,
		"payload": payload,
	}
	raw, err := json.Marshal(body)
	if err != nil {

		fmt.Fprintf(os.Stderr, "warning: audit emit (spike): marshal payload: %v\n", err)
		return
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/audit/emit"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: audit emit (spike): build request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {

		fmt.Fprintf(os.Stderr, "warning: audit emit (spike): daemon unreachable: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// 4xx / 5xx: daemon up but request rejected. Surface a warning with
		// the status so the operator can investigate (e.g. chain DB locked).
		fmt.Fprintf(os.Stderr, "warning: audit emit (spike): status=%d\n", resp.StatusCode)
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
}
