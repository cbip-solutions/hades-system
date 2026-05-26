package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecLiveCmdRunner_Run_Success(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("echo", "func Sum256(data []byte) [Size]byte"), nil
		},
	}
	res, err := r.Run(context.Background(), EcoGo, SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Exists {
		t.Errorf("Exists = false; want true")
	}
	if !strings.Contains(res.Signature, "Sum256") {
		t.Errorf("Signature = %q; want to contain 'Sum256'", res.Signature)
	}
}

func TestExecLiveCmdRunner_Run_EmptyOutput_NotFound(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("true"), nil
		},
	}
	res, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Exists {
		t.Errorf("Exists = true on empty output; want false")
	}
}

func TestExecLiveCmdRunner_Run_ExitOne_NotFound(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("sh", "-c", "exit 1"), nil
		},
	}
	res, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err != nil {
		t.Fatalf("exit 1 should not surface error; got %v", err)
	}
	if res.Exists {
		t.Errorf("Exists = true on exit 1; want false")
	}
}

func TestExecLiveCmdRunner_Run_ExitTwo_Error(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("sh", "-c", "echo broken >&2; exit 2"), nil
		},
	}
	_, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err == nil {
		t.Errorf("expected error on exit 2; got nil")
	}
	if !strings.Contains(err.Error(), "live cmd") {
		t.Errorf("error should mention live cmd context; got %v", err)
	}
}

func TestExecLiveCmdRunner_Run_CmdBuilderError(t *testing.T) {
	want := errors.New("builder boom")
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return nil, want
		},
	}
	_, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if !errors.Is(err, want) {
		t.Errorf("expected wrapped builder error; got %v", err)
	}
}

func TestExecLiveCmdRunner_Run_ContextCancelled(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {

			return exec.Command("sleep", "30"), nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := r.Run(ctx, EcoGo, SymbolRef{})
	if err == nil {
		t.Fatalf("expected error from cancelled cmd; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
}

func TestNewExecLiveCmdRunner_BuildsDefault(t *testing.T) {
	r := NewExecLiveCmdRunner()
	if r == nil {
		t.Fatalf("NewExecLiveCmdRunner returned nil")
	}
	if r.cmdBuilder == nil {
		t.Errorf("cmdBuilder must be wired to defaultCmdBuilder")
	}
}

func TestDefaultCmdBuilder_Go(t *testing.T) {
	cmd, err := defaultCmdBuilder(EcoGo, SymbolRef{SymbolPath: "crypto/sha256.Sum256"})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got := lastSegment(cmd.Path); got != "go" {
		t.Errorf("path = %s; want 'go'", got)
	}
	if want := []string{"go", "doc", "-short", "crypto/sha256.Sum256"}; !equalSlice(cmd.Args, want) {
		t.Errorf("args = %v; want %v", cmd.Args, want)
	}
}

func TestDefaultCmdBuilder_Python(t *testing.T) {
	cmd, err := defaultCmdBuilder(EcoPython, SymbolRef{SymbolPath: "functools.partial"})
	if err != nil {
		t.Fatalf("Python: %v", err)
	}
	if got := lastSegment(cmd.Path); got != "python3" {
		t.Errorf("path = %s; want 'python3'", got)
	}
	if len(cmd.Args) != 5 {
		t.Fatalf("python cmd should have 5 args (python3 -c <script> <module> <attr>); got %d: %v", len(cmd.Args), cmd.Args)
	}
	if cmd.Args[1] != "-c" {
		t.Errorf("argv[1] should be `-c`; got %q", cmd.Args[1])
	}

	if strings.Contains(cmd.Args[2], "functools") {
		t.Errorf("CRIT-1: script body must NOT contain user-derived module name; got %q", cmd.Args[2])
	}
	if strings.Contains(cmd.Args[2], "partial") {
		t.Errorf("CRIT-1: script body must NOT contain user-derived attribute; got %q", cmd.Args[2])
	}
	if !strings.Contains(cmd.Args[2], "sys.argv[1]") || !strings.Contains(cmd.Args[2], "sys.argv[2]") {
		t.Errorf("script body must use sys.argv passing; got %q", cmd.Args[2])
	}

	if cmd.Args[3] != "functools" {
		t.Errorf("argv[3] (module) = %q; want 'functools'", cmd.Args[3])
	}
	if cmd.Args[4] != "partial" {
		t.Errorf("argv[4] (attr) = %q; want 'partial'", cmd.Args[4])
	}
}

func TestDefaultCmdBuilder_TypeScript(t *testing.T) {
	cmd, err := defaultCmdBuilder(EcoTypeScript, SymbolRef{SymbolPath: "react.useState"})
	if err != nil {
		t.Fatalf("TS: %v", err)
	}
	if got := lastSegment(cmd.Path); got != "npm" {
		t.Errorf("path = %s; want 'npm'", got)
	}
	// CRIT-2: `--` end-of-options separator MUST precede the user-derived
	// package name to prevent argv-flag injection on a hypothetical
	// hostile name that slips past the regex.
	if want := []string{"npm", "view", "--", "react", "name", "version"}; !equalSlice(cmd.Args, want) {
		t.Errorf("args = %v; want %v (note `--` end-of-options separator)", cmd.Args, want)
	}
}

func TestDefaultCmdBuilder_Rust(t *testing.T) {
	cmd, err := defaultCmdBuilder(EcoRust, SymbolRef{SymbolPath: "tokio::spawn"})
	if err != nil {
		t.Fatalf("Rust: %v", err)
	}
	if got := lastSegment(cmd.Path); got != "cargo" {
		t.Errorf("path = %s; want 'cargo'", got)
	}

	if want := []string{"cargo", "search", "--limit", "1", "--", "tokio"}; !equalSlice(cmd.Args, want) {
		t.Errorf("args = %v; want %v (note `--` end-of-options separator)", cmd.Args, want)
	}
}

func TestDefaultCmdBuilder_Unsupported(t *testing.T) {
	_, err := defaultCmdBuilder(Ecosystem("bogus"), SymbolRef{})
	if err == nil {
		t.Errorf("unsupported ecosystem should return error")
	}
	if !strings.Contains(err.Error(), "unsupported ecosystem") {
		t.Errorf("error should mention unsupported; got %v", err)
	}
}

func TestFirstPart(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"crypto/sha256.Sum256", "crypto"},
		{"functools.partial", "functools"},
		{"std::async::spawn", "std"},
		{"noseparator", "noseparator"},
		{"", ""},
	}
	for _, c := range cases {
		if got := firstPart(c.in); got != c.want {
			t.Errorf("firstPart(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestLastPart(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"crypto/sha256.Sum256", "Sum256"},
		{"functools.partial", "partial"},
		{"std::async::spawn", "spawn"},
		{"noseparator", "noseparator"},
		{"", ""},
	}
	for _, c := range cases {
		if got := lastPart(c.in); got != c.want {
			t.Errorf("lastPart(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestIsExitCodeOne(t *testing.T) {

	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	if !isExitCodeOne(err) {
		t.Errorf("exit 1 should be detected as exit-code-one; got err=%v", err)
	}

	cmd2 := exec.Command("sh", "-c", "exit 2")
	err2 := cmd2.Run()
	if isExitCodeOne(err2) {
		t.Errorf("exit 2 should NOT be detected as exit-code-one; got true")
	}

	if isExitCodeOne(nil) {
		t.Errorf("nil should return false")
	}

	if isExitCodeOne(fmt.Errorf("plain error")) {
		t.Errorf("non-ExitError should return false")
	}
}

func TestExtractSignature(t *testing.T) {
	cases := []struct {
		name string
		eco  Ecosystem
		in   string
		want string
	}{
		{"empty", EcoGo, "", ""},
		{"single-line", EcoGo, "func F()", "func F()"},
		{"skip-blank-then-content", EcoGo, "\n\nfunc F()\n", "func F()"},
		{"skip-go-comment", EcoGo, "// header\nfunc F()", "func F()"},
		{"skip-python-comment", EcoPython, "# header\nfunc F()", "func F()"},
		{"only-comments-fallback", EcoGo, "// a\n// b", "// a\n// b"},
		{"trim-whitespace-line", EcoGo, "   \n  func X()  \n", "func X()"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractSignature(c.eco, c.in); got != c.want {
				t.Errorf("extractSignature(%q, %q) = %q; want %q", c.eco, c.in, got, c.want)
			}
		})
	}
}

// Even if a hostile SymbolPath bypasses validateSymbolRef (e.g., by
// being passed directly to defaultCmdBuilder), the Python branch MUST
// NOT interpolate it into the `-c` script body. The argv-passing
// design guarantees the script is a constant; this test pins the
// invariant.
//
// Attack shape that historically would have worked:
//
//	SymbolPath = `os; __import__('os').system('whoami')#.x`
//	→ firstPart = "os; __import__('os').system('whoami')#"
//	→ embedded raw into Python `-c` script → RCE.
//
// Under the argv-passing design, the substring above ends up in
// argv[3] / argv[4] as a string literal. Python's importlib then
// raises ModuleNotFoundError because there's no module with that name.
// The subprocess never executes hostile code.
func TestDefaultCmdBuilder_Python_NoScriptInterpolation_HostileInput(t *testing.T) {
	hostile := SymbolRef{SymbolPath: `os; __import__('os').system('whoami')#.x`}
	cmd, err := defaultCmdBuilder(EcoPython, hostile)
	if err != nil {
		t.Fatalf("defaultCmdBuilder should not error on input shape alone: %v", err)
	}

	const wantScript = "import importlib,sys;" +
		"m=importlib.import_module(sys.argv[1]);" +
		"print(getattr(m, sys.argv[2], m).__doc__ or m.__name__)"
	if cmd.Args[2] != wantScript {
		t.Errorf("script body changed from constant — interpolation regression!\n got:  %q\n want: %q", cmd.Args[2], wantScript)
	}

	if strings.Contains(cmd.Args[2], "system") {
		t.Errorf("script body MUST NOT contain user-derived 'system'; got %q", cmd.Args[2])
	}
	if strings.Contains(cmd.Args[2], "whoami") {
		t.Errorf("script body MUST NOT contain user-derived 'whoami'; got %q", cmd.Args[2])
	}
	if strings.Contains(cmd.Args[2], "__import__") {
		t.Errorf("script body MUST NOT contain user-derived '__import__'; got %q", cmd.Args[2])
	}

	if cmd.Args[3] == "" {
		t.Errorf("argv[3] (module) must be non-empty data; got empty")
	}
	if cmd.Args[4] == "" {
		t.Errorf("argv[4] (attr) must be non-empty data; got empty")
	}
}

func TestDefaultCmdBuilder_NpmCargo_HaveEndOfOptionsSeparator(t *testing.T) {
	t.Run("npm", func(t *testing.T) {
		cmd, _ := defaultCmdBuilder(EcoTypeScript, SymbolRef{SymbolPath: "lodash"})

		var sepIdx, pkgIdx int = -1, -1
		for i, a := range cmd.Args {
			if a == "--" {
				sepIdx = i
			}
			if a == "lodash" {
				pkgIdx = i
			}
		}
		if sepIdx < 0 {
			t.Fatalf("npm cmd missing `--` separator: %v", cmd.Args)
		}
		if pkgIdx < 0 {
			t.Fatalf("npm cmd missing package name: %v", cmd.Args)
		}
		if sepIdx >= pkgIdx {
			t.Errorf("`--` must precede package name; sepIdx=%d pkgIdx=%d args=%v", sepIdx, pkgIdx, cmd.Args)
		}
	})
	t.Run("cargo", func(t *testing.T) {
		cmd, _ := defaultCmdBuilder(EcoRust, SymbolRef{SymbolPath: "serde"})
		var sepIdx, pkgIdx int = -1, -1
		for i, a := range cmd.Args {
			if a == "--" {
				sepIdx = i
			}
			if a == "serde" {
				pkgIdx = i
			}
		}
		if sepIdx < 0 {
			t.Fatalf("cargo cmd missing `--` separator: %v", cmd.Args)
		}
		if pkgIdx < 0 {
			t.Fatalf("cargo cmd missing crate name: %v", cmd.Args)
		}
		if sepIdx >= pkgIdx {
			t.Errorf("`--` must precede crate name; sepIdx=%d pkgIdx=%d args=%v", sepIdx, pkgIdx, cmd.Args)
		}
	})
}

// Even with a non-deadlined ctx, Run() must bound subprocess wall-clock
// via its internal timeout. The runner is configured with a tight
// 100 ms cap, so a `sleep 30` cmd MUST surface DeadlineExceeded well
// before 30 s elapse.
func TestExecLiveCmdRunner_Run_InternalTimeout_KillsHungSubprocess(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("sleep", "30"), nil
		},
		timeout:   100 * time.Millisecond,
		outputCap: defaultLiveCmdOutputCap,
	}
	start := time.Now()
	_, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error; got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded; got %v", err)
	}

	if elapsed > 2*time.Second {
		t.Errorf("internal timeout failed to bound runtime; took %v", elapsed)
	}
}

// Default timeout (no field override) MUST fall back to
// defaultLiveCmdTimeout (5 s). Cannot test the 5 s value directly
// without a long-running test, so this verifies the construction path
// for the default-timeout fallback at the runner-config layer.
func TestNewExecLiveCmdRunner_HasDefaultTimeout(t *testing.T) {
	r := NewExecLiveCmdRunner()
	if r.timeout != defaultLiveCmdTimeout {
		t.Errorf("default timeout = %v; want %v", r.timeout, defaultLiveCmdTimeout)
	}
	if r.outputCap != defaultLiveCmdOutputCap {
		t.Errorf("default outputCap = %d; want %d", r.outputCap, defaultLiveCmdOutputCap)
	}
}

func TestExecLiveCmdRunner_Run_ZeroTimeout_FallsBackToDefault(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("echo", "ok"), nil
		},
		timeout:   0,
		outputCap: 0,
	}

	res, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err != nil {
		t.Fatalf("Run with zero-timeout fallback: %v", err)
	}
	if !res.Exists {
		t.Errorf("expected Exists=true from echo; got false")
	}
}

// Caller-provided ctx with deadline shorter than r.timeout MUST win
// (stdlib context.WithTimeout propagates by minimum). The caller
// keeps control of the upper bound.
func TestExecLiveCmdRunner_Run_CallerCtxShorterDeadline_Wins(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("sleep", "30"), nil
		},
		timeout:   30 * time.Second,
		outputCap: defaultLiveCmdOutputCap,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, EcoGo, SymbolRef{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout from caller's ctx; got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded; got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("caller ctx deadline failed to bound runtime; took %v", elapsed)
	}
}

func TestExecLiveCmdRunner_Run_OutputCappedAt1MB(t *testing.T) {

	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("sh", "-c", "head -c 2097152 /dev/zero | tr '\\0' 'a'"), nil
		},
		timeout:   5 * time.Second,
		outputCap: 1 << 20,
	}
	res, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true on >1 MiB output; got false")
	}
	if !res.Exists {
		t.Errorf("expected Exists=true (non-empty output even after truncation); got false")
	}
}

func TestExecLiveCmdRunner_Run_OutputBelowCap_NotTruncated(t *testing.T) {
	r := &execLiveCmdRunner{
		cmdBuilder: func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
			return exec.Command("echo", "small output"), nil
		},
		timeout:   5 * time.Second,
		outputCap: 1 << 20,
	}
	res, err := r.Run(context.Background(), EcoGo, SymbolRef{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Truncated {
		t.Errorf("Truncated=true on tiny output; want false")
	}
}

func TestLimitedBuffer_Write_BelowCap_StoresVerbatim(t *testing.T) {
	lb := &limitedBuffer{cap: 16}
	n, err := lb.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d; want 5", n)
	}
	if lb.String() != "hello" {
		t.Errorf("String = %q; want 'hello'", lb.String())
	}
	if lb.Truncated() {
		t.Errorf("Truncated must be false below cap")
	}
}

func TestLimitedBuffer_Write_AcrossCap_TruncatesAndFlags(t *testing.T) {
	lb := &limitedBuffer{cap: 4}
	n, err := lb.Write([]byte("abcdef"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 6 {
		t.Errorf("n = %d; want 6 (Write must lie about success to satisfy io.Writer contract)", n)
	}
	if lb.String() != "abcd" {
		t.Errorf("String = %q; want 'abcd'", lb.String())
	}
	if !lb.Truncated() {
		t.Errorf("Truncated must be true on cross-cap write")
	}
}

func TestLimitedBuffer_Write_AtFullCap_DiscardsAll(t *testing.T) {
	lb := &limitedBuffer{cap: 4}
	_, _ = lb.Write([]byte("abcd"))
	if lb.Truncated() {
		t.Errorf("exactly-at-cap write should NOT set Truncated; got true")
	}
	n, err := lb.Write([]byte("ef"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d; want 2 (Writer contract)", n)
	}
	if lb.String() != "abcd" {
		t.Errorf("String = %q; want 'abcd' (discard past full)", lb.String())
	}
	if !lb.Truncated() {
		t.Errorf("Truncated must be true after over-cap write")
	}
}

func lastSegment(p string) string {
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
