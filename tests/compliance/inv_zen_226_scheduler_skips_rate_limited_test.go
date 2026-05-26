package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot226(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-226: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-226: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen226_SchedulerSkipsRateLimited(t *testing.T) {
	root := repoRoot226(t)

	schedulerFile := filepath.Join(root, "internal", "daemon", "orchestrator", "recovery_scheduler.go")
	schedulerBytes, err := os.ReadFile(schedulerFile)
	if err != nil {
		t.Fatalf("inv-zen-226: reading scheduler file: %v", err)
	}
	schedulerSrc := string(schedulerBytes)

	if !strings.Contains(schedulerSrc, "StateRateLimited") {
		t.Error("inv-zen-226 VIOLATED: recovery_scheduler.go does not reference StateRateLimited (skip guard missing)")
	}

	runBody, found := extractFuncBody226(schedulerSrc, "func (rs *RecoveryScheduler) Run")
	if !found {
		t.Fatal("inv-zen-226: RecoveryScheduler.Run not found in recovery_scheduler.go")
	}
	if !strings.Contains(runBody, "StateRateLimited") {
		t.Error("inv-zen-226 VIOLATED: RecoveryScheduler.Run does not reference StateRateLimited in its tick body")
	}
	if !strings.Contains(runBody, "continue") {
		t.Error("inv-zen-226 VIOLATED: RecoveryScheduler.Run tick body has no continue (StateRateLimited skip path missing)")
	}

	breakerFile := filepath.Join(root, "internal", "daemon", "orchestrator", "circuit_breaker.go")
	breakerBytes, err := os.ReadFile(breakerFile)
	if err != nil {
		t.Fatalf("inv-zen-226: reading circuit_breaker file: %v", err)
	}
	breakerSrc := string(breakerBytes)

	attemptBody, found := extractFuncBody226(breakerSrc, "func (cb *CircuitBreaker) AttemptRecovery")
	if !found {
		t.Fatal("inv-zen-226: CircuitBreaker.AttemptRecovery not found in circuit_breaker.go")
	}
	if !strings.Contains(attemptBody, "StateRateLimited") {
		t.Error("inv-zen-226 VIOLATED: AttemptRecovery does not handle StateRateLimited (belt-and-suspenders early-return missing)")
	}

	if !strings.Contains(attemptBody, "return false") {
		t.Error("inv-zen-226 VIOLATED: AttemptRecovery has no 'return false' (belt-and-suspenders exit missing)")
	}
}

func extractFuncBody226(src, prefix string) (string, bool) {
	lines := strings.Split(src, "\n")
	var body strings.Builder
	depth := 0
	inFunc := false
	for _, line := range lines {
		if !inFunc {
			if strings.Contains(line, prefix) {
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
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		body.WriteString(line)
		body.WriteByte('\n')
		if depth == 0 {
			break
		}
	}
	return body.String(), inFunc
}
