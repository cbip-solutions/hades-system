package graphql

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

type fakeAudit struct {
	events []recordedEvent
	err    error
}

type recordedEvent struct {
	eventType   string
	workspaceID string
	payload     []byte
}

func (f *fakeAudit) Emit(_ context.Context, eventType, workspaceID string, payload []byte) error {
	f.events = append(f.events, recordedEvent{eventType: eventType, workspaceID: workspaceID, payload: append([]byte(nil), payload...)})
	return f.err
}

func TestNodeFallbackGateClosedSkipsSpawn(t *testing.T) {
	a := &fakeAudit{}
	nf := NewNodeFallback(br.DefaultParams(), a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: br.SevInsufficient},
	}
	out, err := nf.MaybeRun(context.Background(), []byte("type Q {x:Int}"), []byte("type Q {y:Int}"), goResult, false)
	if err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}
	if len(out) != 1 || out[0].Severity != br.SevInsufficient {
		t.Errorf("gate-closed must return goResult unchanged; got %+v", out)
	}
	if len(a.events) != 0 {
		t.Errorf("gate-closed must not audit; got %d events", len(a.events))
	}
}

func TestNodeFallbackGateOpenNoInsufficientSkipsSpawn(t *testing.T) {
	a := &fakeAudit{}
	nf := NewNodeFallback(br.DefaultParams(), a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "FIELD_REMOVED", Severity: br.SevBreaking},
		{DetectorID: "gqlparser", Kind: "ENUM_VALUE_REMOVED", Severity: br.SevBreaking},
	}
	out, err := nf.MaybeRun(context.Background(), nil, nil, goResult, true)
	if err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("expected goResult unchanged (len 2); got %d", len(out))
	}
	if len(a.events) != 0 {
		t.Errorf("no-insufficient must not audit; got %d", len(a.events))
	}
}

func TestNodeFallbackErrNodeBinaryMissing(t *testing.T) {
	a := &fakeAudit{}
	p := br.DefaultParams()
	p.NodeBinaryPath = "/definitely/nonexistent/node"
	nf := NewNodeFallback(p, a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: br.SevInsufficient},
	}
	_, err := nf.MaybeRun(context.Background(), nil, nil, goResult, true)
	if !errors.Is(err, br.ErrNodeBinaryMissing) {
		t.Errorf("err = %v; want ErrNodeBinaryMissing", err)
	}
	if len(a.events) != 1 {
		t.Errorf("expected 1 audit event for failed spawn; got %d", len(a.events))
	}
	if len(a.events) > 0 {
		got := a.events[0].eventType
		want := "plan20.graphql_node_fallback_spawn"
		if got != want {
			t.Errorf("audit eventType = %q; want %q", got, want)
		}
		if a.events[0].workspaceID != "ws-1" {
			t.Errorf("audit workspaceID = %q; want ws-1", a.events[0].workspaceID)
		}

		if !strings.Contains(string(a.events[0].payload), "binary_missing") {
			t.Errorf("audit payload missing binary_missing outcome: %s", a.events[0].payload)
		}
	}
}

func TestNodeFallbackSpawnSucceeds(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		// Per code-review M-5 fix: fail-loud on missing /bin/sh rather than
		// t.Skip — the test silently skipping would mask a CI-image
		// regression that hides every spawn-site behaviour assertion. Every
		// macOS + Linux CI image MUST have /bin/sh; its absence is an
		// environmental defect not a test-skip condition.
		t.Fatal("/bin/sh missing — environmental regression; expected on all macOS/Linux CI images (per code-review M-5: fail-loud over t.Skip)")
	}

	tmpScript := writeTempScript(t, `#!/bin/sh
echo '[{"type":"DIRECTIVE_ARGUMENT_REMOVED","criticality":{"level":"BREAKING"},"message":"custom"}]'`)
	p := br.DefaultParams()
	p.NodeBinaryPath = sh

	p.NodeBinaryPath = tmpScript
	a := &fakeAudit{}
	nf := NewNodeFallback(p, a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "FIELD_REMOVED", Severity: br.SevBreaking},
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_DIRECTIVE_ARGUMENT_ADDED", Severity: br.SevInsufficient},
	}
	out, err := nf.MaybeRun(context.Background(), []byte("type Q {x:Int}"), []byte("type Q {y:Int}"), goResult, true)
	if err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if len(a.events) != 1 {
		t.Errorf("expected 1 audit event; got %d", len(a.events))
	}

	hasCanonical := false
	hasNodeResolved := false
	for _, r := range out {
		if r.Severity == br.SevBreaking && r.Kind == "FIELD_REMOVED" {
			hasCanonical = true
		}
		if r.DetectorID == "node-graphql-inspector" {
			hasNodeResolved = true
		}
	}
	if !hasCanonical {
		t.Errorf("expected canonical FIELD_REMOVED preserved; got %+v", out)
	}
	if !hasNodeResolved {
		t.Errorf("expected ≥1 node-graphql-inspector classified entry; got %+v", out)
	}
}

func TestNodeFallbackDetectorID(t *testing.T) {
	nf := NewNodeFallback(br.DefaultParams(), &fakeAudit{}, "ws-1")
	if nf.DetectorID() != "node-graphql-inspector" {
		t.Errorf("DetectorID = %q; want node-graphql-inspector", nf.DetectorID())
	}
}

func TestNodeFallbackSpawnTimeout(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {

		t.Fatal("/bin/sh missing — environmental regression; expected on all macOS/Linux CI images (per code-review M-5: fail-loud over t.Skip)")
	}

	tmpScript := writeTempScript(t, `#!/bin/sh
sleep 5
echo "[]"`)
	p := br.DefaultParams()
	_ = sh
	p.NodeBinaryPath = tmpScript
	p.NodeSpawnTimeout = 100 * time.Millisecond
	a := &fakeAudit{}
	nf := NewNodeFallback(p, a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: br.SevInsufficient},
	}
	_, err = nf.MaybeRun(context.Background(), nil, nil, goResult, true)
	if err == nil {
		t.Fatal("expected spawn-timeout error; got nil")
	}

	if len(a.events) != 1 {
		t.Errorf("expected 1 audit event on timeout; got %d", len(a.events))
	}
}

func TestNodeFallbackAuditEmitErrorWrapped(t *testing.T) {
	a := &fakeAudit{err: errors.New("tessera unreachable")}
	p := br.DefaultParams()
	p.NodeBinaryPath = "/nonexistent/node"
	nf := NewNodeFallback(p, a, "ws-1")
	goResult := []br.DiffResult{
		{DetectorID: "gqlparser", Kind: "INSUFFICIENT_X", Severity: br.SevInsufficient},
	}
	_, err := nf.MaybeRun(context.Background(), nil, nil, goResult, true)

	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !errors.Is(err, br.ErrNodeBinaryMissing) {
		t.Errorf("err = %v; want ErrNodeBinaryMissing (audit failure must NOT mask the primary error)", err)
	}
}

func TestLevelToSeverityMapsAllCriticalityLevels(t *testing.T) {
	cases := []struct {
		level string
		want  br.Severity
	}{
		{"BREAKING", br.SevBreaking},
		{"DANGEROUS", br.SevDangerous},
		{"NON_BREAKING", br.SevNonBreaking},
		{"UNKNOWN_FUTURE_LEVEL", br.SevInsufficient},
		{"", br.SevInsufficient},
	}
	for _, tc := range cases {
		got := levelToSeverity(tc.level)
		if got != tc.want {
			t.Errorf("levelToSeverity(%q) = %q; want %q", tc.level, got, tc.want)
		}
	}
}

func TestSpawnOutcomeForMapsErrorClass(t *testing.T) {
	if got := spawnOutcomeFor(nil); got != "success" {
		t.Errorf("spawnOutcomeFor(nil) = %q; want success", got)
	}
	if got := spawnOutcomeFor(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("spawnOutcomeFor(DeadlineExceeded) = %q; want timeout", got)
	}
	if got := spawnOutcomeFor(errors.New("random")); got != "exec_error" {
		t.Errorf("spawnOutcomeFor(random) = %q; want exec_error", got)
	}
}

func TestParseNodeOutputInvalidJSON(t *testing.T) {
	_, err := parseNodeOutput([]byte("not valid json {{}}"))
	if err == nil {
		t.Error("expected parse error for invalid JSON; got nil")
	}
}

func TestParseNodeOutputEmptyArray(t *testing.T) {
	got, err := parseNodeOutput([]byte("[]"))
	if err != nil {
		t.Errorf("parseNodeOutput([]) err = %v; want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results; want 0", len(got))
	}
}

func TestExitCodeOfNilErrAndNonExitError(t *testing.T) {
	if got := exitCodeOf(nil); got != 0 {
		t.Errorf("exitCodeOf(nil) = %d; want 0", got)
	}
	if got := exitCodeOf(errors.New("not an ExitError")); got != -1 {
		t.Errorf("exitCodeOf(random) = %d; want -1", got)
	}
}

func writeTempScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/stub.sh"
	if err := writeFileExecutable(path, []byte(content)); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return path
}

// writeFileExecutable writes content to path with the executable bit set on
// the owner — test-only helper that installs stub scripts as
// Params.NodeBinaryPath targets in the spawn-site behaviour tests. Moved
// here from nodefallback.go per code-review I-1 (test-only helper MUST NOT
// live in production code — testing-anti-patterns "production pollution").
func writeFileExecutable(path string, content []byte) error {
	return os.WriteFile(path, content, 0o700)
}
