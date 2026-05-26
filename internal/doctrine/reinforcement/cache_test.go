package reinforcement_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestEmbeddedTemplateLoadsTestFixture(t *testing.T) {
	e := reinforcement.New("")
	vars := &reinforcement.Vars{
		DoctrineName:       "_test_doctrine",
		ProjectAlias:       "demo-project",
		ProjectID:          "proj-123",
		CurrentStage:       "Build",
		CurrentPhase:       "F",
		TaskKind:           "worker",
		TaskComplexityTier: "medium",
		PlanID:             "plan-8",
		TransverseAxioms:   []string{"no_tech_debt", "no_stubs"},
	}
	out, err := e.Render(&v1.Schema{}, vars)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	wantMarkers := []string{
		"_test_doctrine",
		"demo-project",
		"proj-123",
		"Build",
		"F",
		"worker",
		"medium",
		"plan-8",
		"no_tech_debt",
		"no_stubs",
		"## Worker role",
	}
	for _, m := range wantMarkers {
		if !strings.Contains(out, m) {
			t.Errorf("Render output missing marker %q\nfull output:\n%s", m, out)
		}
	}

	if strings.Contains(out, "## Orchestrator role") {
		t.Error("Render output contains Orchestrator section while TaskKind=worker")
	}
}

func TestCacheReturnsIdenticalOutput(t *testing.T) {
	e := reinforcement.New("")
	vars := &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: "p",
		ProjectID:    "1",
		CurrentStage: "Build",
		TaskKind:     "worker",
		PlanID:       "plan-8",
	}
	first, err := e.Render(&v1.Schema{}, vars)
	if err != nil {
		t.Fatalf("first Render returned error: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := e.Render(&v1.Schema{}, vars)
		if err != nil {
			t.Fatalf("Render iter %d returned error: %v", i, err)
		}
		if got != first {
			t.Errorf("Render iter %d output drift\nfirst: %q\ngot:   %q", i, first, got)
		}
	}
}

func TestCacheConcurrentSafe(t *testing.T) {
	e := reinforcement.New("")
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			vars := &reinforcement.Vars{
				DoctrineName: "_test_doctrine",
				ProjectAlias: "p",
				ProjectID:    "1",
				CurrentStage: "Build",
				TaskKind:     "worker",
				PlanID:       "plan-8",
			}
			if _, err := e.Render(&v1.Schema{}, vars); err != nil {
				t.Errorf("goroutine %d Render error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
}
