// tests/compliance/inv_zen_227_exec_safety_test.go
//
// inv-zen-227 (v0.17.7 / C-3) — exec process-group kill + concurrency cap.
//
// Root cause of the v0.17.7 hot-fix: osExecer.Run used exec.CommandContext,
// which sends SIGKILL only to the direct child process. If the child forks
// grandchildren that hold the combined-output pipe open, CombinedOutput()
// blocks until all grandchildren exit — potentially 30s+ for a `sleep 30 &
// wait` pattern.
//
// inv-zen-227 pins the fix: osExecer.Run MUST
//  1. set cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} — puts the
//     child in its own process group so a -pid kill reaches every descendant;
//  2. define cmd.Cancel — the custom cancel func that kills the entire process
//     group via syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL);
//  3. set cmd.WaitDelay — unblocks CombinedOutput even if grandchildren hold
//     the pipe after the cancel fires (Go 1.20+);
//  4. acquire execSem before running the command — caps concurrent execs at
//     execMaxConcurrent so a burst of autonomy-check calls cannot exhaust the
//     host's process table.
//
// This file is a source-level compliance guard (regex scan over the
// implementation file). The behavioral companion tests live in
// internal/daemon/orchestrator_plan5_exec_test.go:
//   - TestOsExecer_TimeoutKillsProcessGroup — confirms Run returns in <5s when
//     a grandchild holds the pipe open;
//   - TestOsExecer_ConcurrencyCapped — confirms the (N+1)th TryAcquire fails.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot227(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-227: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-227: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func extractOsExecerRunBody(t *testing.T, src string) string {
	t.Helper()
	lines := strings.Split(src, "\n")
	var body strings.Builder
	depth := 0
	inFunc := false
	for _, line := range lines {
		if !inFunc {
			if strings.Contains(line, "func (osExecer) Run(") {
				inFunc = true
				depth = strings.Count(line, "{") - strings.Count(line, "}")
				body.WriteString(line)
				body.WriteByte('\n')
				if depth == 0 {
					break
				}
			}
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth == 0 {
			break
		}
	}
	if !inFunc {
		t.Fatalf("inv-zen-227: func (osExecer) Run not found in implementation file")
	}
	return body.String()
}

func TestInvZen227ExecSafety(t *testing.T) {
	root := repoRoot227(t)
	implFile := filepath.Join(root, "internal", "daemon", "orchestrator_plan5_service_more.go")

	raw, err := os.ReadFile(implFile)
	if err != nil {
		t.Fatalf("inv-zen-227: read %s: %v", implFile, err)
	}
	src := string(raw)

	body := extractOsExecerRunBody(t, src)

	checks := []struct {
		name    string
		pattern string
		reason  string
	}{
		{
			name:    "Setpgid",
			pattern: "Setpgid: true",
			reason:  "osExecer.Run must put the child in its own process group (Setpgid: true) so a negative-pid kill reaches all descendants",
		},
		{
			name:    "cmd.Cancel",
			pattern: "cmd.Cancel",
			reason:  "osExecer.Run must define cmd.Cancel (the process-group kill function) instead of relying on exec.CommandContext's default SIGKILL-to-child-only",
		},
		{
			name:    "cmd.WaitDelay",
			pattern: "cmd.WaitDelay",
			reason:  "osExecer.Run must set cmd.WaitDelay so CombinedOutput unblocks even if grandchildren hold the pipe after cancel fires",
		},
		{
			name:    "execSem",
			pattern: "execSem",
			reason:  "osExecer.Run must acquire execSem before running the command to cap concurrent exec calls at execMaxConcurrent",
		},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(body, c.pattern) {
				t.Errorf("inv-zen-227: %s\n\nPattern %q not found in osExecer.Run body.\n\nReason: %s\n\nActual body:\n%s",
					c.name, c.pattern, c.reason, body)
			}
		})
	}
}

func TestInvZen227ExecSemDefined(t *testing.T) {
	root := repoRoot227(t)
	implFile := filepath.Join(root, "internal", "daemon", "orchestrator_plan5_service_more.go")

	raw, err := os.ReadFile(implFile)
	if err != nil {
		t.Fatalf("inv-zen-227: read %s: %v", implFile, err)
	}
	src := string(raw)

	for _, pattern := range []string{
		"execMaxConcurrent",
		"execSem = semaphore.NewWeighted",
	} {
		if !strings.Contains(src, pattern) {
			t.Errorf("inv-zen-227: package-level %q not found in %s — execSem must be a package-level semaphore, not a local variable", pattern, implFile)
		}
	}
}
