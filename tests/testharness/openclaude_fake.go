// SPDX-License-Identifier: MIT
// Package testharness — creates the fake OpenClaude subprocess so
// internal/workforce/subprocess/ unit + integration tests can exercise the
// stdio JSON-RPC contract without depending on the real openclaude binary
// .
//
// The fake is invoked by re-execing the calling test binary with the env
// variables GO_WANT_HELPER_OPENCLAUDE_FAKE=1 + ZEN_FAKE_OPENCLAUDE_*. The
// real test (helperFakeCmd) constructs the Cmd; the helper test in the
// same package (TestHelperOpenClaudeFake) calls RunOpenClaudeFake.
//
// Scenarios (selected by ZEN_FAKE_OPENCLAUDE_SCENARIO):
//
// "happy-path" -> echo each request as a JSON-RPC result; exit on EOF
// "hang" -> read all input, never write, ignore SIGTERM for 30s
// (used to test SubprocessManager TTL evictor escalation
// from SIGTERM to SIGKILL after 10s grace)
// "crash" -> after the first request, write nothing and exit 7
// (used to test SIGCHLD detector + persistent-spec replay)
// "interactive-mock" -> emit 4 tool_use events then a final result; exits 0
// on EOF
//
// Each scenario is deterministic and self-contained — no external network,
// no temp files.
package testharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var FakeScenarios = []string{
	"happy-path",
	"hang",
	"crash",
	"interactive-mock",
}

var exitFunc = os.Exit

func RunOpenClaudeFake() {
	runFakeWithIO(os.Getenv("ZEN_FAKE_OPENCLAUDE_SCENARIO"),
		os.Getenv("ZEN_FAKE_OPENCLAUDE_THREAD_ID"),
		os.Getenv("ZEN_FAKE_OPENCLAUDE_WORKTREE"),
		os.Stdin, os.Stdout, os.Stderr)
}

func runFakeWithIO(scenario, threadID, worktree string,
	in io.Reader, out, errw io.Writer) {
	if worktree != "" {
		if _, err := os.Stat(worktree); err != nil {
			fmt.Fprintf(errw, "fake: worktree stat: %v\n", err)
			exitFunc(2)
			return
		}
	}
	switch scenario {
	case "happy-path":
		runHappyPath(in, out, threadID)
		exitFunc(0)
		return
	case "hang":
		runHang(in, out)
		exitFunc(0)
		return
	case "crash":
		runCrash(in, out, threadID)
		exitFunc(7)
		return
	case "interactive-mock":
		runInteractiveMock(in, out, threadID)
		exitFunc(0)
		return
	default:
		fmt.Fprintf(errw, "fake: unknown scenario %q (valid: %v)\n", scenario, FakeScenarios)
		exitFunc(2)
		return
	}
}

func runHappyPath(in io.Reader, out io.Writer, threadID string) {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "fake: read: %v\n", err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
			writeError(w, nil, -32700, fmt.Sprintf("parse error: %v", err))
			continue
		}
		id := req["id"]
		method, _ := req["method"].(string)
		writeResult(w, id, map[string]any{
			"thread_id": threadID,
			"echo":      method,
			"text":      "ok",
		})
	}
}

var hangSleep = 30 * time.Second

func runHang(in io.Reader, out io.Writer) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		<-sigs
		// swallow; do not exit
		time.Sleep(hangSleep)
	}()

	if out != nil {
		w := bufio.NewWriter(out)
		writeNotification(w, "ready", map[string]any{"scenario": "hang"})
		_ = w.Flush()
	}
	r := bufio.NewReader(in)
	for {
		_, err := r.ReadString('\n')
		if err == io.EOF {
			time.Sleep(hangSleep)
			return
		}
		if err != nil {
			return
		}

	}
}

func runCrash(in io.Reader, _ io.Writer, _ string) {
	r := bufio.NewReader(in)
	_, _ = r.ReadString('\n')
}

func runInteractiveMock(in io.Reader, out io.Writer, threadID string) {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
			writeError(w, nil, -32700, "parse error")
			continue
		}
		id := req["id"]

		for i := 0; i < 4; i++ {
			writeNotification(w, "tool_use", map[string]any{
				"thread_id": threadID,
				"index":     i,
				"name":      []string{"research_dispatch", "ssh_exec_validate", "audit_review", "budget_cap_status"}[i],
			})
			_ = w.Flush()
		}
		writeResult(w, id, map[string]any{
			"thread_id": threadID,
			"text":      "interactive-mock done",
		})
	}
}

func writeResult(w *bufio.Writer, id, result any) {
	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	b, _ := json.Marshal(frame)
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

func writeError(w *bufio.Writer, id any, code int, msg string) {
	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	}
	b, _ := json.Marshal(frame)
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

func writeNotification(w *bufio.Writer, method string, params any) {
	frame := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(frame)
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}
