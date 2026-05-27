// go:build integration
package plan18a_integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan18aFoundation_HadesExecsHermesWithSkinEnv(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)

	stubDir := t.TempDir()
	recordPath := filepath.Join(stubDir, "hermes-record.jsonl")
	zenRecordPath := filepath.Join(stubDir, "zen-record.jsonl")

	_ = buildStubBinaryAt(t, stubDir, "hermes", recordPath, 0)
	// Stub `zen` present so PATH lookups during execHermes do not error;
	// it should NOT be invoked on the bare-invocation path.
	_ = buildStubBinaryAt(t, stubDir, "zen", zenRecordPath, 0)

	env := newSandboxEnv(t, stubDir)

	cmd := exec.Command(hadesBin)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hades exec failed: %v\noutput:\n%s", err, out)
	}

	invocations := readStubInvocations(t, recordPath)
	if len(invocations) == 0 {
		t.Fatalf("stub hermes was NOT invoked; wrapper output:\n%s", out)
	}
	if len(invocations) > 1 {
		t.Errorf("stub hermes invoked %d times; want exactly 1\nrecord:\n%+v", len(invocations), invocations)
	}
	inv := invocations[0]

	if got := inv.Env["HERMES_SKIN"]; got != "hades" {
		t.Errorf("HERMES_SKIN=%q in stub hermes env; want \"hades\"", got)
	}
	if !strings.HasSuffix(inv.Argv[0], "hermes") {
		t.Errorf("stub recorded Argv[0]=%q; want suffix 'hermes'", inv.Argv[0])
	}
	if len(inv.Argv) != 1 {
		t.Errorf("stub recorded Argv=%v; want exactly 1 element (no positional args)", inv.Argv)
	}

	// Sister-property guard: zen MUST NOT be invoked on the bare path.
	// The wrapper routes bare-invocation through hermes only.
	zenInvs := readStubInvocations(t, zenRecordPath)
	if len(zenInvs) != 0 {
		t.Errorf("stub zen was invoked %d times on bare-hades; want 0 (bare invocation routes only through hermes)\nrecord:\n%+v", len(zenInvs), zenInvs)
	}
}

func TestPlan18aFoundation_HadesNoWizardFlag(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)

	stubDir := t.TempDir()
	recordPath := filepath.Join(stubDir, "hermes-record.jsonl")
	_ = buildStubBinaryAt(t, stubDir, "hermes", recordPath, 0)
	_ = buildStubBinaryAt(t, stubDir, "zen", filepath.Join(stubDir, "zen-record.jsonl"), 0)

	env := newSandboxEnv(t, stubDir)
	cmd := exec.Command(hadesBin, "--no-wizard")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hades --no-wizard: %v\n%s", err, out)
	}

	invs := readStubInvocations(t, recordPath)
	if len(invs) != 1 {
		t.Fatalf("want exactly 1 hermes invocation, got %d\nrecord:\n%+v", len(invs), invs)
	}
	if invs[0].Env["HERMES_SKIN"] != "hades" {
		t.Errorf("HERMES_SKIN=%q; want \"hades\"", invs[0].Env["HERMES_SKIN"])
	}
	if invs[0].Env["HADES_NO_WIZARD"] != "1" {
		t.Errorf("HADES_NO_WIZARD=%q; want \"1\"", invs[0].Env["HADES_NO_WIZARD"])
	}
	// Sister-property: --no-wizard MUST be stripped from args (
	// strips it before exec; the flag is wrapper-scoped, not hermes-scoped).
	if len(invs[0].Argv) != 1 {
		t.Errorf("stub hermes received argv=%v; want exactly 1 element (--no-wizard stripped)", invs[0].Argv)
	}
}

func TestPlan18aFoundation_HadesNoWizardDoesNotLeakToZen(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)

	stubDir := t.TempDir()
	recordPath := filepath.Join(stubDir, "zen-record.jsonl")
	_ = buildStubBinaryAt(t, stubDir, "zen", recordPath, 0)
	_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "hermes-record.jsonl"), 0)

	env := newSandboxEnv(t, stubDir)

	cmd := exec.Command(hadesBin, "doctor", "--no-wizard")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hades doctor --no-wizard: %v\n%s", err, out)
	}

	invs := readStubInvocations(t, recordPath)
	if len(invs) != 1 {
		t.Fatalf("want 1 zen invocation, got %d\nrecord:\n%+v", len(invs), invs)
	}

	if v, present := invs[0].Env["HADES_NO_WIZARD"]; present {
		t.Errorf("HADES_NO_WIZARD=%q leaked to zen passthrough; want unset", v)
	}

	if v, present := invs[0].Env["HERMES_SKIN"]; present {
		t.Errorf("HERMES_SKIN=%q leaked to zen passthrough; want unset", v)
	}
	// --no-wizard MUST appear in zen's argv (operator-intent passthrough).
	gotArgs := invs[0].Argv[1:]
	if !equalStrings(gotArgs, []string{"doctor", "--no-wizard"}) {
		t.Errorf("zen argv[1:]=%v; want [doctor --no-wizard] (verbatim passthrough)", gotArgs)
	}
}
