package cli

import (
	stderrors "errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cbip-solutions/hades-system/internal/errors"
)

func TestRender_CodedError_HappyPath(t *testing.T) {
	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		map[string]string{"uds_path": "/tmp/zen-swarm.sock"},
	)

	got := Render(err, RenderOpts{})

	required := []string{
		"HADES:",
		"\n  ",
		"\n  → ",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("Render output missing %q\nfull output:\n%s", sub, got)
		}
	}

	if !strings.Contains(got, "hades daemon") {
		t.Errorf("Render output missing required content %q\nfull output:\n%s", "hades daemon", got)
	}

	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("Render output ends with double newline:\nfull output:\n%s", got)
	}
}

func TestRender_NilError(t *testing.T) {
	got := Render(nil, RenderOpts{})
	if got != "" {
		t.Errorf("Render(nil, ...) = %q; want empty string", got)
	}
}

func TestRenderOpts_ZeroValueIsValid(t *testing.T) {
	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		nil,
	)
	got := Render(err, RenderOpts{})
	if got == "" {
		t.Errorf("Render with zero-value RenderOpts returned empty string; want HADES block")
	}
	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render with zero-value RenderOpts missing HADES headline:\n%s", got)
	}
}

func TestRender_VerboseAppendsTraceback(t *testing.T) {
	cause := stderrors.New("underlying transport error: connection refused")
	err := errors.New(
		errors.Code("daemon.not-running"),
		cause,
		map[string]string{"uds_path": "/tmp/zen-swarm.sock"},
	)

	got := Render(err, RenderOpts{Verbose: true})

	required := []string{"HADES:", "\n  ", "\n  → "}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("Render output missing HADES block marker %q\nfull output:\n%s", sub, got)
		}
	}

	if !strings.Contains(got, "connection refused") {
		t.Errorf("Render(verbose) output missing cause text %q\nfull output:\n%s", "connection refused", got)
	}

	arrowIdx := strings.Index(got, "→ ")
	verboseIdx := strings.Index(got, "connection refused")
	if arrowIdx == -1 || verboseIdx == -1 {
		t.Fatalf("setup invariant violated; arrowIdx=%d verboseIdx=%d", arrowIdx, verboseIdx)
	}
	if verboseIdx <= arrowIdx {
		t.Errorf("Render(verbose) traceback appears BEFORE recovery hint\nfull output:\n%s", got)
	}
}

func TestRender_VerboseFalse_NoTraceback(t *testing.T) {
	cause := stderrors.New("underlying transport error: connection refused")
	err := errors.New(
		errors.Code("daemon.not-running"),
		cause,
		nil,
	)

	got := Render(err, RenderOpts{Verbose: false})

	if strings.Contains(got, "connection refused") {
		t.Errorf("Render(non-verbose) leaked cause text into output\nfull output:\n%s", got)
	}
	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(non-verbose) missing HADES headline\nfull output:\n%s", got)
	}
}

func TestRender_VerboseUncodedError(t *testing.T) {
	err := stderrors.New("raw uncoded error: something went wrong")
	got := Render(err, RenderOpts{Verbose: true})
	if !strings.Contains(got, "something went wrong") {
		t.Errorf("Render(uncoded, verbose) missing cause text\nfull output:\n%s", got)
	}
}

type noFdWriter struct{ buf strings.Builder }

func (w *noFdWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func TestRender_NoColorTrue_SuppressesANSI(t *testing.T) {
	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		map[string]string{"uds_path": "/tmp/zen-swarm.sock"},
	)
	got := Render(err, RenderOpts{NoColor: true})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render(NoColor=true) leaked ANSI escape into output:\nfull output: %q", got)
	}
	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(NoColor=true) missing HADES headline:\n%s", got)
	}
	if !strings.Contains(got, "→ ") {
		t.Errorf("Render(NoColor=true) missing recovery arrow:\n%s", got)
	}
}

func TestRender_NoColorFalse_PreservesANSI(t *testing.T) {

	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		nil,
	)

	buf := &noFdWriter{}
	got := Render(err, RenderOpts{NoColor: false, Stream: buf})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render with noFdWriter Stream leaked ANSI:\nfull output: %q", got)
	}
	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render with noFdWriter Stream missing HADES headline:\n%s", got)
	}
}

func TestRender_NonTTYStream_AutoSuppresses(t *testing.T) {
	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		nil,
	)
	buf := &noFdWriter{}
	got := Render(err, RenderOpts{NoColor: false, Stream: buf})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render with non-TTY Stream leaked ANSI escape into output:\nfull output: %q", got)
	}
}

func TestRender_NilStream_TreatsAsNonTTY(t *testing.T) {
	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		nil,
	)
	got := Render(err, RenderOpts{NoColor: false, Stream: nil})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render with nil Stream leaked ANSI escape into output:\nfull output: %q", got)
	}
}

func TestRender_StderrStream_DetectsTTY(t *testing.T) {

	err := errors.New(
		errors.Code("daemon.not-running"),
		nil,
		nil,
	)
	got := Render(err, RenderOpts{NoColor: false, Stream: io.Discard})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render with io.Discard Stream leaked ANSI:\nfull output: %q", got)
	}
}

func isStderrTTY(w io.Writer) bool {
	type fdReader interface{ Fd() uintptr }
	if fd, ok := w.(fdReader); ok {
		return term.IsTerminal(int(fd.Fd()))
	}
	return false
}

func TestRender_UncodedError_RoutesThroughInternalUncaught(t *testing.T) {
	rawErr := stderrors.New("file open failed: permission denied")
	got := Render(rawErr, RenderOpts{NoColor: true})

	required := []string{
		"HADES:",
		"\n  ",
		"\n  → ",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("Render(uncoded) missing structural marker %q\nfull output:\n%s", sub, got)
		}
	}

	titleLower := strings.ToLower(got)
	if !strings.Contains(titleLower, "internal") || !strings.Contains(titleLower, "error") {
		t.Errorf("Render(uncoded) missing internal/error title text\nfull output:\n%s", got)
	}

	if !strings.Contains(got, "permission denied") {
		t.Errorf("Render(uncoded) missing cause text in body\nfull output:\n%s", got)
	}

	if !strings.Contains(got, "--verbose") {
		t.Errorf("Render(uncoded) recovery hint missing --verbose pointer\nfull output:\n%s", got)
	}
}

func TestRender_UncodedError_VerbosePreservesChain(t *testing.T) {
	rawErr := stderrors.New("file open failed: permission denied")
	got := Render(rawErr, RenderOpts{Verbose: true, NoColor: true})

	if !strings.Contains(got, "permission denied") {
		t.Errorf("Render(uncoded, verbose) missing cause text\nfull output:\n%s", got)
	}
	count := strings.Count(got, "permission denied")
	if count < 2 {
		t.Errorf("Render(uncoded, verbose) expected cause text in body AND traceback (count ≥2); got %d\nfull output:\n%s", count, got)
	}
}

func TestRender_PanicValueAsError(t *testing.T) {
	panicValue := any("nil pointer dereference at /path/to/file.go:42")
	err := fmt.Errorf("panic: %v", panicValue)

	got := Render(err, RenderOpts{NoColor: true, Verbose: true})

	required := []string{
		"HADES:",
		"nil pointer dereference",
		"--verbose",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("Render(panic-as-error) missing %q\nfull output:\n%s", sub, got)
		}
	}
}

func TestRender_WrappedCodedErrorPreservedThroughErrorsIs(t *testing.T) {
	target := errors.Code("daemon.not-running")
	err := errors.New(target, nil, nil)

	got := Render(err, RenderOpts{NoColor: true})
	if !strings.Contains(got, "HADES:") {
		t.Fatalf("Render produced empty HADES block\nfull output:\n%s", got)
	}

	targetErr := &errors.CodedError{Code: target}
	if !stderrors.Is(err, targetErr) {
		t.Errorf("stderrors.Is(err, targetErr) = false; want true (CodedError.Is contract broken)")
	}
}

func TestExitCodeFromError_SeverityMapping(t *testing.T) {

	if got := ExitCodeFromError(nil); got != 0 {
		t.Errorf("ExitCodeFromError(nil) = %d; want 0", got)
	}

	if got := ExitCodeFromError(stderrors.New("raw")); got != 2 {
		t.Errorf("ExitCodeFromError(raw) = %d; want 2 (fatal via internal-uncaught)", got)
	}

	cases := []struct {
		name string
		code errors.Code
		want int
	}{
		{"internal-uncaught (fatal)", errors.Code("internal-uncaught"), 2},
		{"cli.arg-validation-fail (error)", errors.Code("cli.arg-validation-fail"), 1},
		{"bypass.tier-degraded (warn)", errors.Code("bypass.tier-degraded"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New(tc.code, nil, nil)
			got := ExitCodeFromError(err)
			if got != tc.want {
				t.Errorf("ExitCodeFromError(%s) = %d; want %d", tc.code, got, tc.want)
			}
		})
	}
}

func TestExitCodeFromError_UnwrapChain(t *testing.T) {
	inner := errors.New(errors.Code("internal-uncaught"), nil, nil)
	wrapped := fmt.Errorf("transport: %w", inner)
	got := ExitCodeFromError(wrapped)
	if got != 2 {
		t.Errorf("ExitCodeFromError(wrapped fatal) = %d; want 2", got)
	}
}

func TestExitCodeFromError_AlwaysInRange(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"raw", stderrors.New("raw")},
		{"fatal", errors.New(errors.Code("internal-uncaught"), nil, nil)},
		{"error", errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)},
		{"warn", errors.New(errors.Code("bypass.tier-degraded"), nil, nil)},
		{"unknown-code", errors.New(errors.Code("unknown.code-xyz"), nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := ExitCodeFromError(tc.err)
			if code < 0 || code > 2 {
				t.Errorf("ExitCodeFromError out of range for %T %v: got %d", tc.err, tc.err, code)
			}
		})
	}
}

func TestAttachVerboseFlag_RegistersOnCmd(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AttachVerboseFlag(cmd)
	flag := cmd.PersistentFlags().Lookup("verbose")
	if flag == nil {
		t.Fatal("AttachVerboseFlag did not register --verbose persistent flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--verbose default = %q; want false", flag.DefValue)
	}
}

func TestAttachNoColorFlag_RegistersOnCmd(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AttachNoColorFlag(cmd)
	flag := cmd.PersistentFlags().Lookup("no-color")
	if flag == nil {
		t.Fatal("AttachNoColorFlag did not register --no-color persistent flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-color default = %q; want false", flag.DefValue)
	}
}

func TestRenderOptsFromCmd_ReadsPersistentFlags(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	AttachVerboseFlag(root)
	AttachNoColorFlag(root)

	if err := root.ParseFlags([]string{"--verbose", "--no-color"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := RenderOptsFromCmd(root)

	if !opts.Verbose {
		t.Errorf("RenderOptsFromCmd: Verbose=false; want true after --verbose")
	}
	if !opts.NoColor {
		t.Errorf("RenderOptsFromCmd: NoColor=false; want true after --no-color")
	}
}

func TestRender_CatalogMiss_NoColor(t *testing.T) {

	err := errors.New(errors.Code("unknown.code-for-catalog-miss-test"), nil, nil)
	got := Render(err, RenderOpts{NoColor: true})

	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(catalog-miss, no-color) missing HADES headline:\n%s", got)
	}
	if !strings.Contains(got, "catalog miss") {
		t.Errorf("Render(catalog-miss, no-color) missing 'catalog miss' text:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("Render(catalog-miss, no-color) leaked ANSI:\n%q", got)
	}
}

func TestRenderCoded_WithColorStream(t *testing.T) {

	err := errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)
	got := Render(err, RenderOpts{NoColor: false, Stream: os.Stderr})

	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render with os.Stderr stream missing HADES:\n%s", got)
	}
}

func TestExitCodeFromError_SeverityInfo(t *testing.T) {

	infoErr := errors.New(errors.Code("reserved.slot-1"), nil, nil)
	got := ExitCodeFromError(infoErr)
	if got != 0 {
		t.Errorf("ExitCodeFromError(reserved.slot-1 / SeverityInfo) = %d; want 0", got)
	}
}

type fdStream struct{ f *os.File }

func (s fdStream) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s fdStream) Fd() uintptr                 { return s.f.Fd() }

func TestRender_WithStderrFd_ColorConsistency(t *testing.T) {
	err := errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)
	stream := fdStream{os.Stderr}
	got := Render(err, RenderOpts{NoColor: false, Stream: stream})

	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render with fdStream missing HADES headline:\n%s", got)
	}

	wantColor := term.IsTerminal(int(os.Stderr.Fd()))
	hasANSI := strings.Contains(got, "\x1b[")
	if hasANSI != wantColor {
		t.Errorf("ANSI mismatch: hasANSI=%v wantColor=%v (term.IsTerminal result)\noutput: %q",
			hasANSI, wantColor, got)
	}
}

func TestRender_CatalogMiss_WithColor(t *testing.T) {
	err := errors.New(errors.Code("unknown.code-for-color-miss-test"), nil, nil)
	stream := fdStream{os.Stderr}
	got := Render(err, RenderOpts{NoColor: false, Stream: stream})

	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(catalog-miss with fdStream) missing HADES:\n%s", got)
	}
	if !strings.Contains(got, "catalog miss") {
		t.Errorf("Render(catalog-miss with fdStream) missing 'catalog miss':\n%s", got)
	}
}

func TestRenderUncoded_WithColor(t *testing.T) {
	err := stderrors.New("test raw error for color coverage")
	stream := fdStream{os.Stderr}
	got := Render(err, RenderOpts{NoColor: false, Stream: stream})

	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(uncoded with fdStream) missing HADES:\n%s", got)
	}
	if !strings.Contains(got, "test raw error for color coverage") {
		t.Errorf("Render(uncoded with fdStream) missing cause text:\n%s", got)
	}
}

func TestRenderCoded_DirectColorTrue(t *testing.T) {
	coded := errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)
	got := renderCoded(coded, true)

	if !strings.Contains(got, ansiCrimson) {
		t.Errorf("renderCoded(useColor=true) missing crimson ANSI:\n%q", got)
	}

	if !strings.Contains(got, "HADES:") {
		t.Errorf("renderCoded(useColor=true) missing HADES headline:\n%s", got)
	}

	if !strings.Contains(got, ansiGray) {
		t.Errorf("renderCoded(useColor=true) missing gray ANSI for body:\n%q", got)
	}

	if !strings.Contains(got, ansiGreen) {
		t.Errorf("renderCoded(useColor=true) missing green ANSI for hint:\n%q", got)
	}
}

func TestRenderCatalogMiss_DirectColorTrue(t *testing.T) {
	got := renderCatalogMiss(errors.Code("unknown.direct-color-test"), true)

	if !strings.Contains(got, ansiCrimson) {
		t.Errorf("renderCatalogMiss(useColor=true) missing crimson ANSI:\n%q", got)
	}
	if !strings.Contains(got, "catalog miss") {
		t.Errorf("renderCatalogMiss(useColor=true) missing 'catalog miss':\n%s", got)
	}
}

func TestRenderUncoded_DirectColorTrue(t *testing.T) {
	err := stderrors.New("direct color test cause")
	got := renderUncoded(err, true)

	if !strings.Contains(got, ansiCrimson) {
		t.Errorf("renderUncoded(useColor=true) missing crimson ANSI:\n%q", got)
	}
	if !strings.Contains(got, "direct color test cause") {
		t.Errorf("renderUncoded(useColor=true) missing cause text:\n%s", got)
	}
}

func TestRender_VerboseDividerPresent(t *testing.T) {
	err := errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)

	got := Render(err, RenderOpts{Verbose: true, NoColor: true})

	if !strings.Contains(got, strings.Repeat("─", 60)) {
		t.Errorf("Render(verbose) missing 60-char divider:\n%s", got)
	}
}

func TestRender_VerboseWithColorEnabled(t *testing.T) {

	err := errors.New(errors.Code("cli.arg-validation-fail"), nil, nil)
	stream := fdStream{os.Stdout}
	got := Render(err, RenderOpts{Verbose: true, NoColor: false, Stream: stream})

	if !strings.Contains(got, strings.Repeat("─", 60)) {
		t.Errorf("Render(verbose, fdStream) missing 60-char divider:\n%s", got)
	}
	if !strings.Contains(got, "HADES:") {
		t.Errorf("Render(verbose, fdStream) missing HADES headline:\n%s", got)
	}
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

func makeErrorValue(err error) reflect.Value {
	if err == nil {
		return reflect.Zero(errorType)
	}
	return reflect.ValueOf(err)
}

func TestRender_Property_NoPanicAlways(t *testing.T) {
	property := func(err error) bool {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Render panicked on input %T %v: panic=%v", err, err, r)
			}
		}()
		_ = Render(err, RenderOpts{})
		_ = Render(err, RenderOpts{Verbose: true})
		_ = Render(err, RenderOpts{NoColor: true})
		_ = Render(err, RenderOpts{Verbose: true, NoColor: true})
		return true
	}

	cfg := &quick.Config{
		MaxCount: 1000,
		Values: func(args []reflect.Value, r *rand.Rand) {
			args[0] = makeErrorValue(errorGenerator(r))
		},
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("Render property failed: %v", err)
	}
}

func TestRender_Property_AlwaysIncludesHadesHeadline(t *testing.T) {
	property := func(err error) bool {
		if err == nil {
			return true
		}
		out := Render(err, RenderOpts{NoColor: true})
		if !strings.Contains(out, "HADES:") {
			t.Errorf("Render output missing 'HADES:' for input %T %v\noutput: %q", err, err, out)
			return false
		}
		if out == "" {
			t.Errorf("Render returned empty string for non-nil error %T %v", err, err)
			return false
		}
		return true
	}

	cfg := &quick.Config{
		MaxCount: 1000,
		Values: func(args []reflect.Value, r *rand.Rand) {
			args[0] = makeErrorValue(errorGenerator(r))
		},
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("HADES headline property failed: %v", err)
	}
}

func TestRender_Property_ExitCodeAlwaysInRange(t *testing.T) {
	property := func(err error) bool {
		code := ExitCodeFromError(err)
		if code < 0 || code > 2 {
			t.Errorf("ExitCodeFromError out of range for input %T %v: got %d", err, err, code)
			return false
		}
		return true
	}

	cfg := &quick.Config{
		MaxCount: 1000,
		Values: func(args []reflect.Value, r *rand.Rand) {
			args[0] = makeErrorValue(errorGenerator(r))
		},
	}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("exit code range property failed: %v", err)
	}
}

func errorGenerator(r *rand.Rand) error {
	if r.Intn(20) == 0 {
		return nil
	}

	knownCodes := []errors.Code{
		errors.Code("daemon.not-running"),
		errors.Code("daemon.unreachable"),
		errors.Code("provider.auth-401"),
		errors.Code("provider.quota-429"),
		errors.Code("provider.network-timeout"),
		errors.Code("bypass.config-missing"),
		errors.Code("bypass.tier-degraded"),
		errors.Code("wizard.config-corrupt"),
		errors.Code("plugin.load-error"),
		errors.Code("cli.unknown-subcommand"),
		errors.Code("cli.arg-validation-fail"),
		errors.Code("skin.skin-not-registered"),
		errors.Code("internal-uncaught"),
	}

	roll := r.Intn(100)
	switch {
	case roll < 30:
		code := knownCodes[r.Intn(len(knownCodes))]
		return errors.New(code, nil, randomContext(r))
	case roll < 50:
		code := errors.Code(fmt.Sprintf("unknown.code-%d", r.Intn(10000)))
		return errors.New(code, nil, nil)
	case roll < 80:
		return stderrors.New(randomASCII(r, 4, 64))
	default:
		inner := stderrors.New(randomASCII(r, 4, 32))
		return fmt.Errorf("%s: %w", randomASCII(r, 4, 16), inner)
	}
}

func randomASCII(r *rand.Rand, minLen, maxLen int) string {
	n := minLen + r.Intn(maxLen-minLen+1)
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(0x20 + r.Intn(0x7e-0x20+1))
	}
	return string(b)
}

func randomContext(r *rand.Rand) map[string]string {
	if r.Intn(2) == 0 {
		return nil
	}
	n := 1 + r.Intn(3)
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		out[fmt.Sprintf("key%d", i)] = randomASCII(r, 1, 24)
	}
	return out
}

func TestRender_UnknownSubcommand_IncludesSuggestion(t *testing.T) {

	err := errors.New(
		errors.Code("cli.unknown-subcommand"),
		nil,
		map[string]string{
			"suggestion_line": "did you mean `hades doctor`? · ",
		},
	)

	got := Render(err, RenderOpts{NoColor: true})

	if !strings.Contains(got, "HADES:") {
		t.Fatalf("output missing 'HADES:' headline\nfull output:\n%s", got)
	}

	if !strings.Contains(got, "did you mean") {
		t.Errorf("rendered output missing 'did you mean' suggestion\nfull output:\n%s", got)
	}
	if !strings.Contains(got, "doctor") {
		t.Errorf("rendered output missing suggested command 'doctor'\nfull output:\n%s", got)
	}
}

// TestRender_UnknownSubcommand_NoFabricatedSuggestion asserts two safety
// properties when no suggestion_line is provided:
// 1. The rendered output does NOT contain "did you mean".
// 2. The rendered output does NOT contain a dangling "{{" template literal
// (the slot must vanish when its context value is empty string "").
//
// The builder in cmd/zen/main.go MUST always set Context["suggestion_line"]
// — to "" when no candidate exists so the {{suggestion_line}} slot in the
// BodyTemplate resolves to the empty string rather than leaking the literal.
func TestRender_UnknownSubcommand_NoFabricatedSuggestion(t *testing.T) {

	err := errors.New(
		errors.Code("cli.unknown-subcommand"),
		nil,
		map[string]string{
			"suggestion_line": "",
		},
	)

	got := Render(err, RenderOpts{NoColor: true})

	if !strings.Contains(got, "HADES:") {
		t.Fatalf("output missing 'HADES:' headline\nfull output:\n%s", got)
	}
	if strings.Contains(got, "did you mean") {
		t.Errorf("no-suggestion output fabricated 'did you mean'\nfull output:\n%s", got)
	}

	if strings.Contains(got, "{{") {
		t.Errorf("rendered output has dangling '{{' template literal\nfull output:\n%s", got)
	}
}
