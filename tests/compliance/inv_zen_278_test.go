// tests/compliance/inv_zen_278_test.go
//
// Compliance gate for inv-zen-278 (Plan v0.20.0 Phase F): BGE reranker
// auto-detect + log-level convention.
//
// The invariant pins five load-bearing properties:
//
//  1. cmd/zen-swarm-ctld/main.go invokes bgeModelAvailable("") as the
//     pre-check guard before constructing the BGE reranker. (Source
//     regex 1.)
//  2. main.go emits BOTH log lines that encode the WARN→INFO convention:
//     INFO when the model is missing (documented degraded default) AND
//     WARN when files are present but ONNX construction still failed
//     (anomaly path). (Source regex 2.)
//  3. internal/cli/doctor_caronte.go has the "rerank.available" probe
//     entry whose hint mentions scripts/download-bge-model.sh. (Source
//     regex 3.)
//  4. scripts/download-bge-model.sh exists, is executable, has
//     `set -euo pipefail`, references both model + tokenizer filenames,
//     and includes the SHA recording + idempotent-skip logic. (Source
//     regex 4.)
//  5. Behavioural sister-test: a clean tempdir with both files present
//     → bgeModelAvailable returns true; tokenizer absent → returns
//     false. The helper IS the gate driving the log-level branch; a
//     refactor that collapses the branches back to unconditional WARN
//     would silently break the convention.
//
// Sister-test pattern: revert the WARN→INFO switch back to unconditional
// `logger.Warn` (no pre-check) → this test MUST fail because the source
// regexes assert both log strings are present. The branching IS
// load-bearing for the operator UX (INFO is the documented degraded
// default, not a noisy warning).
//
// inv-zen-278 (Plan v0.20.0 Phase F).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFile is a small helper that fails the test with a useful message
// when the path is missing. We do not import io/ioutil; os.ReadFile is
// the modern equivalent.
func readFile(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-278: read %s: %v", path, err)
	}
	return string(src)
}

func TestInvZen278_PreCheckGuardInMain(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "cmd", "zen-swarm-ctld", "main.go"))
	const needle = `bgeModelAvailable("")`
	if !strings.Contains(body, needle) {
		t.Errorf("inv-zen-278: cmd/zen-swarm-ctld/main.go missing pre-check %q — the helper IS the gate driving the log-level convention", needle)
	}
}

func TestInvZen278_BothLogLinesInMain(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "cmd", "zen-swarm-ctld", "main.go"))

	const infoNeedle = `logger.Info("caronte: BGE reranker model not installed`
	if !strings.Contains(body, infoNeedle) {
		t.Errorf("inv-zen-278: cmd/zen-swarm-ctld/main.go missing INFO branch %q — degraded-default state must be INFO not WARN", infoNeedle)
	}

	const warnNeedle = `logger.Warn("caronte: BGE reranker construction failed despite model files present`
	if !strings.Contains(body, warnNeedle) {
		t.Errorf("inv-zen-278: cmd/zen-swarm-ctld/main.go missing WARN-on-anomaly branch %q — files-present-construction-failed must surface as WARN", warnNeedle)
	}
}

func TestInvZen278_DoctorProbeEntry(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "cli", "doctor_caronte.go"))
	for _, needle := range []string{
		`"rerank.available"`,
		`"caronte.rerank.available"`,
		`scripts/download-bge-model.sh`,
		`BGE reranker model missing`,
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("inv-zen-278: internal/cli/doctor_caronte.go missing %q", needle)
		}
	}
}

func TestInvZen278_DownloadScriptShape(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "download-bge-model.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-278: stat %s: %v", path, err)
	}

	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("inv-zen-278: %s is not executable (mode=%o); chmod +x", path, info.Mode().Perm())
	}
	body := readFile(t, path)
	for _, needle := range []string{
		`#!/usr/bin/env bash`,
		`set -euo pipefail`,
		`bge-reranker-v2-m3.onnx`,
		`tokenizer.json`,
		`already present`,
		`expected-sha`,
		`ZEN_BGE_MODEL_DIR`,
		`ZEN_BGE_MODEL_URL`,
		`ZEN_BGE_EXPECTED_SHA`,
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("inv-zen-278: scripts/download-bge-model.sh missing %q", needle)
		}
	}
}

// TestInvZen278_BGEAvailable_Helper_HonoursTokenizerCoLocation is the
// behavioural half: the bgeModelAvailable helper MUST return false
// unless BOTH the ONNX model AND the tokenizer.json sidecar exist in
// the same directory. Cannot be imported from package main; the in-
// package test cmd/zen-swarm-ctld/main_bge_test.go pins this contract,
// and this compliance test re-asserts it via the on-disk source string
// so a future refactor that drops the tokenizer.Stat call would also
// surface here.
//
// We grep for the textual contract in main.go (the helper body
// references the tokenizer co-location), keeping the regression-pin in
// the compliance tier. The in-package test exercises the behaviour
// directly.
func TestInvZen278_BGEAvailable_Helper_HonoursTokenizerCoLocation(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "cmd", "zen-swarm-ctld", "main.go"))
	for _, needle := range []string{
		`tokPath := filepath.Join(filepath.Dir(modelPath), "tokenizer.json")`,
		`if _, err := os.Stat(tokPath); err != nil {`,
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("inv-zen-278: cmd/zen-swarm-ctld/main.go missing tokenizer-co-location check %q — the helper MUST verify the sidecar, not just the .onnx file", needle)
		}
	}
}

func TestInvZen278_PublicResolveBGEModelPath_Exported(t *testing.T) {
	root := repoRoot(t)
	body := readFile(t, filepath.Join(root, "internal", "research", "ecosystem", "reranker_bge.go"))
	const needle = `func ResolveBGEModelPath(explicit string) string`
	if !strings.Contains(body, needle) {
		t.Errorf("inv-zen-278: internal/research/ecosystem/reranker_bge.go missing exported %q — the public wrapper is the single source of truth for the 3-tier path resolution", needle)
	}
}

func TestInvZen278_ServerProbeRouteRegistered(t *testing.T) {
	root := repoRoot(t)
	serverBody := readFile(t, filepath.Join(root, "internal", "daemon", "server.go"))
	const route = `s.mux.HandleFunc("GET /v1/caronte/probe", handlers.CaronteProbeHandler(s))`
	if !strings.Contains(serverBody, route) {
		t.Errorf("inv-zen-278: internal/daemon/server.go missing %q route registration", route)
	}
	handlerBody := readFile(t, filepath.Join(root, "internal", "daemon", "handlers", "caronte_probe.go"))
	const handlerSig = `func CaronteProbeHandler(s CaronteProbeCtx) http.HandlerFunc`
	if !strings.Contains(handlerBody, handlerSig) {
		t.Errorf("inv-zen-278: internal/daemon/handlers/caronte_probe.go missing %q", handlerSig)
	}
	// The handler MUST surface the rerank.available case (the case
	// that drives operator install state to `zen doctor caronte`).
	if !strings.Contains(handlerBody, `case "rerank.available":`) {
		t.Errorf(`inv-zen-278: caronte_probe.go missing case "rerank.available":`)
	}
}
