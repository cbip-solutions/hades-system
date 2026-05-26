package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard"
)

func invokeNewCmd(t *testing.T, args []string, envOverrides map[string]string) (string, string, int) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))

	binDir := filepath.Join(tmpHome, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hermes"), []byte("#!/bin/sh\necho 'Hermes Agent 0.13.0 present at fake'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for k, v := range envOverrides {
		t.Setenv(k, v)
	}

	cmd := NewNewCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	code := exitCodeForTest(err)
	return stdout.String(), stderr.String(), code
}

func exitCodeForTest(err error) int {
	if err == nil {
		return 0
	}
	if IsPreflightFailure(err) {
		return 3
	}
	if IsRecoverable(err) {
		return 1
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 2
}

func TestNewCmd_ListTemplates_PrintsAll6(t *testing.T) {
	stdout, _, code := invokeNewCmd(t, []string{"--list-templates"}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	wantNames := []string{
		"hermes-plugin-only",
		"hermes-plugin+daemon",
		"go-cli",
		"python-cli",
		"ts-saas",
		"ml-pipeline",
	}
	for _, n := range wantNames {
		if !strings.Contains(stdout, n) {
			t.Errorf("--list-templates missing %q\nGOT:\n%s", n, stdout)
		}
	}
}

func TestNewCmd_NonInteractive_HappyPath(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	stdout, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "test-greenfield",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Errorf("expected plugin.yaml at %q: %v", dst, err)
	}
	if !strings.Contains(stdout, "Next steps") && !strings.Contains(stdout, "next steps") {
		t.Errorf("stdout missing next-steps hint:\n%s", stdout)
	}
}

func TestNewCmd_NonInteractive_MissingTemplate_Exit1(t *testing.T) {
	_, stderr, code := invokeNewCmd(t, []string{"--non-interactive"}, nil)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (recoverable)\nSTDERR:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "non-interactive") {
		t.Errorf("stderr missing 'non-interactive' hint:\n%s", stderr)
	}
}

func TestNewCmd_NonInteractive_MissingProjectName_Exit1(t *testing.T) {
	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
	}, nil)
	if code != 1 {
		t.Fatalf("exit %d, want 1\nSTDERR:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "project-name") {
		t.Errorf("stderr missing 'project-name' hint:\n%s", stderr)
	}
}

func TestNewCmd_TargetExistsNonEmpty_Exit1(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "preexisting.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 1 {
		t.Errorf("exit %d, want 1 (target exists)\nSTDERR:\n%s", code, stderr)
	}
}

func TestNewCmd_ForceOverridesConflict(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "preexisting.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--force",
		"--yes",
	}, nil)
	if code != 0 {
		t.Errorf("exit %d, want 0 (force should override)\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
}

func TestNewCmd_CCDetected_SurfacesMigrateHint(t *testing.T) {
	tmpHome := t.TempDir()
	ccDir := filepath.Join(tmpHome, ".claude")
	if err := os.MkdirAll(filepath.Join(ccDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	stdout, _, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--yes",
	}, map[string]string{"HOME": tmpHome})
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(stdout, "~/.claude/") {
		t.Errorf("stdout missing CC-detected mention:\n%s", stdout)
	}
	if !strings.Contains(stdout, "zen migrate") {
		t.Errorf("stdout missing migrate suggestion:\n%s", stdout)
	}
}

func TestNewCmd_ExistingGitRepo_SkipsGitInit(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(parent, "child")
	stdout, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "child",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Errorf("child %q should not have own .git (parent is repo)", dst)
	}
}

func TestNewCmd_PreflightHermesMissing_Exit3(t *testing.T) {

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))
	emptyBin := filepath.Join(tmpHome, "empty-bin")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyBin)

	cmd := NewNewCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--non-interactive", "--template", "hermes-plugin-only", "--project-name", "x"})
	err := cmd.ExecuteContext(context.Background())
	code := exitCodeForTest(err)
	if code != 3 {
		t.Errorf("exit %d, want 3 (preflight)\nSTDERR:\n%s", code, stderr.String())
	}
}

func TestNewCmd_HelpListsFlags(t *testing.T) {
	stdout, _, code := invokeNewCmd(t, []string{"--help"}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	wantFlags := []string{
		"--template",
		"--template-version",
		"--path",
		"--non-interactive",
		"--yes",
		"--reset-preferences",
		"--list-templates",
		"--force",
		"--force-git",
		"--project-name",
	}
	for _, f := range wantFlags {
		if !strings.Contains(stdout, f) {
			t.Errorf("--help missing flag %q", f)
		}
	}
}

func TestNewCmd_TargetExistsEmpty_Succeeds(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "empty-target")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 0 {
		t.Errorf("exit %d, want 0 (empty target should succeed)", code)
	}
}

func TestNewCmd_InferKindFromTemplate(t *testing.T) {
	cases := map[string]string{
		"hermes-plugin-only":   "plugin",
		"hermes-plugin+daemon": "plugin",
		"go-cli":               "cli-go",
		"python-cli":           "cli-python",
		"ts-saas":              "saas",
		"ml-pipeline":          "ml-pipeline",
		"unknown":              "plugin",
		"":                     "plugin",
	}
	for input, want := range cases {
		if got := inferKindFromTemplate(input); got != want {
			t.Errorf("inferKindFromTemplate(%q) = %q want %q", input, got, want)
		}
	}
}

func TestComputeInitGit_NestedRepoFalse(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "child")
	if computeInitGit(target, false) {
		t.Error("computeInitGit: want false inside existing repo")
	}
	if !computeInitGit(target, true) {
		t.Error("computeInitGit(--force-git): want true")
	}
}

func TestComputeInitGit_NoRepoTrue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "child")
	if !computeInitGit(target, false) {
		t.Error("computeInitGit outside repo: want true")
	}
}

func TestNewCmd_UnknownTemplate_Exit1(t *testing.T) {
	_, _, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "totally-not-a-real-template",
		"--project-name", "x",
		"--yes",
	}, nil)
	if code != 1 {
		t.Errorf("exit %d, want 1 for unknown template", code)
	}
}

func TestNewCmd_NoPathFallsBackToProjectName(t *testing.T) {

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	_, _, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "fallback-project",
		"--yes",
	}, nil)
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	expected := filepath.Join(tmpDir, "fallback-project")
	if _, err := os.Stat(filepath.Join(expected, "plugin.yaml")); err != nil {
		t.Errorf("scaffold landed at unexpected location: %v", err)
	}
}

func TestResolveTemplate_EmbeddedHit(t *testing.T) {
	tmpl, err := resolveTemplate(context.Background(), "hermes-plugin-only", "")
	if err != nil {
		t.Fatalf("resolveTemplate(embedded): %v", err)
	}
	if tmpl.Name() != "hermes-plugin-only" {
		t.Errorf("name = %q", tmpl.Name())
	}
}

func TestResolveTemplate_EmptyName(t *testing.T) {
	_, err := resolveTemplate(context.Background(), "", "")
	if err == nil {
		t.Error("resolveTemplate(empty): want error")
	}
}

func TestResolveTemplate_MalformedPluggableURL(t *testing.T) {
	_, err := resolveTemplate(context.Background(), "not-a-real-template", "")
	if err == nil {
		t.Error("resolveTemplate(non-embedded non-URL): want error")
	}
}

func TestResolveTemplate_ValidPluggableURLAttemptsFetch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, "cache"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolveTemplate(ctx, "gh:foo/bar", "")
	if err == nil {
		t.Error("resolveTemplate(gh: cancelled ctx): want error")
	}
}

func TestValidateNewHermesScope_AcceptsCanonical(t *testing.T) {
	cases := map[string]string{
		"":        "user",
		"user":    "user",
		"project": "project",
	}
	for input, want := range cases {
		got, err := validateNewHermesScope(input)
		if err != nil {
			t.Errorf("validateNewHermesScope(%q): want nil err, got %v", input, err)
		}
		if got != want {
			t.Errorf("validateNewHermesScope(%q) = %q want %q", input, got, want)
		}
	}
}

func TestValidateNewHermesScope_RejectsBadEnum(t *testing.T) {
	cases := []string{"USER", "Project", "global", "system", "garbage"}
	for _, c := range cases {
		_, err := validateNewHermesScope(c)
		if err == nil {
			t.Errorf("validateNewHermesScope(%q): want error, got nil", c)
		}
	}
}

func TestNewCmd_HermesScope_DefaultUser(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	stdout, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "scope-default",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(dst, "plugin.yaml")); err != nil {
		t.Errorf("expected plugin.yaml: %v", err)
	}
}

func TestNewCmd_HermesScope_ProjectFlag(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "scope-proj")
	stdout, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "scope-project",
		"--path", dst,
		"--hermes-scope", "project",
		"--yes",
	}, nil)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
}

func TestNewCmd_HermesScope_BadEnum_Exit1(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "scope-bad")
	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "scope-bad",
		"--path", dst,
		"--hermes-scope", "USER",
		"--yes",
	}, nil)
	if code != 1 {
		t.Errorf("exit %d, want 1 (bad --hermes-scope)\nSTDERR:\n%s", code, stderr)
	}
}

func TestAsTemplateAnswers_ScopeOverridePrecedence(t *testing.T) {
	a := onboard.WizardAnswers{ProjectName: "x", HermesPluginScope: "user"}
	got := asTemplateAnswers(a, "/abs", "project")
	if got.HermesPluginScope != "project" {
		t.Errorf("scopeOverride beats wizard answer; got %q want %q", got.HermesPluginScope, "project")
	}
}

func TestAsTemplateAnswers_ScopeFromWizardWhenOverrideEmpty(t *testing.T) {
	a := onboard.WizardAnswers{ProjectName: "x", HermesPluginScope: "project"}
	got := asTemplateAnswers(a, "/abs", "")
	if got.HermesPluginScope != "project" {
		t.Errorf("wizard answer honoured; got %q want %q", got.HermesPluginScope, "project")
	}
}

func TestAsTemplateAnswers_ScopeDefaultsToUser(t *testing.T) {
	a := onboard.WizardAnswers{ProjectName: "x"}
	got := asTemplateAnswers(a, "/abs", "")
	if got.HermesPluginScope != "user" {
		t.Errorf("default scope; got %q want %q", got.HermesPluginScope, "user")
	}
}

func TestNewCmd_BashMissing_Exit3(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))
	binDir := filepath.Join(tmpHome, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hermes"), []byte("#!/bin/sh\necho 'Hermes Agent 0.13.0 present'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	cmd := NewNewCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--non-interactive", "--template", "hermes-plugin-only", "--project-name", "x", "--yes"})
	err := cmd.ExecuteContext(context.Background())
	code := exitCodeForTest(err)
	if code != 3 {
		t.Errorf("exit %d, want 3 (preflight: bash missing)\nSTDERR:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "bash") {
		t.Errorf("stderr should mention bash:\n%s", stderr.String())
	}
}

func TestRunNew_ForceRemoveFailure_SurfacesError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "force-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "x.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", target,
		"--force",
		"--yes",
	}, nil)
	if code == 0 {
		t.Errorf("exit 0, want non-zero (--force wipe should fail)\nSTDERR:\n%s", stderr)
	}
}

func TestNewCmd_RemoveEmptyTargetFailure_SurfacesError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can ignore directory permissions; skip on root")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "empty-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "x",
		"--path", target,
		"--yes",
	}, nil)

	if code == 0 {
		t.Fatalf("exit %d, want non-zero (parent read-only blocks remove)\nSTDERR:\n%s", code, stderr)
	}

	if !strings.Contains(stderr, "remove") && !strings.Contains(stderr, "permission") && !strings.Contains(stderr, "target") {
		t.Errorf("stderr lacks remove/permission/target context:\n%s", stderr)
	}
}

func TestNewCmd_HookFailureMapsRecoverable(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out-bad-name")

	_, stderr, code := invokeNewCmd(t, []string{
		"--non-interactive",
		"--template", "hermes-plugin-only",
		"--project-name", "BAD-UPPERCASE",
		"--path", dst,
		"--yes",
	}, nil)
	if code != 1 {
		t.Errorf("exit %d, want 1 (recoverable on hook validator fail)\nSTDERR:\n%s", code, stderr)
	}
}

func TestDirState_VariousStates(t *testing.T) {
	root := t.TempDir()

	exists, nonEmpty, err := dirState(filepath.Join(root, "missing"))
	if err != nil || exists || nonEmpty {
		t.Errorf("missing: exists=%v nonEmpty=%v err=%v", exists, nonEmpty, err)
	}

	emptyDir := filepath.Join(root, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exists, nonEmpty, err = dirState(emptyDir)
	if err != nil || !exists || nonEmpty {
		t.Errorf("empty: exists=%v nonEmpty=%v err=%v", exists, nonEmpty, err)
	}

	nonEmptyDir := filepath.Join(root, "non-empty")
	if err := os.MkdirAll(nonEmptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptyDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists, nonEmpty, err = dirState(nonEmptyDir)
	if err != nil || !exists || !nonEmpty {
		t.Errorf("non-empty: exists=%v nonEmpty=%v err=%v", exists, nonEmpty, err)
	}

	filePath := filepath.Join(root, "regular-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists, nonEmpty, err = dirState(filePath)
	if err != nil || !exists || !nonEmpty {
		t.Errorf("regular file: exists=%v nonEmpty=%v err=%v", exists, nonEmpty, err)
	}
}
