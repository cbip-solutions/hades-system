// SPDX-License-Identifier: MIT
package ecosystem

// verifier_live_cmd.go — production LiveCmdRunner (HADES design
// task D-5, Appendix A.3).
//
// Shells out to per-ecosystem package-manager / doc-tool binaries:
//
// Go `go doc -short <symbolPath>`
// Python `python3 -c "<argv-passed script>" <module> <attr>`
// npm/TS: `npm view -- <pkg> name version` (package-existence proxy)
// Rust `cargo search --limit 1 -- <crate>` (crate-existence proxy)
//
// Production correctness: every Cmd is rebuilt under exec.CommandContext
// so ctx cancellation aborts the in-flight process. Exit code 1 is
// treated as "not found" (most tools surface absence with exit 1 + a
// minimal error line); other non-zero exits are surfaced as errors so
// the caller can distinguish "symbol absent" from "tool broken".
//
// Security hardening (D-5 fix-cycle 2 — verifier is the LLM-hallucination
// anchor per USENIX Sec 2025 arXiv 2406.10279 §5):
//
// - validateSymbolRef (in verifier.go) gates SymbolPath at the cascade
// entry. The regex restricts paths to ASCII identifiers separated by
// '.', '/' or ':'. Leading '-' is structurally rejected.
//
// - Python branch: script no longer interpolates user input via
// fmt.Sprintf into the `-c` body. Module + attr are passed via
// sys.argv[1..2] so a malicious SymbolPath cannot smuggle Python
// statements into the interpreter (CRIT-1).
//
// - npm + cargo branches: end-of-options '--' separator inserted
// before the user-derived package name. Combined with the regex
// above, this neutralises argv-flag injection (CRIT-2).
//
// - Run() wraps ctx with WithTimeout(defaultLiveCmdTimeout) so a hung
// subprocess cannot block the dispatcher indefinitely even when the
// caller passes a non-deadlined ctx (IMP-1).
//
// - Stdout + stderr are written into a *limitedBuffer capped at
// defaultLiveCmdOutputCap (1 MiB). Truncation is reported back
// to the runner contract via liveCmdResult.Truncated so the
// dispatcher can surface partial-output provenance (IMP-2).
//
// Sandboxing note: in restricted environments where the per-ecosystem
// binary is not on PATH, defaultCmdBuilder still constructs the Cmd
// (exec.Command does not verify PATH at construction time). The Run
// call then fails with "executable file not found in $PATH" wrapped
// in *exec.Error; isExitCodeOne returns false for that path, so the
// error propagates up as expected. The verifier_live_cmd_test.go suite
// exercises both the happy path (via custom cmdBuilder that runs `echo`)
// and the missing-binary path.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultLiveCmdTimeout   = 5 * time.Second
	defaultLiveCmdOutputCap = 1 << 20
)

type execLiveCmdRunner struct {
	cmdBuilder func(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error)
	timeout    time.Duration
	outputCap  int
}

func NewExecLiveCmdRunner() *execLiveCmdRunner {
	return &execLiveCmdRunner{
		cmdBuilder: defaultCmdBuilder,
		timeout:    defaultLiveCmdTimeout,
		outputCap:  defaultLiveCmdOutputCap,
	}
}

func (r *execLiveCmdRunner) Run(ctx context.Context, eco Ecosystem, ref SymbolRef) (liveCmdResult, error) {
	cmd, err := r.cmdBuilder(eco, ref)
	if err != nil {
		return liveCmdResult{}, err
	}

	timeout := r.timeout
	if timeout <= 0 {
		timeout = defaultLiveCmdTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []string
	if len(cmd.Args) > 1 {
		args = cmd.Args[1:]
	}
	ctxCmd := exec.CommandContext(cmdCtx, cmd.Path, args...)

	cap := r.outputCap
	if cap <= 0 {
		cap = defaultLiveCmdOutputCap
	}
	out := &limitedBuffer{cap: cap}
	ctxCmd.Stdout = out
	ctxCmd.Stderr = out

	err = ctxCmd.Run()
	if err != nil {

		if ctxErr := cmdCtx.Err(); ctxErr != nil {
			return liveCmdResult{Truncated: out.Truncated()}, ctxErr
		}
		if isExitCodeOne(err) {
			return liveCmdResult{Exists: false, Truncated: out.Truncated()}, nil
		}
		return liveCmdResult{Truncated: out.Truncated()}, fmt.Errorf("live cmd %s: %w; out=%s", eco, err, out.String())
	}
	body := out.String()
	if strings.TrimSpace(body) == "" {
		return liveCmdResult{Exists: false, Truncated: out.Truncated()}, nil
	}
	return liveCmdResult{
		Exists:    true,
		Signature: extractSignature(eco, body),
		Truncated: out.Truncated(),
	}, nil
}

func defaultCmdBuilder(eco Ecosystem, ref SymbolRef) (*exec.Cmd, error) {
	switch eco {
	case EcoGo:
		return exec.Command("go", "doc", "-short", ref.SymbolPath), nil
	case EcoPython:

		const pyScript = "import importlib,sys;" +
			"m=importlib.import_module(sys.argv[1]);" +
			"print(getattr(m, sys.argv[2], m).__doc__ or m.__name__)"
		return exec.Command("python3", "-c", pyScript, firstPart(ref.SymbolPath), lastPart(ref.SymbolPath)), nil
	case EcoTypeScript:

		return exec.Command("npm", "view", "--", firstPart(ref.SymbolPath), "name", "version"), nil
	case EcoRust:

		return exec.Command("cargo", "search", "--limit", "1", "--", firstPart(ref.SymbolPath)), nil
	}
	return nil, fmt.Errorf("live cmd: unsupported ecosystem %s", eco)
}

func firstPart(p string) string {
	if idx := strings.IndexAny(p, "./:"); idx >= 0 {
		return p[:idx]
	}
	return p
}

func lastPart(p string) string {
	if idx := strings.LastIndexAny(p, "./:"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func isExitCodeOne(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 1
	}
	return false
}

func extractSignature(eco Ecosystem, body string) string {
	_ = eco
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "#") {
			continue
		}
		return l
	}
	return strings.TrimSpace(body)
}

type limitedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len() >= l.cap {
		l.truncated = true
		return len(p), nil
	}
	available := l.cap - l.buf.Len()
	if len(p) > available {
		l.truncated = true

		_, _ = l.buf.Write(p[:available])
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) String() string { return l.buf.String() }

func (l *limitedBuffer) Truncated() bool { return l.truncated }
