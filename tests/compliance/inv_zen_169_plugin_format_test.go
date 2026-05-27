// go:build compliance
//go:build compliance
// +build compliance

package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var validHooks = map[string]struct{}{
	"pre_tool_call":             {},
	"post_tool_call":            {},
	"transform_terminal_output": {},
	"transform_tool_result":     {},
	"transform_llm_output":      {},
	"pre_llm_call":              {},
	"post_llm_call":             {},
	"pre_api_request":           {},
	"post_api_request":          {},
	"on_session_start":          {},
	"on_session_end":            {},
	"on_session_finalize":       {},
	"on_session_reset":          {},
	"subagent_stop":             {},
	"pre_gateway_dispatch":      {},
	"pre_approval_request":      {},
	"post_approval_response":    {},
}

type inv169PluginManifest struct {
	Name          string   `yaml:"name"`
	Version       string   `yaml:"version"`
	Description   string   `yaml:"description"`
	Author        string   `yaml:"author"`
	RequiresEnv   []string `yaml:"requires_env"`
	ProvidesTools []string `yaml:"provides_tools"`
	ProvidesHooks []string `yaml:"provides_hooks"`
	Kind          string   `yaml:"kind"`
}

type inv169RawManifestKeys map[string]any

var inv169AllowedManifestKeys = map[string]struct{}{
	"name":           {},
	"version":        {},
	"description":    {},
	"author":         {},
	"requires_env":   {},
	"provides_tools": {},
	"provides_hooks": {},
	"kind":           {},
}

func inv169RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func inv169ReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestInvZen169RegisterSymbolPresent(t *testing.T) {
	root := inv169RepoRoot(t)
	src := inv169ReadFile(t, filepath.Join(root, "plugin/hades/__init__.py"))
	if !regexp.MustCompile(`(?m)^\s*def\s+register\s*\(\s*ctx`).MatchString(src) {
		t.Errorf(
			"inv-zen-169: plugin/hades/__init__.py must export `def register(ctx)` " +
				"(Hermes plugin entry-point per spike §2)",
		)
	}
}

func TestInvZen169PreLLMCallSymbolPresent(t *testing.T) {
	root := inv169RepoRoot(t)
	src := inv169ReadFile(t, filepath.Join(root, "plugin/hades/hooks/llm_handlers.py"))
	if !regexp.MustCompile(`(?m)^\s*def\s+pre_llm_call\s*\(`).MatchString(src) {
		t.Errorf(
			"inv-zen-169: plugin/hades/hooks/llm_handlers.py must export " +
				"`def pre_llm_call(` (Phase H' Task H'-3 + Plan 11 Phase B B-6 extension)",
		)
	}
}

func TestInvZen169RegisteredHookNamesAreInValidHooks(t *testing.T) {
	root := inv169RepoRoot(t)
	src := inv169ReadFile(t, filepath.Join(root, "plugin/hades/__init__.py"))

	re := regexp.MustCompile(`ctx\.register_hook\(\s*["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatalf(
			"inv-zen-169: plugin/hades/__init__.py must contain at least one " +
				"ctx.register_hook() call (Phase H' Task H'-3 wires 5 hooks)",
		)
	}
	var violations []string
	for _, m := range matches {
		name := m[1]
		if _, ok := validHooks[name]; !ok {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf(
			"inv-zen-169: hook names not in Hermes VALID_HOOKS (spike §4): %s. "+
				"Permitted names: see hermes_cli/plugins.py:81-127.",
			strings.Join(violations, ", "),
		)
	}
}

func TestInvZen169PluginYamlShape(t *testing.T) {

	root := inv169RepoRoot(t)
	manifestPath := filepath.Join(root, "plugin/hades/plugin.yaml")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Skipf("plugin.yaml not present (optional per spike §2); skipping: %v", err)
	}

	var keys inv169RawManifestKeys
	if err := yaml.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("inv-zen-169: plugin.yaml must be valid YAML: %v", err)
	}

	var fictional []string
	for k := range keys {
		if _, ok := inv169AllowedManifestKeys[k]; !ok {
			fictional = append(fictional, k)
		}
	}
	if len(fictional) > 0 {
		sort.Strings(fictional)
		t.Errorf(
			"inv-zen-169: plugin.yaml contains fictional top-level keys not in "+
				"Hermes PluginManifest schema (spike §3): %s. Allowed keys: "+
				"name, version, description, author, requires_env, "+
				"provides_tools, provides_hooks, kind.",
			strings.Join(fictional, ", "),
		)
	}

	var manifest inv169PluginManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("inv-zen-169: plugin.yaml typed parse failed: %v", err)
	}
	if manifest.Name == "" {
		t.Errorf("inv-zen-169: plugin.yaml must declare 'name' (PluginManifest.name is required)")
	}

	// provides_hooks (advertised) MUST be a subset of VALID_HOOKS if declared.
	for _, h := range manifest.ProvidesHooks {
		if _, ok := validHooks[h]; !ok {
			t.Errorf(
				"inv-zen-169: provides_hooks contains %q which is not in Hermes "+
					"VALID_HOOKS (spike §4)", h,
			)
		}
	}
}

func TestInvZen169RegisterRunsUnderFakePluginContext(t *testing.T) {

	root := inv169RepoRoot(t)
	src := inv169ReadFile(t, filepath.Join(root, "plugin/hades/__init__.py"))

	requiredHooks := []string{
		"on_session_start",
		"on_session_end",
		"pre_tool_call",
		"post_tool_call",
		"pre_llm_call",
	}
	for _, hook := range requiredHooks {
		needle := `ctx.register_hook("` + hook + `"`
		if !strings.Contains(src, needle) {
			t.Errorf(
				"inv-zen-169: register(ctx) must wire %s "+
					"(missing ctx.register_hook(\"%s\", ...) call)",
				hook, hook,
			)
		}
	}
}

func TestInvZen169NoOpenClaudeImportsOrLiveCode(t *testing.T) {

	root := inv169RepoRoot(t)
	pluginRoot := filepath.Join(root, "plugin/hades")

	livePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*from\s+opencode[._a-zA-Z0-9]*\s+import`),
		regexp.MustCompile(`(?m)^\s*import\s+opencode[._a-zA-Z0-9]*`),
		regexp.MustCompile(`(?m)^\s*from\s+openclaude[._a-zA-Z0-9]*\s+import`),
		regexp.MustCompile(`(?m)^\s*import\s+openclaude[._a-zA-Z0-9]*`),
	}

	var violations []string
	err := filepath.Walk(pluginRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".py" {
			return nil
		}

		base := filepath.Base(path)
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, re := range livePatterns {
			if re.Match(raw) {
				violations = append(violations, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk plugin/: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf(
			"inv-zen-169: plugin tree contains LIVE OpenClaude/opencode imports "+
				"(ADR-0080 supersedes ADR-0001) in: %s. Documentary references "+
				"are permitted; only `import opencode...` or `from openclaude...` "+
				"forms are rejected.",
			strings.Join(violations, ", "),
		)
	}
}

func TestInvZen169Plan12PhaseBCommandCoverage(t *testing.T) {
	t.Parallel()

	root := inv169RepoRoot(t)
	src := inv169ReadFile(t, filepath.Join(root, "plugin/hades/__init__.py"))

	plan12Commands := []string{

		"brainstorm",
		"write-plan",
		"execute-plan",

		"doctrine",
		"amendment-list",
		"amendment-show",
		"amendment-ack",
		"amendment-deny",

		"impact-pre-merge",
		"audit-impact",
		"doctrine-drift-check",

		"knowledge-query",
		"knowledge-promote",

		"full",
		"voice",

		"openspec-apply",
		"openspec-archive",
		"openspec-propose",
		"openspec-resume",
	}
	if len(plan12Commands) != 19 {
		t.Fatalf("plan12Commands count is %d, expected 19", len(plan12Commands))
	}

	var missing []string
	for _, name := range plan12Commands {

		needle := `"` + name + `"`
		if !strings.Contains(src, needle) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf(
			"inv-zen-169 (Plan 12 Phase B): register(ctx) missing ctx.register_command() "+
				"calls for commands: %s — check plugin/hades/__init__.py Task B-3..B-8 blocks",
			strings.Join(missing, ", "),
		)
	}

	plan12Skills := []string{
		"brainstorm", "write-plan", "execute-plan", "doctrine", "amendment",
		"impact-pre-merge", "audit-impact", "doctrine-drift-check",
		"knowledge-query", "knowledge-promote",
	}
	for _, name := range plan12Skills {
		needle := `"` + name + `"`
		if !strings.Contains(src, needle) {
			t.Errorf(
				"inv-zen-169 (Plan 12 Phase B): register(ctx) missing ctx.register_skill() "+
					"call for skill %q — check plugin/hades/__init__.py Task B-9 block",
				name,
			)
		}
	}
}
