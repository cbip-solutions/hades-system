package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZenF1ToxiproxySetupScriptPresent(t *testing.T) {
	path := filepath.Join(repoRootF1(t), "scripts", "setup_toxiproxy_dev.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-305: %s missing: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("inv-zen-305: %s not executable (mode=%o)", path, info.Mode())
	}
}

func TestInvZenF1ToxiproxyPlistRendererPresent(t *testing.T) {
	path := filepath.Join(repoRootF1(t), "scripts", "chaos", "render_toxiproxy_plist.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-305: %s missing: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("inv-zen-305: %s not executable (mode=%o)", path, info.Mode())
	}
}

func TestInvZenF1ToxiproxyCISidecarScriptPresent(t *testing.T) {
	path := filepath.Join(repoRootF1(t), "scripts", "chaos", "ci_toxiproxy_service.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-305: %s missing: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("inv-zen-305: %s not executable (mode=%o)", path, info.Mode())
	}
}

func TestInvZenF1ToxiproxyPlistRendererEmitsValidXML(t *testing.T) {
	script := filepath.Join(repoRootF1(t), "scripts", "chaos", "render_toxiproxy_plist.sh")
	cmd := exec.Command("bash", script)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("inv-zen-305: render_toxiproxy_plist.sh failed: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<key>Label</key>`,
		`com.hades-system.zen-toxiproxy-dev`,
		`<key>RunAtLoad</key>`,
		`<true/>`,
		`<key>KeepAlive</key>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-305: rendered plist missing %q\n%s", want, s)
		}
	}
}

func TestInvZenF1ToxiproxyMakefileTargetPresent(t *testing.T) {
	mk := filepath.Join(repoRootF1(t), "Makefile")
	data, err := os.ReadFile(mk)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	for _, want := range []string{
		"toxiproxy-install:",
		"toxiproxy-uninstall:",
		"toxiproxy-print-config:",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("inv-zen-305: Makefile missing target %q", want)
		}
	}
}

func repoRootF1(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err != nil {
		t.Fatalf("repo root sanity check failed at %s: %v", root, err)
	}
	return root
}

func TestInvZenF2GofailCommentsPresent(t *testing.T) {
	type site struct {
		filename string
		varName  string
	}

	sites := []site{
		{"internal/audit/chain/seal.go", "auditWALFsync"},
		{"internal/daemon/dispatcher/dispatcher.go", "dispatcherCancelMidFlight"},
		{"internal/orchestrator/merge/candidate_apply.go", "mergeEngineApplyConflict"},
		{"internal/audit/litestream/rsync.go", "litestreamWALFlush"},
		{"internal/audit/tessera/adapter.go", "tesseraTileUpload"},
		{"internal/daemon/orchestrator/circuit_breaker.go", "breakerTransitionRace"},
		{"internal/scheduler/fire.go", "schedulerTickMiss"},
		{"internal/orchestrator/worktreepool/pool.go", "worktreepoolAcquireTimeout"},
		{"internal/daemon/handlers/hermes_probe.go", "pluginRPCBoundary"},
		{"internal/daemon/handlers/bypass.go", "sidecarRPCBoundary"},
		{"internal/audit/chain/backfill.go", "auditChainAnchor"},
		{"internal/daemon/aggregatorbridge/bridge.go", "aggregatorIndexCorruption"},
		{"internal/daemon/orchestrator/cost_counters.go", "costLedgerRebuild"},
		{"internal/doctrine/reload/validate_swap.go", "doctrineReloadRace"},
		{"internal/augment/privacy.go", "privacyClassifierSidecarTimeout"},
	}
	root := repoRootF1(t)
	for _, s := range sites {
		t.Run(s.varName, func(t *testing.T) {
			path := filepath.Join(root, s.filename)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("inv-zen-306: %s missing: %v", path, err)
			}

			needle := "// gofail:"
			if !strings.Contains(string(data), needle) {
				t.Fatalf("inv-zen-306: %s has no gofail directive at all", s.filename)
			}
			marker := "gofail: var " + s.varName
			if !strings.Contains(string(data), marker) {
				t.Errorf("inv-zen-306: %s missing gofail var %q", s.filename, s.varName)
			}
		})
	}
}

func TestInvZenF2GofailMakefileTargetsPresent(t *testing.T) {
	mk := filepath.Join(repoRootF1(t), "Makefile")
	data, err := os.ReadFile(mk)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	for _, want := range []string{
		"gofail-install:",
		"gofail-enable:",
		"gofail-disable:",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("inv-zen-306: Makefile missing target %q", want)
		}
	}
}

func TestInvZenF3MakefileCadenceTargetsPresent(t *testing.T) {
	mk := filepath.Join(repoRootF1(t), "Makefile")
	targets := []string{
		`^test-chaos-network:`,
		`^test-chaos-failpoint:`,
		`^test-dst-pr:`,
		`^test-dst-nightly:`,
		`^test-dst-release:`,
		`^test-soak-24h:`,
		`^verify-chaos-suite:`,
	}
	for _, pat := range targets {
		t.Run(pat, func(t *testing.T) {
			out, err := exec.Command("grep", "-nE", pat, mk).CombinedOutput()
			if err != nil || len(out) == 0 {
				t.Fatalf("inv-zen-307: Makefile missing target matching %s:\n%s", pat, out)
			}
		})
	}
}

func TestInvZenF3ChaosWorkflowExtended(t *testing.T) {
	path := filepath.Join(repoRootF1(t), ".github", "workflows", "chaos.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chaos.yml: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"chaos-smoke",
		"chaos-nightly",
		"chaos-release-dst",
		"chaos-release-full",
		"make test-chaos-network",
		"make test-chaos-failpoint",
		"make test-dst-pr",
		"make test-dst-nightly",
		"ZEN_DST_SEED_BUDGET",
		"ZEN_DST_SEED_OFFSET",
		"matrix:",
		"shard:",
		"ci_toxiproxy_service.sh",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-307: chaos.yml missing marker %q", want)
		}
	}
}

func TestInvZenF3ChaosHandbookPresent(t *testing.T) {
	path := filepath.Join(repoRootF1(t), "docs", "operations", "chaos-engineering.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-307: %s missing: %v", path, err)
	}
	s := string(data)
	for _, want := range []string{
		"## Toxiproxy setup",
		"## gofail invocation",
		"## DST seed reproduction",
		"## 24h soak procedure",
		"## Persisted regression seeds",
		"`make test-dst-pr`",
		"`make test-dst-release`",
		"`make test-chaos-network`",
		"`make test-chaos-failpoint`",
		"`make test-soak-24h`",
		"`make verify-chaos-suite`",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-307: chaos-engineering.md missing marker %q", want)
		}
	}

	if got := len(strings.Split(s, "\n")); got < 250 {
		t.Errorf("inv-zen-307: chaos-engineering.md too short (%d lines); spec §6.5 calls for ~400", got)
	}
}

func TestInvZenF4SoakMakefileTargetPresent(t *testing.T) {
	mk := filepath.Join(repoRootF1(t), "Makefile")
	out, err := exec.Command("grep", "-nE", "^test-soak-24h:", mk).CombinedOutput()
	if err != nil || len(out) == 0 {
		t.Fatalf("inv-zen-308: Makefile missing test-soak-24h target:\n%s", out)
	}
	// Sister-assertion: the target MUST set ZEN_SOAK_DURATION so the
	// soak duration is opt-in via env (cadence env-var contract).
	data, err := os.ReadFile(mk)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if !strings.Contains(string(data), "ZEN_SOAK_DURATION") {
		t.Error("inv-zen-308: Makefile test-soak-24h missing ZEN_SOAK_DURATION env-var wiring")
	}
}

func TestInvZenF4SoakHandbookSection(t *testing.T) {
	hb := filepath.Join(repoRootF1(t), "docs", "operations", "chaos-engineering.md")
	data, err := os.ReadFile(hb)
	if err != nil {
		t.Fatalf("read handbook: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"## 24h soak procedure",
		"### Pre-flight",
		"### Launch",
		"### Monitor",
		"### Post-flight",
		"nohup make test-soak-24h",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-308: handbook 24h soak section missing %q", want)
		}
	}
}

// ─── inv-zen-309: per-package property tests preserved ───────────────
//
// Per spec §6.2 the existing 13+ chaos files + Plan 7 K-tier property
// suite + replay/timeaccel suites MUST be preserved alongside the new
// DST harness (DST extends, NOT replaces). F-5 asserts the canonical
// baseline files all still exist; a regression here is the
// preservation-contract alarm.

func TestInvZenF5Legacy13ChaosFilesPresent(t *testing.T) {
	// The canonical 13+ preserved-baseline chaos files (per spec §6.2)
	// MUST all still exist. DST harness EXTENDS the baseline; it does
	// NOT replace it. If a file is renamed or removed the
	// preservation contract is broken and the operator needs to know.
	root := repoRootF1(t)
	for _, want := range []string{
		"tests/chaos/bypass_chaos_test.go",
		"tests/chaos/merge_chaos_test.go",
		"tests/chaos/clock_drift_test.go",
		"tests/chaos/daemon_panic_recovery_test.go",
		"tests/chaos/disk_full_test.go",
		"tests/chaos/knowledge_watcher_cpu_spike_test.go",
		"tests/chaos/inbox_aggregator_divergence_test.go",
		"tests/chaos/scheduler_concurrent_dispatch_test.go",
		"tests/chaos/tmuxlife_kill_test.go",
		"tests/chaos/plan9_audit_chaos",
		"tests/chaos/plan9_knowledge_chaos",
		"tests/chaos/chaos_placeholder_test.go",
		"tests/chaos/README.md",
	} {
		path := filepath.Join(root, want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("inv-zen-309: %s missing (preservation contract violation)", want)
		}
	}
}

func TestInvZenF5ExistingPropertyTestsPresent(t *testing.T) {

	root := repoRootF1(t)
	matches, err := filepath.Glob(filepath.Join(root, "tests", "property", "*.go"))
	if err != nil {
		t.Fatalf("glob property/: %v", err)
	}
	if len(matches) == 0 {
		t.Error("inv-zen-309: tests/property/ empty (Plan 7 K-tier property surface lost)")
	}
}

func TestInvZenF5ChaosPlaceholderPreserved(t *testing.T) {
	// The placeholder file's build tag is the canonical "chaos tag is
	// wired" assertion from Plan 1. It MUST survive Phase F.
	root := repoRootF1(t)
	data, err := os.ReadFile(filepath.Join(root, "tests", "chaos", "chaos_placeholder_test.go"))
	if err != nil {
		t.Fatalf("read placeholder: %v", err)
	}
	if !strings.Contains(string(data), "//go:build chaos") {
		t.Error("inv-zen-309: chaos_placeholder_test.go lost //go:build chaos directive")
	}
}

func TestInvZenF5ReplayAndTimeaccelPreserved(t *testing.T) {

	root := repoRootF1(t)
	for _, want := range []string{
		"tests/replay",
		"tests/timeaccel",
	} {
		path := filepath.Join(root, want)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("inv-zen-309: %s missing or not a directory", want)
		}
	}
}

func TestInvZenF3VerifyChaosSuiteBinaryPresent(t *testing.T) {
	root := repoRootF1(t)
	for _, want := range []string{
		"cmd/verify-chaos-suite/main.go",
		"cmd/verify-chaos-suite/main_test.go",
	} {
		path := filepath.Join(root, want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("inv-zen-307: %s missing: %v", want, err)
		}
	}
}

func TestInvZenF3SmokeAggregatorPresent(t *testing.T) {
	mk := filepath.Join(repoRootF1(t), "Makefile")
	out, err := exec.Command("grep", "-nE", "^smoke-chaos:", mk).CombinedOutput()
	if err != nil || len(out) == 0 {
		t.Fatalf("inv-zen-307: Makefile missing smoke-chaos target:\n%s", out)
	}
	// Sister-assertion: the smoke target MUST compose all 4 steps
	// (test-chaos-failpoint + test-dst-pr + Toxiproxy network + DST
	// regression replay) so a partial smoke does not pass as the
	// full aggregator. Each marker corresponds to one of the 4 echo
	// labels the target emits.
	data, err := os.ReadFile(mk)
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"smoke-chaos] step 1/4",
		"smoke-chaos] step 2/4",
		"smoke-chaos] step 3/4",
		"smoke-chaos] step 4/4",
		"smoke-chaos] ALL PASS",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-307: Makefile smoke-chaos target missing step marker %q", want)
		}
	}
}
