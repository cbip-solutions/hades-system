package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestWriteCommand_PythonHandler(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "commands", "hello.py")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindCommand,
		BodyBytes:  []byte("# hello slash command\nbody\n"),
		TargetPath: "plugin/zen-swarm/commands/hello.py",
	}
	if err := writeCommand(path, e); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "def hello_handler(raw_args: str) -> str | None:") {
		t.Errorf("handler def missing: %s", s)
	}
	if !strings.Contains(s, "operator extends as needed") {
		t.Errorf("operator hint missing")
	}

	sidecarPath := filepath.Join(filepath.Dir(path), "hello.md")
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar markdown missing: %v", err)
	}
	if string(sidecar) != "# hello slash command\nbody\n" {
		t.Errorf("sidecar body: got %q (want verbatim markdown)", sidecar)
	}

	if strings.Contains(s, "# hello slash command") {
		t.Errorf("wrapper inlined markdown body — sidecar pattern broken: %s", s)
	}
	if !strings.Contains(s, "hello.md") {
		t.Errorf("wrapper doesn't reference sidecar by name: %s", s)
	}
}

// TestWriteCommand_HostileBodyDoesNotExecute — C-3 security regression guard.
// Pre-fix: docstring `"""\n${BODY}\n"""` allowed body containing `"""` to
// terminate the docstring early; rest interpreted as Python. A body of
// `"""\nimport os\nos.system('echo PWNED')\n"""` would cause os.system()
// to fire at plugin import.
//
// Post-fix sidecar pattern: body lives in <name>.md (raw markdown, never
// parsed as Python). Wrapper contains zero operator content. This test
// verifies (a) hostile body lands verbatim in sidecar, (b) wrapper has
// no inlined body, (c) generated.py is syntactically valid Python.
func TestWriteCommand_HostileBodyDoesNotExecute(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "commands", "evil.py")
	hostile := `# /evil command

` + "```python\n" + `"""
import os
os.system('echo PWNED > /tmp/migrate_test_pwn')
"""
` + "```" + `
`
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindCommand,
		BodyBytes:  []byte(hostile),
		TargetPath: "plugin/zen-swarm/commands/evil.py",
	}
	if err := writeCommand(path, e); err != nil {
		t.Fatal(err)
	}
	wrapper, _ := os.ReadFile(path)
	if strings.Contains(string(wrapper), "os.system") {
		t.Errorf("wrapper inlined hostile body — RCE pre-fix re-introduced: %s", wrapper)
	}
	if strings.Contains(string(wrapper), `"""`) {

	}

	sidecarPath := filepath.Join(filepath.Dir(path), "evil.md")
	sidecar, _ := os.ReadFile(sidecarPath)
	if string(sidecar) != hostile {
		t.Errorf("sidecar mutated hostile body (sidecar pattern broken)")
	}

	pyParseOrSkip(t, wrapper)

	pyImportNoSideEffectOrSkip(t, wrapper, "/tmp/migrate_test_pwn_"+t.Name())
}

func TestWriteCommand_EmptyBodyErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hello.py")
	err := writeCommand(path, mapping.PlanEntry{TargetPath: "x/hello.py"})
	if err == nil {
		t.Errorf("expected error on empty body")
	}
}

func TestCommandNameFromTargetPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plugin/zen-swarm/commands/hello.py", "hello"},
		{"commands/foo.py", "foo"},
		{"x.py", "x"},
		{"no-extension", "no-extension"},
		{"", ""},
	}
	for _, c := range cases {
		got := commandNameFromTargetPath(c.in)
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPyIdentFromCommandName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello-world", "hello_world"},
		{"123abc", "_123abc"},
		{"pre.tool.call", "pre_tool_call"},
		{"", "_"},
		{"a-b-c", "a_b_c"},
		{"____", "____"},
	}
	for _, c := range cases {
		got := pyIdentFromCommandName(c.in)
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteCommand_HyphenatedSlashNameUsesUnderscoredFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	path := filepath.Join(tmp, "commands", "execute-plan.py")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindCommand,
		BodyBytes:  []byte("# /execute-plan slash command\nbody\n"),
		TargetPath: "plugin/zen-swarm/commands/execute-plan.py",
	}
	if err := writeCommand(path, e); err != nil {
		t.Fatalf("writeCommand: %v", err)
	}
	// Underscored on-disk file MUST exist; hyphenated form MUST NOT.
	underscoredPyPath := filepath.Join(tmp, "commands", "execute_plan.py")
	hyphenatedPyPath := filepath.Join(tmp, "commands", "execute-plan.py")
	if _, err := os.Stat(underscoredPyPath); err != nil {
		t.Errorf("expected underscored .py at %s: %v", underscoredPyPath, err)
	}
	if _, err := os.Stat(hyphenatedPyPath); err == nil {
		t.Errorf("hyphenated .py still present at %s — Bug 2 not fixed", hyphenatedPyPath)
	}

	underscoredMdPath := filepath.Join(tmp, "commands", "execute_plan.md")
	hyphenatedMdPath := filepath.Join(tmp, "commands", "execute-plan.md")
	if _, err := os.Stat(underscoredMdPath); err != nil {
		t.Errorf("expected underscored .md sidecar at %s: %v", underscoredMdPath, err)
	}
	if _, err := os.Stat(hyphenatedMdPath); err == nil {
		t.Errorf("hyphenated .md sidecar still present at %s — Bug 2 not fixed", hyphenatedMdPath)
	}
	// The rendered Python wrapper MUST define `execute_plan_handler` and reference
	// the underscored sidecar basename.
	body, err := os.ReadFile(underscoredPyPath)
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "def execute_plan_handler(raw_args: str) -> str | None:") {
		t.Errorf("wrapper missing underscored handler def: %s", s)
	}
	if !strings.Contains(s, "execute_plan.md") {
		t.Errorf("wrapper does not reference underscored sidecar basename: %s", s)
	}
	if strings.Contains(s, "execute-plan.md") {
		t.Errorf("wrapper still references hyphenated sidecar basename: %s", s)
	}
}

func TestWriteCommand_NonHyphenatedNamesUnaffected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "commands", "doctrine.py")
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindCommand,
		BodyBytes:  []byte("# doctrine slash command\nbody\n"),
		TargetPath: "plugin/zen-swarm/commands/doctrine.py",
	}
	if err := writeCommand(path, e); err != nil {
		t.Fatalf("writeCommand: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("doctrine.py should exist verbatim (no hyphens to translate): %v", err)
	}
	sidecar := filepath.Join(tmp, "commands", "doctrine.md")
	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("doctrine.md sidecar should exist: %v", err)
	}
}

func TestCommandFileBasename(t *testing.T) {
	t.Parallel()
	cases := []struct {
		slashName, want string
	}{
		{"execute-plan", "execute_plan"},
		{"write-plan", "write_plan"},
		{"amendment-list", "amendment_list"},
		{"doctrine", "doctrine"},
		{"openspec-apply", "openspec_apply"},

		{"already_underscored", "already_underscored"},

		{"a-b-c", "a_b_c"},
	}
	for _, c := range cases {
		got := commandFileBasename(c.slashName)
		if got != c.want {
			t.Errorf("commandFileBasename(%q): got %q, want %q", c.slashName, got, c.want)
		}
	}
}
