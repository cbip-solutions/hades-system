// tests/compliance/inv_zen_224_health_side_effect_free_test.go
//
// invariant (v0.17.7 / A-7) — health endpoints are side-effect-free.
//
// Root cause of the v0.17.7 hot-fix: HealthResearchMCPUp, HealthGitnexusUp,
// and HealthEventLogWritable previously called CheckEngine.RunCheck on EVERY
// HTTP poll, emitting audit_events_raw rows on every health check and coupling
// the health surface to the 11-check autonomy matrix.
//
// invariant pins the fix: the health service methods MUST perform no inline
// RunCheck call and no per-call DB write (EmitRaw). Background sampling (via
// HealthSampler.Run) may still call SampleEventLogWritable — which does call
// EmitRaw — but ONLY once per cadence, never per HTTP poll.
//
// Two assertions:
// 1. Source guard: HealthResearchMCPUp / HealthGitnexusUp /
// HealthEventLogWritable function bodies in
// internal/daemon/orchestrator_plan5_service_more.go MUST contain no
// "RunCheck(" and no "EmitRaw(" (grep the file, scoped to each function
// body).
// 2. Structural guard: SampleEventLogWritable (the one function ALLOWED to
// call EmitRaw, but only at sampler cadence, not per HTTP poll) MUST be
// absent from the health-handler dispatch path. The handler file
// (internal/daemon/handlers/orchestrator_plan5.go) must NOT call
// SampleEventLogWritable.
//
// Note: the behavioral companion — "zero audit_events_raw rows on 50 health
// polls" — lives in internal/daemon/orchestrator_plan5_health_test.go
// (TestHealthEventLogWritable_NoPerCallAuditWrite) as an in-package test with
// direct DB access. That test and this source-level compliance gate together
// fully cover invariant.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot224(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-224: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-224: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func extractFuncBody(src, funcName string) (string, bool) {
	lines := strings.Split(src, "\n")
	var body strings.Builder
	depth := 0
	inFunc := false
	for _, line := range lines {
		if !inFunc {
			if strings.Contains(line, "func ") && strings.Contains(line, funcName) {
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

func TestInvZen224_HealthSideEffectFree(t *testing.T) {
	root := repoRoot224(t)

	svcFile := filepath.Join(root, "internal", "daemon", "orchestrator_plan5_service_more.go")
	svcBytes, err := os.ReadFile(svcFile)
	if err != nil {
		t.Fatalf("inv-zen-224: reading service file: %v", err)
	}
	svcSrc := string(svcBytes)

	guardedFuncs := []string{
		"HealthResearchMCPUp",
		"HealthEventLogWritable",
	}
	forbidden := []string{"RunCheck(", "EmitRaw("}

	for _, fn := range guardedFuncs {
		body, found := extractFuncBody(svcSrc, fn)
		if !found {
			t.Errorf("inv-zen-224: function %s not found in %s", fn, svcFile)
			continue
		}
		for _, needle := range forbidden {
			if strings.Contains(body, needle) {
				t.Errorf("inv-zen-224 VIOLATED: %s body contains %q (must be side-effect-free)", fn, needle)
			}
		}
	}

	handlerFile := filepath.Join(root, "internal", "daemon", "handlers", "orchestrator_plan5.go")
	handlerBytes, err := os.ReadFile(handlerFile)
	if err != nil {
		t.Fatalf("inv-zen-224: reading handler file: %v", err)
	}
	if strings.Contains(string(handlerBytes), "SampleEventLogWritable") {
		t.Errorf("inv-zen-224 VIOLATED: handler %s calls SampleEventLogWritable (per-cadence probe must not be in HTTP path)", handlerFile)
	}

	if _, found := extractFuncBody(svcSrc, "HealthEventLogWritable"); !found {
		t.Errorf("inv-zen-224: HealthEventLogWritable not found in %s (method may have been renamed or deleted)", svcFile)
	}

	sampleBody, found := extractFuncBody(svcSrc, "SampleEventLogWritable")
	if !found {
		t.Errorf("inv-zen-224: SampleEventLogWritable not found in %s (per-cadence probe missing)", svcFile)
	} else {

		if !strings.Contains(sampleBody, "EmitRaw(") {
			t.Errorf("inv-zen-224: SampleEventLogWritable body does not call EmitRaw( — the per-cadence write probe is missing its implementation")
		}

		if strings.Contains(sampleBody, "RunCheck(") {
			t.Errorf("inv-zen-224 VIOLATED: SampleEventLogWritable body contains RunCheck( — sampler must not invoke the autonomy matrix")
		}
	}
}
