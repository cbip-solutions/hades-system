package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func TestZenMigrateClaudeCode_DryRunEmitsPlan(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"claude-code", "--source", tmp, "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	dec := json.NewDecoder(&out)
	var plan struct {
		SchemaVersion string `json:"schemaVersion"`
		Entries       []struct {
			Kind string `json:"kind"`
		} `json:"entries"`
	}
	if err := dec.Decode(&plan); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if plan.SchemaVersion != "1.0" {
		t.Errorf("schemaVersion: %s", plan.SchemaVersion)
	}
	hasSkill := false
	for _, e := range plan.Entries {
		if e.Kind == "skill" {
			hasSkill = true
		}
	}
	if !hasSkill {
		t.Errorf("no skill entry in plan")
	}
}

func TestZenMigrateClaudeCode_InvalidPresetRejected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cmd := NewMigrateCmd()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"claude-code", "--source", tmp, "--preset", "garbage"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "preset") {
		t.Errorf("err message should mention preset: %v", err)
	}
}

func TestZenMigrateClaudeCode_DryRunHumanReadable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", tmp, "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "Dry-run plan") {
		t.Errorf("missing dry-run header: %s", s)
	}
	if !strings.Contains(s, "[skill]") {
		t.Errorf("missing skill entry: %s", s)
	}
}

func TestZenMigrateClaudeCode_PlanOutputWritesFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", tmp, "--plan-output", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	body, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if !strings.Contains(string(body), `"schemaVersion": "1.0"`) {
		t.Errorf("plan missing schemaVersion: %s", body)
	}

	info, _ := os.Stat(planPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("plan mode: got %o, want 0600", info.Mode().Perm())
	}
}

func TestZenMigrateClaudeCode_FullApplyAndVerify(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	pluginRoot := filepath.Join(target, "plugin", "zen-swarm")
	hermesCfg := filepath.Join(target, "hermes", "config.yaml")
	zenCfg := filepath.Join(target, "zen-config")

	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"claude-code",
		"--source", src,
		"--target-hermes", pluginRoot,
		"--target-config", hermesCfg,
		"--target-zen-config", zenCfg,
		"--force",
		"--verify",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(pluginRoot, "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("skill not written: %v", err)
	}
	if !strings.Contains(out.String(), "Migration applied") {
		t.Errorf("missing success message: %s", out.String())
	}
	if !strings.Contains(out.String(), "Verify") {
		t.Errorf("missing verify hint: %s", out.String())
	}

	if !strings.Contains(out.String(), "deferred to Phase F") {
		t.Errorf("verify message should announce deferral, not fake OK: %s", out.String())
	}
}

func TestZenMigrateClaudeCode_PreviewPresetAliasesDryRun(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--preset", "preview"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Dry-run plan") {
		t.Errorf("preview preset should trigger dry-run: %s", out.String())
	}
}

func TestZenMigrateClaudeCode_IncludeFiltersSurfaces(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "commands", "hello.md"), []byte("# /hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--dry-run", "--include", "skills"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "[skill]") {
		t.Errorf("expected skill entry: %s", s)
	}
	if strings.Contains(s, "[command]") {
		t.Errorf("commands should be filtered out: %s", s)
	}
}

func TestZenMigrateClaudeCode_ExcludeFiltersSurfaces(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "commands", "hello.md"), []byte("# /hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--dry-run", "--exclude", "commands"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "[skill]") {
		t.Errorf("expected skill entry: %s", s)
	}
	if strings.Contains(s, "[command]") {
		t.Errorf("commands should be excluded: %s", s)
	}
}

func TestZenMigrateClaudeCode_ApplyPlanRoundtrip(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--plan-output", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan-output: %v\n%s", err, out.String())
	}

	target := t.TempDir()
	pluginRoot := filepath.Join(target, "plugin", "zen-swarm")
	cmd2 := NewMigrateCmd()
	out2 := bytes.Buffer{}
	cmd2.SetOut(&out2)
	cmd2.SetErr(&out2)
	cmd2.SetArgs([]string{
		"claude-code",
		"--apply-plan", planPath,
		"--target-hermes", pluginRoot,
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force",
	})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("apply-plan: %v\n%s", err, out2.String())
	}
	if _, err := os.Stat(filepath.Join(pluginRoot, "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("skill not written via apply-plan: %v", err)
	}
}

func TestZenMigrateClaudeCode_MissingSourceErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cmd := NewMigrateCmd()
	buf := bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"claude-code", "--source", filepath.Join(tmp, "does-not-exist")})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing source")
	}
}

func TestZenMigrateClaudeCode_ApplyPlanTOCTOUDetectsTamper(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha original"), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--plan-output", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan-output: %v\n%s", err, out.String())
	}

	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"),
		[]byte("# alpha TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	pluginRoot := filepath.Join(target, "plugin", "zen-swarm")
	cmd2 := NewMigrateCmd()
	out2 := bytes.Buffer{}
	cmd2.SetOut(&out2)
	cmd2.SetErr(&out2)
	cmd2.SetArgs([]string{
		"claude-code",
		"--apply-plan", planPath,
		"--target-hermes", pluginRoot,
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force",
	})
	err := cmd2.Execute()
	if err == nil {
		t.Fatalf("expected TOCTOU error, got success:\n%s", out2.String())
	}
	if !errors.Is(err, ErrPlanHashMismatch) {
		t.Errorf("err: got %v, want ErrPlanHashMismatch", err)
	}
	// Target file MUST NOT have been written with the tampered content.
	skillPath := filepath.Join(pluginRoot, "skills", "alpha", "SKILL.md")
	if _, statErr := os.Stat(skillPath); statErr == nil {

		body, _ := os.ReadFile(skillPath)
		if strings.Contains(string(body), "TAMPERED") {
			t.Errorf("apply wrote tampered content despite hash mismatch:\n%s", body)
		}
	}
}

func TestZenMigrateClaudeCode_HashesPopulatedInPlan(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "alpha", "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := NewMigrateCmd()
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claude-code", "--source", src, "--plan-output", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan-output: %v\n%s", err, out.String())
	}
	body, _ := os.ReadFile(planPath)
	var plan mapping.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if plan.MerkleRoot == "" {
		t.Errorf("plan missing merkleRoot:\n%s", body)
	}
	for _, e := range plan.Entries {
		if e.Kind == mapping.EntryKindMCPServer {
			continue
		}
		if e.SHA256 == "" {
			t.Errorf("entry %s/%s missing sha256", e.Kind, e.SourcePath)
		}
	}
}

var _ = source.ReadAll

func TestParseCSV(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		got := parseCSV(c.in)
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("parseCSV(%q): missing %q", c.in, w)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("parseCSV(%q): cardinality %d, want %d", c.in, len(got), len(c.want))
		}
	}
}
