package cli

import (
	"strings"
	"testing"
)

func TestProbeStatusString(t *testing.T) {
	tests := []struct {
		s    ProbeStatus
		want string
	}{
		{ProbeOK, "ok"},
		{ProbeWarn, "warn"},
		{ProbeFail, "fail"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("ProbeStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestProbeStatusGlyph(t *testing.T) {
	tests := []struct {
		s    ProbeStatus
		want string
	}{
		{ProbeOK, "ok  "},
		{ProbeWarn, "warn"},
		{ProbeFail, "x   "},
	}
	for _, tt := range tests {
		if got := tt.s.Glyph(); got != tt.want {
			t.Errorf("ProbeStatus(%d).Glyph() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestProbeStatusUnknownString(t *testing.T) {
	got := ProbeStatus(99).String()
	if !strings.HasPrefix(got, "unknown(") {
		t.Errorf("unknown ProbeStatus String prefix: %q", got)
	}
}

func TestProbeStatusUnknownGlyph(t *testing.T) {
	got := ProbeStatus(99).Glyph()
	if got != "?   " {
		t.Errorf("unknown ProbeStatus Glyph = %q, want \"?   \"", got)
	}
}

func TestRenderProbesEmptySliceReturnsEmptyString(t *testing.T) {
	got := RenderProbes(nil)
	if got != "" {
		t.Errorf("RenderProbes(nil) = %q, want empty", got)
	}
	got = RenderProbes([]ProbeResult{})
	if got != "" {
		t.Errorf("RenderProbes([]) = %q, want empty", got)
	}
}

func TestRenderProbesSingleOK(t *testing.T) {
	probes := []ProbeResult{{
		Name:    "knowledge.index.current",
		Status:  ProbeOK,
		Message: "last update 2m ago",
	}}
	got := RenderProbes(probes)

	if !strings.Contains(got, "ok  ") {
		t.Errorf("missing OK glyph: %q", got)
	}
	if !strings.Contains(got, "knowledge.index.current") {
		t.Errorf("missing name: %q", got)
	}
	if !strings.Contains(got, "last update 2m ago") {
		t.Errorf("missing message: %q", got)
	}
}

func TestRenderProbesWarnIncludesHint(t *testing.T) {
	probes := []ProbeResult{{
		Name:    "scheduler.queue.depth",
		Status:  ProbeWarn,
		Message: "depth=7 threshold=10",
		Hint:    "consider operator override: zen project priority --reset",
	}}
	got := RenderProbes(probes)
	if !strings.Contains(got, "warn") {
		t.Errorf("missing warn glyph: %q", got)
	}
	if !strings.Contains(got, "hint: consider operator override") {
		t.Errorf("missing hint line: %q", got)
	}
}

func TestRenderProbesFailDetailMultiline(t *testing.T) {
	probes := []ProbeResult{{
		Name:    "tmux.server.reachable",
		Status:  ProbeFail,
		Message: "tmux has-session -t zen-internal-platform-x-12345678 returned 1",
		Detail:  "stderr line 1\nstderr line 2",
	}}
	got := RenderProbes(probes)
	if !strings.Contains(got, "x   ") {
		t.Errorf("missing fail glyph: %q", got)
	}
	if !strings.Contains(got, "stderr line 1") {
		t.Errorf("missing detail line 1: %q", got)
	}
	if !strings.Contains(got, "stderr line 2") {
		t.Errorf("missing detail line 2: %q", got)
	}
}

func TestRenderProbesLongNameTruncates(t *testing.T) {

	probes := []ProbeResult{{
		Name:    "this.is.a.very.long.subsystem.dot.aspect.name.that.exceeds.forty.chars",
		Status:  ProbeOK,
		Message: "ok",
	}}
	got := RenderProbes(probes)
	if !strings.Contains(got, "...") {
		t.Errorf("expected long name to be truncated with ellipsis: %q", got)
	}
}

func TestRenderProbesNoMessageNoTrailingSpace(t *testing.T) {
	probes := []ProbeResult{{
		Name:   "noisy.but.silent",
		Status: ProbeOK,
	}}
	got := RenderProbes(probes)

	lines := strings.Split(got, "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least one line, got %q", got)
	}

	if strings.HasSuffix(lines[0], "  ") {
		t.Errorf("expected no trailing two-space when message empty, got %q", lines[0])
	}
}

func TestRenderProbesMultipleStatuses(t *testing.T) {
	probes := []ProbeResult{
		{Name: "a", Status: ProbeOK, Message: "ok-a"},
		{Name: "b", Status: ProbeWarn, Message: "warn-b", Hint: "fix-b"},
		{Name: "c", Status: ProbeFail, Message: "fail-c", Detail: "d-c"},
	}
	got := RenderProbes(probes)
	for _, want := range []string{"ok-a", "warn-b", "fix-b", "fail-c", "d-c"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in render:\n%s", want, got)
		}
	}
}

func TestExitCodeAllOK(t *testing.T) {
	probes := []ProbeResult{
		{Name: "a", Status: ProbeOK},
		{Name: "b", Status: ProbeOK},
	}
	if got := ExitCode(probes, false); got != 0 {
		t.Errorf("ExitCode all-OK = %d, want 0", got)
	}
	if got := ExitCode(probes, true); got != 0 {
		t.Errorf("ExitCode all-OK strict = %d, want 0", got)
	}
}

func TestExitCodeAnyFail(t *testing.T) {
	probes := []ProbeResult{
		{Name: "a", Status: ProbeOK},
		{Name: "b", Status: ProbeFail},
	}
	if got := ExitCode(probes, false); got != 1 {
		t.Errorf("ExitCode any-Fail = %d, want 1", got)
	}
	if got := ExitCode(probes, true); got != 1 {
		t.Errorf("ExitCode any-Fail strict = %d, want 1", got)
	}
}

func TestExitCodeWarnOnlyDefaultIsZero(t *testing.T) {
	probes := []ProbeResult{
		{Name: "a", Status: ProbeOK},
		{Name: "b", Status: ProbeWarn},
	}
	if got := ExitCode(probes, false); got != 0 {
		t.Errorf("ExitCode warn-only (non-strict) = %d, want 0", got)
	}
}

func TestExitCodeStrictPromotesWarnToFail(t *testing.T) {
	probes := []ProbeResult{
		{Name: "a", Status: ProbeOK},
		{Name: "b", Status: ProbeWarn},
	}
	if got := ExitCode(probes, true); got != 1 {
		t.Errorf("ExitCode warn-only (strict) = %d, want 1", got)
	}
}

func TestExitCodeEmptyAlwaysZero(t *testing.T) {
	if got := ExitCode(nil, false); got != 0 {
		t.Errorf("ExitCode(nil, false) = %d, want 0", got)
	}
	if got := ExitCode(nil, true); got != 0 {
		t.Errorf("ExitCode(nil, true) = %d, want 0", got)
	}
	if got := ExitCode([]ProbeResult{}, false); got != 0 {
		t.Errorf("ExitCode([], false) = %d, want 0", got)
	}
}

func TestExitCodeFailDominatesWarnRegardlessOfStrict(t *testing.T) {

	probes := []ProbeResult{
		{Name: "a", Status: ProbeWarn},
		{Name: "b", Status: ProbeFail},
	}
	if got := ExitCode(probes, false); got != 1 {
		t.Errorf("ExitCode warn-then-fail (non-strict) = %d, want 1", got)
	}
}
