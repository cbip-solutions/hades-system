package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractRealHappyPath(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "cfg.json")
	cfg := &Config{
		Mode:      "extract",
		FlowsPath: "testdata/flow_dump_sample.json",
		OutPath:   out,
	}
	var stdout, stderr bytes.Buffer
	if err := runExtractReal(cfg, &stdout, &stderr); err != nil {
		t.Fatalf("runExtractReal: %v", err)
	}
	if !strings.Contains(stdout.String(), "wrote ") {
		t.Errorf("missing wrote line: %s", stdout.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"schema_version": "1"`) {
		t.Errorf("emitted JSON missing schema_version: %s", b)
	}
}

func TestRunExtractRealMissingFlows(t *testing.T) {
	cfg := &Config{Mode: "extract", FlowsPath: "/no/such/path", OutPath: t.TempDir() + "/cfg.json"}
	if err := runExtractReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunExtractRealUnwritable(t *testing.T) {

	dir := t.TempDir()
	flowsPath := filepath.Join(dir, "flows.json")
	if err := os.WriteFile(flowsPath, []byte(`{"flows":[{"request":{"host":"other.com"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Mode: "extract", FlowsPath: flowsPath, OutPath: dir + "/cfg.json"}
	if err := runExtractReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunExtractRealBadOutPath(t *testing.T) {
	cfg := &Config{
		Mode:      "extract",
		FlowsPath: "testdata/flow_dump_sample.json",
		OutPath:   "/no/such/dir/cfg.json",
	}
	if err := runExtractReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunCrossValidateRealHappyPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.json")
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	ec, _ := extractConfig(flows, "2026-04-30")
	if err := writeConfigJSON(ec, cfgPath); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "report.txt")
	cfg := &Config{Mode: "cross-validate", ConfigPath: cfgPath, Plugin: "meridian", ReportPath: report}
	var stdout bytes.Buffer
	if err := runCrossValidateReal(cfg, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCrossValidateReal: %v", err)
	}
	if !strings.Contains(stdout.String(), "ALL FIELDS MATCH") {
		t.Errorf("missing ALL FIELDS MATCH: %s", stdout.String())
	}
	if _, err := os.Stat(report); err != nil {
		t.Error("report not written")
	}
}

func TestRunCrossValidateRealMissingConfig(t *testing.T) {
	cfg := &Config{Mode: "cross-validate", ConfigPath: "/no/such/cfg.json", Plugin: "meridian", ReportPath: t.TempDir() + "/r.txt"}
	if err := runCrossValidateReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunCrossValidateRealBadJSON(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Mode: "cross-validate", ConfigPath: bad, Plugin: "meridian", ReportPath: dir + "/r.txt"}
	if err := runCrossValidateReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunCrossValidateRealUnknownPlugin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.json")
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	ec, _ := extractConfig(flows, "2026-04-30")
	_ = writeConfigJSON(ec, cfgPath)
	cfg := &Config{Mode: "cross-validate", ConfigPath: cfgPath, Plugin: "bogus", ReportPath: dir + "/r.txt"}
	if err := runCrossValidateReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestRunCrossValidateRealUnwritableReport(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.json")
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	ec, _ := extractConfig(flows, "2026-04-30")
	_ = writeConfigJSON(ec, cfgPath)
	cfg := &Config{Mode: "cross-validate", ConfigPath: cfgPath, Plugin: "meridian", ReportPath: "/no/such/dir/r.txt"}
	if err := runCrossValidateReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestLoadPluginFieldsAllBranches(t *testing.T) {
	if _, err := loadPluginFields("meridian", "testdata/meridian_plugin_sample.go"); err != nil {
		t.Errorf("meridian: %v", err)
	}
	if _, err := loadPluginFields("griffinmartin", "testdata/griffinmartin_plugin_sample.json"); err != nil {
		t.Errorf("griffinmartin: %v", err)
	}
}

func TestLoadEmbeddedPluginFields(t *testing.T) {

	if _, err := loadEmbeddedPluginFields("meridian"); err != nil {
		t.Errorf("meridian: %v", err)
	}
	if _, err := loadEmbeddedPluginFields("griffinmartin"); err != nil {
		t.Errorf("griffinmartin: %v", err)
	}
	if _, err := loadEmbeddedPluginFields("bogus"); err == nil {
		t.Error("bogus plugin = nil err, want error")
	}
}

func TestRunCrossValidateRealFromArbitraryCwd(t *testing.T) {

	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	cfgPath := filepath.Join(dir, "cfg.json")
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	ec, _ := extractConfig(flows, "2026-04-30")
	if err := writeConfigJSON(ec, cfgPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "report.txt")
	cfg := &Config{Mode: "cross-validate", ConfigPath: cfgPath, Plugin: "meridian", ReportPath: report}
	if err := runCrossValidateReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCrossValidateReal from arbitrary cwd: %v", err)
	}
}

func TestMainEntry(t *testing.T) {

	var stdout, stderr bytes.Buffer
	if rc := mainEntry([]string{"bogus"}, &stdout, &stderr); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(stderr.String(), "USAGE:") {
		t.Errorf("usage not in stderr: %s", stderr.String())
	}

	if rc := mainEntry(nil, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("nil args rc = %d, want 2", rc)
	}

	out := t.TempDir() + "/cfg.json"
	if rc := mainEntry([]string{"extract", "-flows", "testdata/flow_dump_sample.json", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 0 {
		t.Errorf("extract rc = %d, want 0", rc)
	}
}

func TestRunDispatcher(t *testing.T) {

	cfg := &Config{Mode: "extract", FlowsPath: "/no/such", OutPath: t.TempDir() + "/x.json"}
	if rc := run(cfg, &bytes.Buffer{}, &bytes.Buffer{}); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}

	cfg2 := &Config{Mode: "extract", FlowsPath: "testdata/flow_dump_sample.json", OutPath: t.TempDir() + "/x.json"}
	if rc := run(cfg2, &bytes.Buffer{}, &bytes.Buffer{}); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}

	cfg3 := &Config{Mode: "cross-validate", ConfigPath: "/no/such", Plugin: "meridian", ReportPath: t.TempDir() + "/r.txt"}
	if rc := run(cfg3, &bytes.Buffer{}, &bytes.Buffer{}); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}

	if CheckMitmStatus().Available {
		t.Skip("mitmproxy installed; cannot exercise the unavailable-path dispatch here")
	}
	cfg4 := &Config{Mode: "capture", OutPath: t.TempDir() + "/flows.json", ListenAddr: "127.0.0.1:0", CCBinary: "claude"}
	if rc := run(cfg4, &bytes.Buffer{}, &bytes.Buffer{}); rc != 1 {
		t.Errorf("capture rc = %d, want 1 (mitmproxy unavailable)", rc)
	}
}

func TestParseFlagsBadSubcommandFlag(t *testing.T) {
	if _, err := parseFlags([]string{"capture", "-bogus"}); err == nil {
		t.Fatal("err = nil")
	}
	if _, err := parseFlags([]string{"extract", "-bogus"}); err == nil {
		t.Fatal("err = nil")
	}
	if _, err := parseFlags([]string{"cross-validate", "-bogus"}); err == nil {
		t.Fatal("err = nil")
	}
}

func TestExtractMissingHeader(t *testing.T) {
	flows := []Flow{{
		Request: FlowRequest{
			Host:    "api.anthropic.com",
			Method:  "POST",
			Path:    "/v1/messages",
			Headers: [][]string{{"User-Agent", "X"}},
			Body:    "{}",
		},
	}}
	if _, err := extractConfig(flows, "2026-04-30"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestExtractNoMessagesEndpoint(t *testing.T) {
	flows := []Flow{{
		Request: FlowRequest{Host: "api.anthropic.com", Method: "GET", Path: "/v1/other"},
	}}
	if _, err := extractConfig(flows, "2026-04-30"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestCalVerBadDate(t *testing.T) {
	got := calVerForDate("not-a-date")
	if !strings.HasPrefix(got, "v") {
		t.Errorf("calVerForDate fallback = %q", got)
	}
}

func TestInferToolNameConventionEdge(t *testing.T) {
	if got := inferToolNameConvention(`{"no_tools":1}`); got != "PascalCase" {
		t.Errorf("no tools -> %q", got)
	}
	if got := inferToolNameConvention(`{"tools":[]}`); got != "PascalCase" {
		t.Errorf("empty tools -> %q", got)
	}
	if got := inferToolNameConvention(`{"tools":[{"name":""}]}`); got != "PascalCase" {
		t.Errorf("empty name -> %q", got)
	}
	if got := inferToolNameConvention(`{"tools":[{"name":"snake_thing"}]}`); got != "snake_case" {
		t.Errorf("snake -> %q", got)
	}
	if got := inferToolNameConvention(`{"tools":[{"name":"camelOne"}]}`); got != "camelCase" {
		t.Errorf("camel -> %q", got)
	}

	if got := inferToolNameConvention(`not json`); got != "PascalCase" {
		t.Errorf("invalid json -> %q", got)
	}
}

func TestClassifyConventionEmpty(t *testing.T) {
	if got := classifyConvention(""); got != "PascalCase" {
		t.Errorf("empty = %q", got)
	}
}

func TestHeaderValueMissing(t *testing.T) {
	if got := headerValue([][]string{{"a", "b"}}, "missing"); got != "" {
		t.Errorf("got %q", got)
	}
	if got := headerValue([][]string{{"only-one"}}, "x"); got != "" {
		t.Errorf("malformed kept: %q", got)
	}
}

func TestMitmStatusStringTrustedAndUntrusted(t *testing.T) {
	st := MitmStatus{Available: true, BinaryPath: "/usr/bin/mitmdump", CertTrusted: true}
	got := st.String()
	if !strings.Contains(got, "trusted") || strings.Contains(got, "NOT trusted") {
		t.Errorf("trusted form wrong: %s", got)
	}
	st2 := MitmStatus{Available: true, BinaryPath: "/usr/bin/mitmdump", CertTrusted: false}
	if !strings.Contains(st2.String(), "NOT trusted") {
		t.Errorf("untrusted form wrong: %s", st2.String())
	}
}

func TestMitmStatusForEmptyPath(t *testing.T) {
	st := mitmStatusForPath("")
	if st.Available {
		t.Error("empty path should be unavailable")
	}
	if st.InstallHint == "" {
		t.Error("hint should be set")
	}
}

func TestCheckMitmStatus(t *testing.T) {

	st := CheckMitmStatus()
	if st.InstallHint == "" {
		t.Error("InstallHint empty")
	}
}

func TestCertInstalled(t *testing.T) {

	_ = certInstalled()
}

func TestParseGriffinmartinBadJSON(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "g.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseGriffinmartinFixture(bad); err == nil {
		t.Fatal("err = nil")
	}
}

func TestParseGriffinmartinMissing(t *testing.T) {
	if _, err := parseGriffinmartinFixture("/no/such"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestParseMeridianMissing(t *testing.T) {
	if _, err := parseMeridianFixture("/no/such"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestParseMeridianNoExampleTool(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "m.go")
	content := `package x
const (
	UserAgent = "ua"
	XClaudeVersion = "1"
	XClaudeClient = "c"
	EndpointPath = "/v1/messages"
)
`
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pf, err := parseMeridianFixture(src)
	if err != nil {
		t.Fatal(err)
	}
	if pf.ToolNameConvention != "PascalCase" {
		t.Errorf("default = %q", pf.ToolNameConvention)
	}
}

func TestDiffReportMissing(t *testing.T) {
	ec := &ExtractedConfig{
		Headers:      map[string]string{"User-Agent": "x", "X-Claude-Version": "1", "X-Claude-Client": "c"},
		EndpointPath: "/v1/messages", ToolNameConvention: "PascalCase",
	}
	pf := PluginFields{}
	rep := buildDiffReport(ec, pf, "x")
	if !strings.Contains(rep, "MISSING") {
		t.Errorf("MISSING not present: %s", rep)
	}
}

func TestWriteConfigJSONUnwritable(t *testing.T) {
	cfg := &ExtractedConfig{}
	if err := writeConfigJSON(cfg, "/no/such/dir/file.json"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestLaunchMitmUnavailable(t *testing.T) {
	st := MitmStatus{Available: false, InstallHint: "x"}
	if _, err := LaunchMitm(nil, st, "127.0.0.1:8888", "/tmp/x"); err == nil {
		t.Fatal("err = nil")
	}
}

func TestFinalizeCaptureHappyPath(t *testing.T) {

	dir := t.TempDir()
	dump := filepath.Join(dir, "flows.json")
	srcBytes, err := os.ReadFile("testdata/flow_dump_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dump, srcBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := finalizeCapture(nil, func() {}, dump, &stdout); err != nil {
		t.Fatalf("finalizeCapture: %v", err)
	}
	if !strings.Contains(stdout.String(), "captured 2 anthropic flows") {
		t.Errorf("missing captured-line: %s", stdout.String())
	}
}

func TestFinalizeCaptureLoadFails(t *testing.T) {

	dir := t.TempDir()
	err := finalizeCapture(nil, func() {}, filepath.Join(dir, "missing.json"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("err = nil; want load failure")
	}
	if !strings.Contains(err.Error(), "validate captured dump") {
		t.Errorf("missing context: %v", err)
	}
}

func TestFinalizeCaptureEmptyDump(t *testing.T) {

	dir := t.TempDir()
	dump := filepath.Join(dir, "flows.json")
	if err := os.WriteFile(dump, []byte(`{"flows":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := finalizeCapture(nil, func() {}, dump, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "captured 0 anthropic flows") {
		t.Errorf("err = %v", err)
	}
}

func TestShutdownMitmdumpNilCmd(t *testing.T) {

	if err := shutdownMitmdump(nil, func() {}); err != nil {
		t.Errorf("nil cmd should be no-op: %v", err)
	}
}

func TestShutdownMitmdumpSIGINT(t *testing.T) {

	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := shutdownMitmdump(cmd, cancel); err != nil {
		t.Errorf("expected SIGINT-induced exit to be filtered as success: %v", err)
	}
}

func TestRunCaptureRealUnavailable(t *testing.T) {

	if CheckMitmStatus().Available {
		t.Skip("mitmproxy installed; cannot test the unavailable path here")
	}
	cfg := &Config{Mode: "capture", OutPath: t.TempDir() + "/flows.json", ListenAddr: "127.0.0.1:0", CCBinary: "claude"}
	if err := runCaptureReal(cfg, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("err = nil; expected mitmproxy-required error")
	}
}

func TestExtractedConfigJSONRoundTrip(t *testing.T) {
	flows, _ := loadFlowDump("testdata/flow_dump_sample.json")
	cfg, _ := extractConfig(flows, "2026-04-30")
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var back ExtractedConfig
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.SchemaVersion != "1" || back.ConfigVersion != "v2026.04.30.1" {
		t.Errorf("round-trip lost fields: %+v", back)
	}
	if len(back.SmokeProbes) != 6 {
		t.Errorf("len(SmokeProbes) = %d", len(back.SmokeProbes))
	}
}
