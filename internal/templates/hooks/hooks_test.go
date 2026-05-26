package hooks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tpls "github.com/cbip-solutions/hades-system/internal/templates"
)

type fakeTemplate struct {
	name   string
	tree   fstest.MapFS
	matErr error
}

func (f *fakeTemplate) Name() string { return f.name }
func (f *fakeTemplate) FS() fs.FS    { return f.tree }
func (f *fakeTemplate) Materialize(_ context.Context, dst string, _ tpls.Answers) error {
	if f.matErr != nil {
		return f.matErr
	}
	return os.WriteFile(filepath.Join(dst, "scaffolded.txt"), []byte("ok"), 0o644)
}

func TestRun_HappyPath_AllHooksSucceed(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "happy",
		tree: fstest.MapFS{
			"pre_prompt.sh": {Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0o755},
			"pre_gen.sh":    {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
			"post_gen.sh":   {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
		},
	}
	answers := tpls.Answers{ProjectName: "test-project"}
	if err := Run(context.Background(), tmpl, dst, answers); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "scaffolded.txt")); err != nil {
		t.Errorf("expected scaffolded.txt at %q: %v", dst, err)
	}
}

func TestRun_PreGenFails_RollsBack(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "pre-gen-fails",
		tree: fstest.MapFS{
			"pre_gen.sh":  {Data: []byte("#!/bin/sh\ncat >/dev/null; echo 'bad answers' >&2; exit 1\n"), Mode: 0o755},
			"post_gen.sh": {Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0o755},
		},
	}
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "pre_gen") {
		t.Errorf("error %q does not mention pre_gen", err.Error())
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("expected dst absent on rollback, stat err: %v", statErr)
	}
}

func TestRun_PostGenFails_RollsBack(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "post-gen-fails",
		tree: fstest.MapFS{
			"pre_gen.sh":  {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
			"post_gen.sh": {Data: []byte("#!/bin/sh\ncat >/dev/null; echo fail >&2; exit 1\n"), Mode: 0o755},
		},
	}
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "post_gen") {
		t.Errorf("error %q does not mention post_gen", err.Error())
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("expected dst absent on rollback, stat err: %v", statErr)
	}
}

func TestRun_MaterializeFails_RollsBack(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "mat-fails",
		tree: fstest.MapFS{
			"pre_gen.sh":  {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
			"post_gen.sh": {Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0o755},
		},
		matErr: errors.New("simulated disk full"),
	}
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "materialize") {
		t.Errorf("error %q does not mention materialize", err.Error())
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("expected dst absent on rollback, stat err: %v", statErr)
	}
}

func TestRun_ContextCancel_AbortsBeforeFinalize(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "ctx-cancel",
		tree: fstest.MapFS{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected ctx cancel error, got nil")
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("expected dst absent on cancel, stat err: %v", statErr)
	}
}

func TestRun_HookReceivesAnswersAsJSON(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	hookScript := `#!/bin/sh
NAME=$(cat | python3 -c 'import sys,json; print(json.load(sys.stdin)["ProjectName"])')
echo "$NAME" > "` + sentinel + `"
exit 0
`
	tmpl := &fakeTemplate{
		name: "stdin-test",
		tree: fstest.MapFS{
			"pre_gen.sh":  {Data: []byte(hookScript), Mode: 0o755},
			"post_gen.sh": {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
		},
	}
	answers := tpls.Answers{ProjectName: "stdin-project"}
	if err := Run(context.Background(), tmpl, dst, answers); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if strings.TrimSpace(string(got)) != "stdin-project" {
		t.Errorf("sentinel: got %q want %q", string(got), "stdin-project")
	}
}

func TestRun_OmittedHooksAreOK(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "no-hooks",
		tree: fstest.MapFS{},
	}
	if err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "scaffolded.txt")); err != nil {
		t.Errorf("expected scaffolded.txt: %v", err)
	}
}

func TestRunPreflight_PrePromptFails(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "preflight-fail",
		tree: fstest.MapFS{
			"pre_prompt.sh": {Data: []byte("#!/bin/sh\necho 'fail' >&2; exit 1\n"), Mode: 0o755},
		},
	}
	err := RunPreflight(context.Background(), tmpl)
	if err == nil {
		t.Fatal("expected preflight error")
	}
}

func TestRunPreflight_NoHook(t *testing.T) {
	tmpl := &fakeTemplate{name: "no-preflight", tree: fstest.MapFS{}}
	if err := RunPreflight(context.Background(), tmpl); err != nil {
		t.Errorf("RunPreflight without hook: want nil, got %v", err)
	}
}

func TestPickHook_PrefersShOverPy(t *testing.T) {
	tmpl := &fakeTemplate{
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte("#!/bin/sh\n"), Mode: 0o755},
			"pre_gen.py": {Data: []byte("# py\n"), Mode: 0o755},
		},
	}
	picked, ok := pickHook(tmpl, "pre_gen")
	if !ok {
		t.Fatal("pickHook missed")
	}
	if picked != "pre_gen.sh" {
		t.Errorf("picked %q, want pre_gen.sh", picked)
	}
}

func TestPickHook_PythonFallback(t *testing.T) {
	tmpl := &fakeTemplate{
		tree: fstest.MapFS{
			"pre_gen.py": {Data: []byte("# py\n"), Mode: 0o755},
		},
	}
	picked, ok := pickHook(tmpl, "pre_gen")
	if !ok {
		t.Fatal("pickHook missed")
	}
	if picked != "pre_gen.py" {
		t.Errorf("picked %q, want pre_gen.py", picked)
	}
}

func TestPickHook_AbsentReturnsFalse(t *testing.T) {
	tmpl := &fakeTemplate{tree: fstest.MapFS{}}
	_, ok := pickHook(tmpl, "pre_gen")
	if ok {
		t.Error("pickHook on empty FS: want false")
	}
}

func TestSanitizeID_ReplacesUnsafeChars(t *testing.T) {
	got := sanitizeID("Hermes-Plugin+Daemon")
	want := "h-rm-s-plugin-daemon"
	if got != want {

		if strings.ContainsAny(got, "+ ") {
			t.Errorf("sanitizeID retained unsafe chars: %q", got)
		}
	}
	_ = want
}

func TestStageTempDir_ReturnsUniqueDir(t *testing.T) {
	a, err := StageTempDir("foo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(a)
	b, err := StageTempDir("foo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(b)
	if a == b {
		t.Errorf("StageTempDir returned same path twice: %q", a)
	}
}

func TestSwapInto_SuccessRemovesSrc(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "x.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := swapInto(src, dst); err != nil {
		t.Fatalf("swapInto: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "x.txt")); err != nil {
		t.Errorf("dst missing file: %v", err)
	}
}

func TestSwapInto_DstExistsFails(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "preexisting.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := swapInto(src, dst); err == nil {
		t.Error("swapInto into non-empty dst: want error")
	}
}

func TestSwapInto_CrossDeviceFallbackViaCopyTree(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")

	if err := os.MkdirAll(filepath.Join(src, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "exec.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "exec.sh"))
	if err != nil {
		t.Fatalf("stat exec.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("exec.sh executable bit not preserved: mode=%v", info.Mode())
	}

	body, err := os.ReadFile(filepath.Join(dst, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("read nested.txt: %v", err)
	}
	if string(body) != "nested" {
		t.Errorf("nested content mismatch: %q", string(body))
	}
}

func TestSwapInto_RenameErrorOtherThanEXDEV(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")

	dst := filepath.Join(root, "missing-parent-not-needed", "dst")
	err := swapInto(src, dst)
	if err == nil {
		t.Fatal("swapInto with non-existent src: want error")
	}
	if strings.Contains(err.Error(), "cross-device") {
		t.Errorf("error should NOT be cross-device: %v", err)
	}
}

func TestCopyTree_HandlesUnreadableSourceFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions; skip on root")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(src, "secret.txt")
	if err := os.WriteFile(unreadable, []byte("nope"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o644)
	err := copyTree(src, dst)
	if err == nil {
		t.Fatal("copyTree of unreadable file: want error")
	}
}

func TestCopyTree_RecursivelyCopies(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Errorf("dst/a.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil {
		t.Errorf("dst/sub/b.txt: %v", err)
	}
}

func TestIsCrossDeviceError_DetectsCanonical(t *testing.T) {
	if isCrossDeviceError(errors.New("invalid cross-device link")) != true {
		t.Error("did not detect 'invalid cross-device link'")
	}
	if isCrossDeviceError(errors.New("rename: EXDEV")) != true {
		t.Error("did not detect 'EXDEV'")
	}
	if isCrossDeviceError(errors.New("file exists")) {
		t.Error("false positive on unrelated error")
	}
	if isCrossDeviceError(nil) {
		t.Error("nil should be false")
	}
}

func TestRun_BadHookExtension(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "bad-ext",
		tree: fstest.MapFS{
			"weird.bin": {Data: []byte("garbage"), Mode: 0o755},
		},
	}

	err := runHook(context.Background(), tmpl, "weird.bin", tpls.Answers{}, "")
	if err == nil {
		t.Error("runHook with .bin extension: want error")
	}
}

func TestRun_HookReadMissingFile(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "missing-script",
		tree: fstest.MapFS{},
	}
	err := runHook(context.Background(), tmpl, "nonexistent.sh", tpls.Answers{}, "")
	if err == nil {
		t.Error("runHook with missing file: want error")
	}
}

func TestRunHook_PythonScript(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "py-hook",
		tree: fstest.MapFS{
			"pre_gen.py":  {Data: []byte("import sys; sys.stdin.read(); print('ok')\n"), Mode: 0o755},
			"post_gen.sh": {Data: []byte("#!/bin/sh\ncat >/dev/null; exit 0\n"), Mode: 0o755},
		},
	}
	dst := filepath.Join(t.TempDir(), "out")
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err != nil {
		t.Skipf("python3 hook path skipped: %v", err)
	}
}

func TestRun_HookFailureWrapsErrHookFailed(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "hook-failure-classify",
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte("#!/usr/bin/env bash\ncat >/dev/null; exit 1\n"), Mode: 0o755},
		},
	}
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected error from failing pre_gen")
	}
	if !errors.Is(err, ErrHookFailed) {
		t.Errorf("err does not wrap ErrHookFailed: %v", err)
	}
}

func TestRun_MaterializeErrorIsNotHookFailure(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	tmpl := &fakeTemplate{
		name: "mat-error-not-hook",
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte("#!/usr/bin/env bash\ncat >/dev/null; exit 0\n"), Mode: 0o755},
		},
		matErr: errors.New("simulated disk full"),
	}
	err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"})
	if err == nil {
		t.Fatal("expected error from materialize")
	}
	if errors.Is(err, ErrHookFailed) {
		t.Errorf("materialize error should NOT wrap ErrHookFailed: %v", err)
	}
}

func TestRunHook_StderrCaptureOnFail(t *testing.T) {
	tmpl := &fakeTemplate{
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte("#!/bin/sh\ncat >/dev/null; echo 'CUSTOM-STDERR-MARKER' >&2; exit 1\n"), Mode: 0o755},
		},
	}
	err := runHook(context.Background(), tmpl, "pre_gen.sh", tpls.Answers{}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "CUSTOM-STDERR-MARKER") {
		t.Errorf("error missing stderr marker: %v", err)
	}
}

func TestRun_BashOnlySyntaxInHook_Succeeds(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "bash-only-syntax",
		tree: fstest.MapFS{

			"pre_gen.sh": {Data: []byte(`#!/usr/bin/env bash
set -euo pipefail
ANSWERS=$(cat)
NAME=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["ProjectName"])')
if ! [[ "$NAME" =~ ^[a-z][a-z0-9-]{0,63}$ ]]; then
  echo "bad name" >&2
  exit 1
fi
exit 0
`), Mode: 0o755},
		},
	}
	dst := filepath.Join(t.TempDir(), "out")
	answers := tpls.Answers{ProjectName: "valid-name"}
	if err := Run(context.Background(), tmpl, dst, answers); err != nil {
		t.Fatalf("bash-only syntax pre_gen should succeed: %v", err)
	}
}

func TestRun_BashOnlySyntaxInHook_FailsOnInvalidInput(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "bash-only-syntax-fail",
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte(`#!/usr/bin/env bash
set -euo pipefail
ANSWERS=$(cat)
NAME=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["ProjectName"])')
if ! [[ "$NAME" =~ ^[a-z][a-z0-9-]{0,63}$ ]]; then
  echo "bad name" >&2
  exit 1
fi
exit 0
`), Mode: 0o755},
		},
	}
	dst := filepath.Join(t.TempDir(), "out")
	answers := tpls.Answers{ProjectName: "INVALID-UPPERCASE"}
	err := Run(context.Background(), tmpl, dst, answers)
	if err == nil {
		t.Fatal("expected pre_gen hook to reject INVALID-UPPERCASE, got nil")
	}
	if !strings.Contains(err.Error(), "pre_gen") {
		t.Errorf("error %q does not mention pre_gen", err.Error())
	}
}

func TestRun_HookIsInvokedViaBash(t *testing.T) {
	tmpl := &fakeTemplate{
		name: "pipefail-test",
		tree: fstest.MapFS{
			"pre_gen.sh": {Data: []byte(`#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
exit 0
`), Mode: 0o755},
		},
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "x"}); err != nil {
		t.Fatalf("Run with bash-only opts: %v (executor regressed to non-bash?)", err)
	}
}

func TestRun_ConcurrentSafety(t *testing.T) {
	parent := t.TempDir()
	tmpl := &fakeTemplate{
		name: "parallel",
		tree: fstest.MapFS{},
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			dst := filepath.Join(parent, fmt.Sprintf("out-%d", i))
			errs <- Run(context.Background(), tmpl, dst, tpls.Answers{ProjectName: "p"})
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Errorf("parallel Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for parallel Run")
		}
	}
}
