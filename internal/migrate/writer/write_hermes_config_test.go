package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"gopkg.in/yaml.v3"
)

func TestWriteHermesConfig_Shape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")

	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes:  []byte(`{"model":"opus[1m]","mcpServers":{}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	for _, req := range []string{
		"default_provider:",
		"mcp_servers:",
		"zen-swarm:",
		"# imported-from: claude-code",
	} {
		if !strings.Contains(s, req) {
			t.Errorf("missing %q: %s", req, s)
		}
	}

	if strings.Contains(s, "gitnexus:") {
		t.Errorf("gitnexus entry should not be emitted post-caronte-cutover:\n%s", s)
	}
}

func TestWriteHermesConfig_RawSourceMentioned(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/operator/.claude/settings.json",
		BodyBytes:  []byte(`{"model":"opus[1m]","mcpServers":{}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "/operator/.claude/settings.json") {
		t.Errorf("source path missing from output: %s", body)
	}
}

// TestWriteHermesConfig_PreservesOperatorModel — C-1 regression guard.
// Mapper synthesises BodyBytes with operator's model string; writer MUST
// emit it under default_provider.model. inv-zen-183 (no silent drop).
func TestWriteHermesConfig_PreservesOperatorModel(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes:  []byte(`{"model":"opus[1m]","mcpServers":{}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)

	var parsed struct {
		DefaultProvider struct {
			Name  string `yaml:"name"`
			Model string `yaml:"model"`
		} `yaml:"default_provider"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse failure: %v\nbody:\n%s", err, body)
	}
	if parsed.DefaultProvider.Model != "opus[1m]" {
		t.Errorf("model: got %q, want %q (operator's literal model preserved per amendment §2.4)\nbody:\n%s",
			parsed.DefaultProvider.Model, "opus[1m]", body)
	}
}

// TestWriteHermesConfig_PreservesOperatorMCPServers — C-1 regression guard.
// Mapper synthesises BodyBytes with operator's mcpServers map; writer MUST
// emit every server under mcp_servers (alongside the unconditional gateway
// entry zen-swarm per spec §3.1 Q1=B).
func TestWriteHermesConfig_PreservesOperatorMCPServers(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes: []byte(`{"model":"opus[1m]","mcpServers":{
			"playwright":{"command":"npx","args":["@playwright/mcp"],"env":{}},
			"postgres":{"command":"postgres-mcp","args":["--host","localhost"],"env":{"PG_USER":"op"}}
		}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	var parsed struct {
		MCPServers map[string]struct {
			Command string            `yaml:"command"`
			Args    []string          `yaml:"args"`
			Env     map[string]string `yaml:"env"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse failure: %v\nbody:\n%s", err, body)
	}
	// Operator's servers MUST be present.
	pw, ok := parsed.MCPServers["playwright"]
	if !ok {
		t.Fatalf("playwright MCP server dropped from output (inv-zen-183 violation)\nbody:\n%s", body)
	}
	if pw.Command != "npx" {
		t.Errorf("playwright command: got %q, want %q", pw.Command, "npx")
	}
	if len(pw.Args) != 1 || pw.Args[0] != "@playwright/mcp" {
		t.Errorf("playwright args: got %v, want [@playwright/mcp]", pw.Args)
	}
	pg, ok := parsed.MCPServers["postgres"]
	if !ok {
		t.Fatalf("postgres MCP server dropped from output\nbody:\n%s", body)
	}
	if pg.Env["PG_USER"] != "op" {
		t.Errorf("postgres env PG_USER: got %q, want %q", pg.Env["PG_USER"], "op")
	}

	if _, ok := parsed.MCPServers["zen-swarm"]; !ok {
		t.Errorf("zen-swarm gateway entry missing (must be unconditional)")
	}

	if _, ok := parsed.MCPServers["gitnexus"]; ok {
		t.Errorf("gitnexus entry must not be emitted post-caronte-cutover")
	}
}

// TestWriteHermesConfig_OperatorServerCollisionWithGateway — when operator
// names a server "zen-swarm", we MUST NOT silently overwrite either side.
// Operator's entry wins (inv-zen-183 no silent drop); a comment notes the
// collision.
func TestWriteHermesConfig_OperatorServerCollisionWithGateway(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes: []byte(`{"model":"opus[1m]","mcpServers":{
			"zen-swarm":{"command":"/operator/custom-zen","args":["--port","9000"],"env":{}}
		}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `yaml:"command"`
			Args    []string `yaml:"args"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse failure: %v\nbody:\n%s", err, body)
	}
	zs, ok := parsed.MCPServers["zen-swarm"]
	if !ok {
		t.Fatalf("zen-swarm entry missing entirely\nbody:\n%s", body)
	}

	if zs.Command != "/operator/custom-zen" {
		t.Errorf("operator's zen-swarm override dropped; got command %q (want %q)",
			zs.Command, "/operator/custom-zen")
	}
}

func TestWriteHermesConfig_DeterministicOrder(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes: []byte(`{"model":"opus[1m]","mcpServers":{
			"z-last":{"command":"a","args":[],"env":{}},
			"a-first":{"command":"a","args":[],"env":{}},
			"m-middle":{"command":"a","args":[],"env":{}}
		}}`),
	}
	var first []byte
	for i := 0; i < 3; i++ {
		path := filepath.Join(tmp, "config.yaml")
		if err := writeHermesConfig(path, e); err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(path)
		if first == nil {
			first = body
		} else if string(first) != string(body) {
			t.Errorf("non-deterministic output across runs:\nfirst:\n%s\nlater:\n%s", first, body)
		}
	}

	s := string(first)
	idxA := strings.Index(s, "a-first:")
	idxM := strings.Index(s, "m-middle:")
	idxZ := strings.Index(s, "z-last:")
	if idxA == -1 || idxM == -1 || idxZ == -1 {
		t.Fatalf("operator entries missing: %s", s)
	}
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("operator entries not in lex order: a=%d m=%d z=%d", idxA, idxM, idxZ)
	}
}

// TestWriteHermesConfig_EmptyBodyBytesFallback — defensive: malformed plan
// (BodyBytes nil or non-JSON) MUST NOT crash; emits gateway-only config +
// returns nil (graceful degradation).
func TestWriteHermesConfig_EmptyBodyBytesFallback(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	if !strings.Contains(s, "zen-swarm:") {
		t.Errorf("zen-swarm gateway entry missing in fallback path: %s", s)
	}

	if strings.Contains(s, "gitnexus:") {
		t.Errorf("gitnexus entry must not be emitted post-caronte-cutover:\n%s", s)
	}
}

func TestWriteHermesConfig_ZenSwarmGatewayHTTPTransport(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")

	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes:  []byte(`{"model":"opus[1m]","mcpServers":{}}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)

	// 1) Stdio form MUST NOT appear anywhere — this is the load-bearing assertion.
	bodyStr := string(body)
	if strings.Contains(bodyStr, "command: zen-swarm-ctld") {
		t.Errorf("Bug 3 not fixed — output still contains `command: zen-swarm-ctld` stdio form:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, `"--socket", "/tmp/zen-swarm.sock"`) {
		t.Errorf("Bug 3 not fixed — output still contains stdio args form for the daemon:\n%s", bodyStr)
	}

	var parsed struct {
		MCPServers map[string]map[string]any `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse failure: %v\nbody:\n%s", err, body)
	}
	zs, ok := parsed.MCPServers["zen-swarm"]
	if !ok {
		t.Fatalf("zen-swarm gateway entry missing from output:\n%s", body)
	}

	if got, want := zs["transport"], "http"; got != want {
		t.Errorf("transport: got %v, want %q", got, want)
	}
	if got, want := zs["url"], "http://unix/v1/mcpgateway"; got != want {
		t.Errorf("url: got %v, want %q", got, want)
	}
	if got, want := zs["socket"], "/tmp/zen-swarm.sock"; got != want {
		t.Errorf("socket: got %v, want %q", got, want)
	}
	// The HTTP entry MUST NOT carry a `command` key.
	if _, present := zs["command"]; present {
		t.Errorf("zen-swarm HTTP entry has `command` key — stdio leakage: %v", zs)
	}
}

func TestWriteHermesConfig_OperatorZenSwarmEntryUnchanged(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindHermesConfig,
		SourcePath: "/x/settings.json",
		BodyBytes: []byte(`{"model":"opus[1m]","mcpServers":{
            "zen-swarm":{"command":"/operator/custom-zen","args":["--port","9000"],"env":{}}
        }}`),
	}
	if err := writeHermesConfig(path, e); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "/operator/custom-zen") {
		t.Errorf("operator zen-swarm override dropped:\n%s", bodyStr)
	}

	if !strings.Contains(bodyStr, "collided with operator config") {
		t.Errorf("collision-comment line missing:\n%s", bodyStr)
	}

	var parsed struct {
		MCPServers map[string]map[string]any `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("YAML parse: %v", err)
	}
	zs := parsed.MCPServers["zen-swarm"]
	if zs["command"] != "/operator/custom-zen" {
		t.Errorf("operator zen-swarm command lost: %v", zs)
	}
	if _, hasTransport := zs["transport"]; hasTransport {
		t.Errorf("operator's zen-swarm entry was rewritten to HTTP transport (operator should win): %v", zs)
	}
}
