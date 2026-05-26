//go:build integration

package plan18a_integration_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPlan18aFoundation_HadesDashboardExecsZenTui(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)

	cases := []struct {
		name        string
		hadesArgs   []string
		wantZenArgs []string
	}{
		{"dashboard", []string{"dashboard"}, []string{"tui"}},
		{"tui_alias", []string{"tui"}, []string{"tui"}},
		{"panels_alias", []string{"panels"}, []string{"tui"}},
		{"dashboard_panel_codegraph", []string{"dashboard", "--panel=codegraph"}, []string{"tui", "--panel=codegraph"}},
		{"dashboard_panel_inbox", []string{"dashboard", "--panel=inbox"}, []string{"tui", "--panel=inbox"}},
		{"panels_panel_workforce", []string{"panels", "--panel=workforce"}, []string{"tui", "--panel=workforce"}},
		{"tui_panel_audit", []string{"tui", "--panel=audit"}, []string{"tui", "--panel=audit"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubDir := t.TempDir()
			zenRecord := filepath.Join(stubDir, "zen-record.jsonl")
			hermesRecord := filepath.Join(stubDir, "hermes-record.jsonl")
			_ = buildStubBinaryAt(t, stubDir, "zen", zenRecord, 0)
			_ = buildStubBinaryAt(t, stubDir, "hermes", hermesRecord, 0)

			env := newSandboxEnv(t, stubDir)
			cmd := exec.Command(hadesBin, tc.hadesArgs...)
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hades %v: %v\n%s", tc.hadesArgs, err, out)
			}

			invs := readStubInvocations(t, zenRecord)
			if len(invs) != 1 {
				t.Fatalf("stub zen invocations = %d, want 1\nrecord:\n%+v", len(invs), invs)
			}
			gotArgs := invs[0].Argv[1:]
			if !equalStrings(gotArgs, tc.wantZenArgs) {
				t.Errorf("stub zen argv[1:]=%v, want %v", gotArgs, tc.wantZenArgs)
			}

			if v, present := invs[0].Env["HERMES_SKIN"]; present {
				t.Errorf("HERMES_SKIN=%q leaked to zen on dashboard path; want unset", v)
			}

			hermesInvs := readStubInvocations(t, hermesRecord)
			if len(hermesInvs) != 0 {
				t.Errorf("stub hermes invoked %d times for `hades %v`; want 0 (dashboard routes through zen)", len(hermesInvs), tc.hadesArgs)
			}
		})
	}
}

func TestPlan18aFoundation_HadesPanelNamePassthroughAllValues(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)
	panels := []string{
		"workforce", "cost", "audit", "hra", "confirmations", "memory",
		"skills", "doctrine", "codegraph", "inbox", "crossproject", "help",
	}
	for _, p := range panels {
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			stubDir := t.TempDir()
			recordPath := filepath.Join(stubDir, "zen-record.jsonl")
			_ = buildStubBinaryAt(t, stubDir, "zen", recordPath, 0)
			_ = buildStubBinaryAt(t, stubDir, "hermes", filepath.Join(stubDir, "hermes-record.jsonl"), 0)
			env := newSandboxEnv(t, stubDir)
			cmd := exec.Command(hadesBin, "dashboard", "--panel="+p)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("hades dashboard --panel=%s: %v\n%s", p, err, out)
			}
			invs := readStubInvocations(t, recordPath)
			if len(invs) != 1 {
				t.Fatalf("want 1 zen invocation, got %d", len(invs))
			}
			want := []string{"tui", "--panel=" + p}
			if !equalStrings(invs[0].Argv[1:], want) {
				t.Errorf("argv[1:]=%v, want %v", invs[0].Argv[1:], want)
			}
		})
	}
}

func TestPlan18aFoundation_HadesBarePanelFlagRoutedToZen(t *testing.T) {
	t.Parallel()
	hadesBin := buildHadesBinary(t)
	stubDir := t.TempDir()
	zenRecord := filepath.Join(stubDir, "zen-record.jsonl")
	hermesRecord := filepath.Join(stubDir, "hermes-record.jsonl")
	_ = buildStubBinaryAt(t, stubDir, "zen", zenRecord, 0)
	_ = buildStubBinaryAt(t, stubDir, "hermes", hermesRecord, 0)

	env := newSandboxEnv(t, stubDir)
	cmd := exec.Command(hadesBin, "--panel=codegraph")
	cmd.Env = env
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Logf("hades --panel=codegraph: %v\noutput:\n%s", err, out)
	}

	zenInvs := readStubInvocations(t, zenRecord)
	if len(zenInvs) != 1 {
		t.Fatalf("stub zen invocations = %d for `hades --panel=codegraph`; want 1 (passthrough). hermesRec: %v\nrecord:\n%+v",
			len(zenInvs), readStubInvocations(t, hermesRecord), zenInvs)
	}
	gotArgs := zenInvs[0].Argv[1:]
	want := []string{"--panel=codegraph"}
	if !equalStrings(gotArgs, want) {
		t.Errorf("zen argv[1:]=%v, want %v (verbatim passthrough)", gotArgs, want)
	}

	// HERMES_SKIN MUST NOT be set on zen passthrough (cmd/hades/main.go:96-99).
	if v, present := zenInvs[0].Env["HERMES_SKIN"]; present {
		t.Errorf("HERMES_SKIN=%q leaked to zen passthrough; want unset", v)
	}

	hermesInvs := readStubInvocations(t, hermesRecord)
	if len(hermesInvs) != 0 {
		t.Errorf("hermes invoked %d times on `hades --panel=codegraph` passthrough; want 0", len(hermesInvs))
	}
}
