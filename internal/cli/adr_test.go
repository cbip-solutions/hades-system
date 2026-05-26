package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func invokeAdrCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })
	cmd := NewAdrCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockAdrServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/adr/propose", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ADR{
			ID:     "ADR-0070",
			Status: "proposed",
			Topic:  "tessera-batch-cadence-tuning",
			Plan:   "plan-9",
		})
	})

	mux.HandleFunc("/v1/adr/show", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		_ = json.NewEncoder(w).Encode(client.ADR{
			ID:        id,
			Status:    "accepted",
			Topic:     "tessera-batch-cadence-tuning",
			Plan:      "plan-9",
			RiskLevel: "low",
			Frontmatter: map[string]string{
				"id": id, "status": "accepted", "plan": "plan-9", "risk_level": "low",
				"title": "Tessera batch cadence tuning",
			},
			Body: "## Context\n\nbody...\n",
		})
	})

	mux.HandleFunc("/v1/adr/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ADR{
				{ID: "ADR-0060", Status: "accepted", Plan: "plan-9", RiskLevel: "high", Topic: "tessera-vendor-mode"},
				{ID: "ADR-0061", Status: "accepted", Plan: "plan-9", RiskLevel: "medium", Topic: "per-project-tile-log"},
			},
			"count": 2,
		})
	})

	mux.HandleFunc("/v1/adr/graph", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ADRGraph{
			Nodes: []client.ADRGraphNode{
				{ID: "ADR-0001", Status: "accepted"},
				{ID: "ADR-0010", Status: "accepted"},
				{ID: "ADR-0060", Status: "accepted"},
			},
			Edges: []client.ADREdge{
				{From: "ADR-0001", To: "ADR-0010", Type: "supersedes"},
				{From: "ADR-0010", To: "ADR-0060", Type: "supersedes"},
			},
		})
	})

	mux.HandleFunc("/v1/adr/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ADRTransition{
				{ID: "ADR-0070", Status: "proposed", At: 1761000000, Reason: "Q4 B brainstorm"},
				{ID: "ADR-0070", Status: "accepted", At: 1762000000, Reason: "Plan 9 ship"},
			},
			"count": 2,
		})
	})

	mux.HandleFunc("/v1/adr/accept", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/adr/reject", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/adr/supersede", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/adr/index", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		check, _ := req["check"].(bool)
		if check {
			_ = json.NewEncoder(w).Encode(client.ADRManifest{
				GeneratedAt: 1762000000,
				ADRCount:    39,
				Manifest:    `{"adrs":[]}`,
				Graph:       `{"nodes":[],"edges":[]}`,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(client.ADRManifest{
			GeneratedAt: 1762000000,
			ADRCount:    39,
			Manifest:    `{"adrs":[]}`,
			Graph:       `{"nodes":[],"edges":[]}`,
		})
	})

	return httptest.NewServer(mux)
}

func fakeEditor(content string) func(path string) error {
	return func(path string) error {
		return os.WriteFile(path, []byte(content), 0o600)
	}
}

func TestAdr_RegistersAllSubcommands(t *testing.T) {
	cmd := NewAdrCmd()
	want := []string{"propose", "show", "ls", "graph", "history", "accept", "reject", "supersede", "migrate", "index"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand %q", w)
		}
	}
}

func TestAdrPropose_HappyPath(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	prev := editorRunner
	editorRunner = fakeEditor(`---
id: ADR-0070
status: proposed
plan: plan-9
risk_level: low
title: Tessera batch cadence tuning
---

## Context

Q4 B operator decision per Plan 9 brainstorm.

## Decision

Doctrine-tunable BatchMaxAge.
`)
	t.Cleanup(func() { editorRunner = prev })

	stdout, _, err := invokeAdrCmd(t, []string{"propose", "tessera-batch-cadence-tuning"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "ADR-0070") {
		t.Errorf("missing ADR id: %s", stdout)
	}
}

func TestAdrPropose_EmptyBodyAborts(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	prev := editorRunner
	editorRunner = fakeEditor("")
	t.Cleanup(func() { editorRunner = prev })

	_, _, err := invokeAdrCmd(t, []string{"propose", "empty-topic"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for empty draft body")
	}
}

func TestAdrShow_TableFormat(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"show", "ADR-0070"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"ADR-0070", "accepted", "low"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}

func TestAdrLs_DefaultRender(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"ls"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"ADR-0060", "ADR-0061", "high", "medium"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}

func TestAdrLs_FilterStatus(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()

	_, _, err := invokeAdrCmd(t, []string{"ls", "--status", "accepted"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
}

func TestAdrGraph_AsciiTree(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"graph", "--from", "ADR-0001", "--depth", "5"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}

	for _, want := range []string{"ADR-0001", "ADR-0010", "ADR-0060", "─"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}

func TestAdrGraph_RequiresFrom(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"graph"}, srv.URL)
	if err == nil {
		t.Fatal("expected error when --from is missing")
	}
}

func TestAdrEditor_FallbackChain(t *testing.T) {

	origVisual := os.Getenv("VISUAL")
	origEditor := os.Getenv("EDITOR")
	defer func() {
		os.Setenv("VISUAL", origVisual)
		os.Setenv("EDITOR", origEditor)
	}()

	var captured string
	stub := func(path string) error {
		captured = path
		return nil
	}

	prev := editorRunner
	editorRunner = stub
	defer func() { editorRunner = prev }()

	os.Setenv("VISUAL", "my-visual")
	os.Setenv("EDITOR", "my-editor")
	ed1 := resolveEditorName()
	if ed1 != "my-visual" {
		t.Errorf("VISUAL should win: got %q", ed1)
	}

	os.Unsetenv("VISUAL")
	ed2 := resolveEditorName()
	if ed2 != "my-editor" {
		t.Errorf("EDITOR should win when VISUAL unset: got %q", ed2)
	}

	os.Unsetenv("EDITOR")
	ed3 := resolveEditorName()
	if ed3 != "vi" {
		t.Errorf("vi fallback expected: got %q", ed3)
	}

	_ = editorRunner("/dev/null")
	if captured != "/dev/null" {
		t.Errorf("stub not called with correct path: %q", captured)
	}
}

func TestAdrHistory_RendersTransitions(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"history", "ADR-0070"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"proposed", "accepted", "Q4 B brainstorm", "Plan 9 ship"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}

func TestAdrAccept_RequiresReason(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"accept", "ADR-0070"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required (inv-zen-146)")
	}
}

func TestAdrAccept_HappyPath(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"accept", "ADR-0070", "--reason", "Plan 9 ship approved"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "ADR-0070") {
		t.Errorf("missing id in output: %s", stdout)
	}
}

func TestAdrReject_RequiresReason(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"reject", "ADR-0070"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required")
	}
}

func TestAdrReject_HappyPath(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"reject", "ADR-0070", "--reason", "superseded by simpler design"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "ADR-0070") {
		t.Errorf("missing id in output: %s", stdout)
	}
	if !strings.Contains(stdout, "rejected") {
		t.Errorf("missing rejected status: %s", stdout)
	}
}

func TestAdrSupersede_RequiresTwoArgs(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"supersede", "ADR-0070", "--reason", "x"}, srv.URL)
	if err == nil {
		t.Fatal("expected 2 positional args")
	}
}

func TestAdrSupersede_RequiresReason(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"supersede", "ADR-0070", "ADR-0080"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required")
	}
}

func TestAdrSupersede_HappyPath(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"supersede", "ADR-0070", "ADR-0080", "--reason", "design drift"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "superseded") {
		t.Errorf("missing superseded in output: %s", stdout)
	}
}

func TestAdrLs_EmptyResult(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/adr/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.ADR{}, "count": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"ls"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "no rows") {
		t.Errorf("expected 'no rows' for empty list: %s", stdout)
	}
}

func TestAdrHistory_EmptyResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/adr/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.ADRTransition{}, "count": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"history", "ADR-0042"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "no transitions") {
		t.Errorf("expected 'no transitions' for empty history: %s", stdout)
	}
}

const legacyADRContent = `# ADR 0099: Synthetic test ADR

**Status**: Proposed
**Date**: 2026-05-01
**Decision-maker**: the operator
**Plan**: Plan 9 (Phase I testing)

## Context

This is a synthetic legacy ADR for migration testing.

## Decision

Use MADR frontmatter.

## Consequences

Structured parsing is now possible.
`

func TestAdrMigrate_DryRun(t *testing.T) {

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("setup go.mod: %v", err)
	}
	decDir := filepath.Join(tmp, "docs", "decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	adrPath := filepath.Join(decDir, "0099-synthetic-test.md")
	if err := os.WriteFile(adrPath, []byte(legacyADRContent), 0o644); err != nil {
		t.Fatalf("write ADR: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	srv := mockAdrServer(t)
	defer srv.Close()

	stdout, _, err := invokeAdrCmd(t, []string{"migrate", "--dry-run"}, srv.URL)
	if err != nil {
		t.Fatalf("migrate --dry-run failed: %v", err)
	}

	if !strings.Contains(stdout, "success") {
		t.Errorf("expected 'success' status in dry-run output: %s", stdout)
	}
	if !strings.Contains(stdout, "0099-synthetic-test.md") {
		t.Errorf("expected adr filename in output: %s", stdout)
	}

	after, _ := os.ReadFile(adrPath)
	if string(after) != legacyADRContent {
		t.Errorf("dry-run must not modify file; got:\n%s", string(after))
	}
}

func TestAdrMigrate_WritesRealFile(t *testing.T) {

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("setup go.mod: %v", err)
	}
	decDir := filepath.Join(tmp, "docs", "decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	adrPath := filepath.Join(decDir, "0099-synthetic-test.md")
	if err := os.WriteFile(adrPath, []byte(legacyADRContent), 0o644); err != nil {
		t.Fatalf("write ADR: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	srv := mockAdrServer(t)
	defer srv.Close()

	stdout, _, err := invokeAdrCmd(t, []string{"migrate", "--plan", "plan-9"}, srv.URL)
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if !strings.Contains(stdout, "success") {
		t.Errorf("expected success status: %s", stdout)
	}

	after, _ := os.ReadFile(adrPath)
	if !strings.HasPrefix(string(after), "---\n") {
		t.Errorf("expected YAML frontmatter after migration; got:\n%s", string(after))
	}
	if !strings.Contains(string(after), "ADR-0099") {
		t.Errorf("expected ADR-0099 in frontmatter: %s", string(after))
	}
}

func TestAdrMigrate_IdempotentOnAlreadyMigrated(t *testing.T) {

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("setup go.mod: %v", err)
	}
	decDir := filepath.Join(tmp, "docs", "decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	alreadyMigrated := `---
id: ADR-0099
title: Already migrated
status: proposed
date: "2026-05-01"
plan: plan-9
tags:
  - plan-9
---

## Context

Already has frontmatter.
`
	adrPath := filepath.Join(decDir, "0099-synthetic-test.md")
	if err := os.WriteFile(adrPath, []byte(alreadyMigrated), 0o644); err != nil {
		t.Fatalf("write ADR: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	srv := mockAdrServer(t)
	defer srv.Close()

	stdout, _, err := invokeAdrCmd(t, []string{"migrate"}, srv.URL)
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if !strings.Contains(stdout, "skipped") {
		t.Errorf("expected 'skipped' for already-migrated file: %s", stdout)
	}

	after, _ := os.ReadFile(adrPath)
	if string(after) != alreadyMigrated {
		t.Errorf("idempotent: file should not change; got:\n%s", string(after))
	}
}

func TestFindRepoRoot_ErrorWhenNoGoMod(t *testing.T) {

	tmp := t.TempDir()
	sub := filepath.Join(tmp, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	_, err = findRepoRoot()
	if err == nil {
		t.Fatal("expected error when no go.mod found")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("expected go.mod in error message: %v", err)
	}
}

func TestAdrIndex_Regenerate(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()
	stdout, _, err := invokeAdrCmd(t, []string{"index"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "adr_count") && !strings.Contains(stdout, "39") {
		t.Errorf("missing index output: %s", stdout)
	}
}

func TestAdrIndex_CheckNoDrift(t *testing.T) {
	srv := mockAdrServer(t)
	defer srv.Close()

	_, _, err := invokeAdrCmd(t, []string{"index", "--check"}, srv.URL)
	if err != nil {
		t.Fatalf("expected no-drift exit 0; got %v", err)
	}
}

func TestAdrIndex_CheckDriftExitsNonZero(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/adr/index", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ADRManifest{
			GeneratedAt: 1762000000,
			ADRCount:    39,
			Manifest:    "",
			Graph:       "",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeAdrCmd(t, []string{"index", "--check"}, srv.URL)
	if err == nil {
		t.Fatal("expected drift to surface as error (CI exit code 1)")
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Errorf("expected drift in error: %v", err)
	}
}
