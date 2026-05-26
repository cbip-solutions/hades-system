package main

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestParseFlagsCapture(t *testing.T) {
	cfg, err := parseFlags([]string{"capture", "-out", "/tmp/flows.json", "-listen", "127.0.0.1:8888"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Mode != "capture" || cfg.OutPath != "/tmp/flows.json" || cfg.ListenAddr != "127.0.0.1:8888" {
		t.Errorf("got %+v", cfg)
	}
}

func TestParseFlagsExtract(t *testing.T) {
	cfg, err := parseFlags([]string{"extract", "-flows", "/tmp/f.json", "-out", "/tmp/c.json"})
	if err != nil || cfg.Mode != "extract" || cfg.FlowsPath != "/tmp/f.json" {
		t.Errorf("got %+v err=%v", cfg, err)
	}
}

func TestParseFlagsCrossValidate(t *testing.T) {
	cfg, err := parseFlags([]string{"cross-validate", "-config", "/tmp/c.json", "-plugin", "meridian"})
	if err != nil || cfg.Mode != "cross-validate" || cfg.Plugin != "meridian" {
		t.Errorf("got %+v err=%v", cfg, err)
	}
}

func TestParseFlagsUnknownMode(t *testing.T) {
	_, err := parseFlags([]string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("err=%v, want 'unknown mode'", err)
	}
}

func TestParseFlagsNoArgs(t *testing.T) {
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestUsageMentionsAllModes(t *testing.T) {
	var buf bytes.Buffer
	writeUsage(&buf)
	for _, m := range []string{"capture", "extract", "cross-validate"} {
		if !strings.Contains(buf.String(), m) {
			t.Errorf("usage missing %q", m)
		}
	}
}

func TestMitmStatusForMissingPath(t *testing.T) {
	st := mitmStatusForPath("/definitely/not/here/mitmdump")
	if st.Available {
		t.Error("Available = true for nonexistent path")
	}
	if st.InstallHint == "" {
		t.Error("InstallHint empty")
	}
}

func TestMitmStatusFormatting(t *testing.T) {
	st := MitmStatus{Available: false, InstallHint: "brew install mitmproxy"}
	got := st.String()
	if !strings.Contains(got, "brew install mitmproxy") || !strings.Contains(got, "NOT installed") {
		t.Errorf("String() = %q", got)
	}
}

func TestBuildMitmArgs(t *testing.T) {
	args := buildMitmArgs("127.0.0.1:8888", "/tmp/flows.json")
	joined := strings.Join(args, " ")
	for _, want := range []string{"127.0.0.1", "8888", "/tmp/flows.json", "anthropic"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

func TestBuildMitmArgs_UsesHardump(t *testing.T) {
	args := buildMitmArgs("127.0.0.1:8888", "/tmp/flows.json")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "hardump=/tmp/flows.json") {
		t.Errorf("args missing hardump=/tmp/flows.json (the JSON export option): %v", args)
	}
	// The binary tnetstring format MUST NOT be selected — flows.json
	// would not be parseable JSON.
	if strings.Contains(joined, "save_stream_file=") {
		t.Errorf("args still reference the binary save_stream_file option: %v", args)
	}
}

func TestExtractConfig_ModernCCHeaders(t *testing.T) {

	flow := Flow{
		Request: FlowRequest{
			Host:   "api.anthropic.com",
			Method: "POST",
			Path:   "/v1/messages",
			Headers: [][]string{
				{"User-Agent", "claude-cli/2.1.145 (external, cli)"},
				{"anthropic-version", "2023-06-01"},
				{"anthropic-beta", "oauth-2025-04-20,interleaved-thinking-2025-05-14"},
				{"x-app", "cli"},
				{"anthropic-dangerous-direct-browser-access", "true"},
				{"Content-Type", "application/json"},
			},
			Body: `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Bash"}]}`,
		},
		Response: FlowResponse{StatusCode: 200},
	}
	cfg, err := extractConfig([]Flow{flow}, "2026-05-20")
	if err != nil {
		t.Fatalf("extractConfig with modern CC headers must succeed: %v", err)
	}

	if cfg.Headers["User-Agent"] != "claude-cli/2.1.145 (external, cli)" {
		t.Errorf("User-Agent = %q", cfg.Headers["User-Agent"])
	}
	// Modern identification headers MUST be captured (the bypass module's
	// ConfigBackedRenderer copies them verbatim — without them the
	// proxied request looks malformed to api.anthropic.com).
	if cfg.Headers["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic-version = %q", cfg.Headers["anthropic-version"])
	}
	if !strings.HasPrefix(cfg.Headers["anthropic-beta"], "oauth-2025-04-20") {
		t.Errorf("anthropic-beta = %q", cfg.Headers["anthropic-beta"])
	}

	if cfg.Headers["x-app"] != "cli" {
		t.Errorf("x-app = %q (modern CC ships this)", cfg.Headers["x-app"])
	}

	if len(cfg.PatchesApplied) == 0 || !strings.Contains(cfg.PatchesApplied[0].Description, "2.1.145") {
		t.Errorf("PatchesApplied missing CC version 2.1.145: %+v", cfg.PatchesApplied)
	}
}

func TestExtractConfig_MissingAllCCIdentification(t *testing.T) {
	flow := Flow{
		Request: FlowRequest{
			Host:    "api.anthropic.com",
			Method:  "POST",
			Path:    "/v1/messages",
			Headers: [][]string{{"Content-Type", "application/json"}},
			Body:    `{}`,
		},
	}
	if _, err := extractConfig([]Flow{flow}, "2026-05-20"); err == nil {
		t.Fatal("extractConfig must fail when no CC identification header is captured")
	}
}

func TestSplitHARURL_DropsQueryString(t *testing.T) {
	host, path := splitHARURL("https://api.anthropic.com/v1/messages?beta=true")
	if host != "api.anthropic.com" {
		t.Errorf("host = %q, want api.anthropic.com", host)
	}
	if path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages (query MUST be dropped — extract.go matches the bare endpoint)", path)
	}

	_, bare := splitHARURL("https://api.anthropic.com/v1/messages")
	if bare != "/v1/messages" {
		t.Errorf("path (no query) = %q, want /v1/messages", bare)
	}

	_, root := splitHARURL("https://api.anthropic.com")
	if root != "/" {
		t.Errorf("path (root) = %q, want /", root)
	}
}

func TestBuildMitmArgs_AllowHostsMatchesHostPort(t *testing.T) {
	args := buildMitmArgs("127.0.0.1:8888", "/tmp/flows.json")
	var pattern string
	for i, a := range args {
		if a == "--allow-hosts" && i+1 < len(args) {
			pattern = args[i+1]
			break
		}
	}
	if pattern == "" {
		t.Fatal("--allow-hosts arg missing from buildMitmArgs output")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("regex %q does not compile: %v", pattern, err)
	}
	// Both the bare host (older mitmproxy semantics) and host:port (v10+)
	// MUST match, otherwise no TLS interception happens and flows.json
	// stays empty even though connections appear in mitmdump's log.
	for _, target := range []string{"api.anthropic.com", "api.anthropic.com:443"} {
		if !re.MatchString(target) {
			t.Errorf("regex %q does not match %q (mitmproxy v10+ evaluates allow-hosts against host:port)", pattern, target)
		}
	}
	// Negative-case guard: the regex MUST NOT silently match unrelated
	// hosts (e.g., a typo'd "evilapi.anthropic.com.attacker.example") —
	// keep the anchor on the right side of the domain to bound matches.
	for _, target := range []string{"evil-api.anthropic.com.attacker.example"} {
		if re.MatchString(target) {
			t.Errorf("regex %q over-matches %q (unbounded right side)", pattern, target)
		}
	}
}

func TestListenPort(t *testing.T) {
	if got := listenPort("127.0.0.1:9999"); got != "9999" {
		t.Errorf("listenPort = %q, want 9999", got)
	}
	if got := listenPort("noport"); got != "8888" {
		t.Errorf("listenPort fallback = %q, want 8888", got)
	}
}

func TestLoadFlowDumpFiltersAnthropic(t *testing.T) {
	flows, err := loadFlowDump("testdata/flow_dump_sample.json")
	if err != nil {
		t.Fatalf("loadFlowDump: %v", err)
	}
	if len(flows) != 2 {
		t.Errorf("len(flows) = %d, want 2 (anthropic only)", len(flows))
	}
	for _, f := range flows {
		if f.Request.Host != "api.anthropic.com" {
			t.Errorf("non-anthropic host kept: %q", f.Request.Host)
		}
	}
}

func TestLoadFlowDumpMissing(t *testing.T) {
	if _, err := loadFlowDump("testdata/does_not_exist.json"); err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestLoadFlowDumpInvalidJSON(t *testing.T) {
	tmp := t.TempDir() + "/bad.json"
	if err := os.WriteFile(tmp, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFlowDump(tmp); err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestExtractFromFixture(t *testing.T) {
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	cfg, err := extractConfig(flows, "2026-04-30")
	if err != nil {
		t.Fatalf("extractConfig: %v", err)
	}
	if cfg.SchemaVersion != "1" {
		t.Errorf("SchemaVersion = %q", cfg.SchemaVersion)
	}
	if cfg.ConfigVersion != "v2026.04.30.1" {
		t.Errorf("ConfigVersion = %q", cfg.ConfigVersion)
	}
	if cfg.Headers["User-Agent"] != "Claude-Code/2.1.115 (darwin; arm64)" {
		t.Errorf("User-Agent = %q", cfg.Headers["User-Agent"])
	}
	if cfg.Headers["X-Claude-Version"] != "2.1.115" {
		t.Errorf("X-Claude-Version = %q", cfg.Headers["X-Claude-Version"])
	}
	if cfg.Headers["X-Claude-Client"] != "claude-code" {
		t.Errorf("X-Claude-Client = %q", cfg.Headers["X-Claude-Client"])
	}
	if cfg.EndpointPath != "/v1/messages" || cfg.AuthScheme != "bearer-oauth" || cfg.StreamingProtocolVersion != "sse-v1" {
		t.Errorf("constants mismatch: %+v", cfg)
	}
	if cfg.ToolNameConvention != "PascalCase" {
		t.Errorf("ToolNameConvention = %q (fixture tool 'ReadFile')", cfg.ToolNameConvention)
	}
	if cfg.RequestBodyMungingRules == nil {
		t.Error("RequestBodyMungingRules must marshal as []")
	}
	wantProbes := []string{"single_message", "tool_use", "streaming", "multi_turn", "long_context", "vision"}
	if len(cfg.SmokeProbes) != 6 {
		t.Fatalf("len(SmokeProbes) = %d, want 6", len(cfg.SmokeProbes))
	}
	for i, want := range wantProbes {
		if cfg.SmokeProbes[i].Name != want {
			t.Errorf("SmokeProbes[%d].Name = %q, want %q", i, cfg.SmokeProbes[i].Name, want)
		}
	}
	if len(cfg.PatchesApplied) != 1 || !strings.Contains(cfg.PatchesApplied[0].Description, "2.1.115") {
		t.Errorf("PatchesApplied = %+v", cfg.PatchesApplied)
	}
}

func TestExtractEmptyErrors(t *testing.T) {
	if _, err := extractConfig(nil, "2026-04-30"); err == nil {
		t.Fatal("err = nil, want error")
	}
}

func TestCalVer(t *testing.T) {
	if got := calVerForDate("2026-04-30"); got != "v2026.04.30.1" {
		t.Errorf("calVerForDate = %q", got)
	}
}

func TestWriteConfigJSON(t *testing.T) {
	tmp := t.TempDir() + "/out.json"
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	cfg, _ := extractConfig(flows, "2026-04-30")
	if err := writeConfigJSON(cfg, tmp); err != nil {
		t.Fatalf("writeConfigJSON: %v", err)
	}
	b, _ := os.ReadFile(tmp)
	got := string(b)
	if !strings.Contains(got, `"schema_version": "1"`) || !strings.Contains(got, `"config_version": "v2026.04.30.1"`) {
		t.Errorf("emitted JSON missing required fields:\n%s", got)
	}
}

func TestClassifyConvention(t *testing.T) {
	cases := map[string]string{"ReadFile": "PascalCase", "readFile": "camelCase", "read_file": "snake_case"}
	for in, want := range cases {
		if got := classifyConvention(in); got != want {
			t.Errorf("classifyConvention(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMeridian(t *testing.T) {
	pf, err := parseMeridianFixture("testdata/meridian_plugin_sample.go")
	if err != nil {
		t.Fatal(err)
	}
	if pf.UserAgent != "Claude-Code/2.1.115 (darwin; arm64)" || pf.XClaudeVersion != "2.1.115" {
		t.Errorf("got %+v", pf)
	}
	if pf.ToolNameConvention != "PascalCase" {
		t.Errorf("ToolNameConvention = %q", pf.ToolNameConvention)
	}
}

func TestParseGriffinmartin(t *testing.T) {
	pf, err := parseGriffinmartinFixture("testdata/griffinmartin_plugin_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	if pf.XClaudeVersion != "2.1.114" || pf.ToolNameConvention != "PascalCase" {
		t.Errorf("got %+v", pf)
	}
}

func TestDiffReportDiff(t *testing.T) {
	ec := &ExtractedConfig{
		Headers: map[string]string{
			"User-Agent": "Claude-Code/2.1.115 (darwin; arm64)", "X-Claude-Version": "2.1.115", "X-Claude-Client": "claude-code",
		},
		EndpointPath: "/v1/messages", ToolNameConvention: "PascalCase",
	}
	pf := PluginFields{
		UserAgent: "Claude-Code/2.1.114 (darwin; arm64)", XClaudeVersion: "2.1.114",
		XClaudeClient: "claude-code", EndpointPath: "/v1/messages", ToolNameConvention: "PascalCase",
	}
	rep := buildDiffReport(ec, pf, "griffinmartin")
	if !strings.Contains(rep, "DIFF") || !strings.Contains(rep, "MATCH") || !strings.Contains(rep, "griffinmartin") {
		t.Errorf("report missing expected tokens:\n%s", rep)
	}
}

func TestDiffReportAllMatch(t *testing.T) {
	ec := &ExtractedConfig{
		Headers: map[string]string{
			"User-Agent": "Claude-Code/2.1.115 (darwin; arm64)", "X-Claude-Version": "2.1.115", "X-Claude-Client": "claude-code",
		},
		EndpointPath: "/v1/messages", ToolNameConvention: "PascalCase",
	}
	pf := PluginFields{
		UserAgent: "Claude-Code/2.1.115 (darwin; arm64)", XClaudeVersion: "2.1.115",
		XClaudeClient: "claude-code", EndpointPath: "/v1/messages", ToolNameConvention: "PascalCase",
	}
	rep := buildDiffReport(ec, pf, "meridian")
	if strings.Contains(rep, "DIFF") || !strings.Contains(rep, "ALL FIELDS MATCH") {
		t.Errorf("all-match report wrong:\n%s", rep)
	}
}

func TestUnknownPlugin(t *testing.T) {
	if _, err := loadPluginFields("frobnicate", "x"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestExtract_PreservesFingerprintHeaders(t *testing.T) {
	flow := Flow{
		Request: FlowRequest{
			Host:   "api.anthropic.com",
			Method: "POST",
			Path:   "/v1/messages",
			Headers: [][]string{

				{"User-Agent", "claude-cli/2.1.150 (external, cli)"},
				{"anthropic-version", "2023-06-01"},
				{"anthropic-beta", "oauth-2025-04-20,claude-code-20250219"},
				{"x-app", "cli"},
				{"anthropic-dangerous-direct-browser-access", "true"},
				{"Accept", "application/json"},
				{"Accept-Encoding", "gzip, deflate, br, zstd"},

				{"x-stainless-arch", "arm64"},
				{"x-stainless-lang", "js"},
				{"x-stainless-os", "MacOS"},
				{"x-stainless-package-version", "0.94.0"},
				{"x-stainless-runtime", "node"},
				{"x-stainless-runtime-version", "v24.3.0"},
				{"x-stainless-retry-count", "0"},
				{"x-stainless-timeout", "600"},

				{"x-claude-code-session-id", "b9181ec7-039f-4214-8555-831c8188c6a6"},
				{"x-client-request-id", "9e97bdf5-5d6d-4069-aeac-1496385306cf"},

				{"Host", "api.anthropic.com"},
				{"Content-Length", "323"},
				{"Authorization", "Bearer sk-ant-oat01-NOT-REAL"},
				{"Content-Type", "application/json"},
				{"Connection", "keep-alive"},
			},
			Body: `{"model":"claude-sonnet-4-5","max_tokens":20,"messages":[{"role":"user","content":"hi"}]}`,
		},
	}

	cfg, err := extractConfig([]Flow{flow}, "2026-05-23")
	if err != nil {
		t.Fatalf("extractConfig: %v", err)
	}

	required := []string{
		"User-Agent", "anthropic-version", "anthropic-beta", "x-app",
		"anthropic-dangerous-direct-browser-access", "Accept", "Accept-Encoding",
		"x-stainless-arch", "x-stainless-lang", "x-stainless-os",
		"x-stainless-package-version", "x-stainless-runtime",
		"x-stainless-runtime-version", "x-stainless-retry-count",
		"x-stainless-timeout",
		"x-claude-code-session-id", "x-client-request-id",
	}
	for _, h := range required {
		if _, ok := cfg.Headers[h]; !ok {
			t.Errorf("inv-zen-242 VIOLATED: extracted Headers missing %q (fingerprint header dropped — Anthropic anti-abuse will 429)", h)
		}
	}

	forbidden := []string{"Host", "Content-Length", "Authorization", "Content-Type", "Connection"}
	for _, h := range forbidden {
		if _, ok := cfg.Headers[h]; ok {
			t.Errorf("extracted Headers retains managed header %q (must be dropped — bypass engine owns it)", h)
		}
	}
}

func TestExtract_CapturesMetadataUserId(t *testing.T) {
	body := `{"model":"claude-opus-4-7","max_tokens":1,"messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"{\"device_id\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"account_uuid\":\"00000000-0000-0000-0000-000000000000\",\"session_id\":\"00000000-0000-0000-0000-000000000001\"}"}}`
	flow := Flow{
		Request: FlowRequest{
			Host:   "api.anthropic.com",
			Method: "POST",
			Path:   "/v1/messages",
			Headers: [][]string{
				{"User-Agent", "claude-cli/2.1.150 (external, cli)"},
				{"anthropic-version", "2023-06-01"},
				{"anthropic-beta", "oauth-2025-04-20"},
			},
			Body: body,
		},
	}
	cfg, err := extractConfig([]Flow{flow}, "2026-05-24")
	if err != nil {
		t.Fatalf("extractConfig: %v", err)
	}
	if cfg.MetadataTemplate == nil {
		t.Fatal("inv-zen-246 VIOLATED: extracted config has nil MetadataTemplate (extractor did NOT parse metadata.user_id from body)")
	}
	if cfg.MetadataTemplate.DeviceID != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("DeviceID = %q, want 64-zero synthetic", cfg.MetadataTemplate.DeviceID)
	}
	if cfg.MetadataTemplate.AccountUUID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("AccountUUID = %q, want nil-UUID synthetic", cfg.MetadataTemplate.AccountUUID)
	}
}

func TestExtract_MissingMetadataUserId_NilTemplate(t *testing.T) {
	flow := Flow{
		Request: FlowRequest{
			Host:   "api.anthropic.com",
			Method: "POST",
			Path:   "/v1/messages",
			Headers: [][]string{
				{"User-Agent", "claude-cli/2.1.145 (external, cli)"},
				{"anthropic-version", "2023-06-01"},
				{"anthropic-beta", "oauth-2025-04-20"},
			},
			Body: `{"model":"x","messages":[]}`,
		},
	}
	cfg, err := extractConfig([]Flow{flow}, "2026-05-24")
	if err != nil {
		t.Fatalf("extractConfig: %v", err)
	}
	if cfg.MetadataTemplate != nil {
		t.Errorf("MetadataTemplate = %+v, want nil for body without metadata.user_id", cfg.MetadataTemplate)
	}
}
