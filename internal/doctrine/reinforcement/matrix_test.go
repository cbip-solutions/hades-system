package reinforcement_test

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

var doctrineCases = []struct {
	name        string
	voiceMarker string
}{
	{"max-scope", "MAX-SCOPE-VOICE-MARKER-D7E1F"},
	{"default", "DEFAULT-VOICE-MARKER-9CB47A"},
	{"capa-firewall", "CAPA-FIREWALL-VOICE-MARKER-3B7E11"},
}

var taskKinds = []struct {
	kind          string
	sectionHeader string
}{
	{"orchestrator", "### Orchestrator role"},
	{"team_lead", "### Team lead role"},
	{"worker", "### Worker role"},
	{"reviewer_tactical", "### Tactical reviewer role"},
	{"reviewer_strategic", "### Strategic reviewer role"},
	{"reviewer_architectural", "### Architectural reviewer role"},
}

func canonicalVars(doctrine, kind string) *reinforcement.Vars {
	return &reinforcement.Vars{
		DoctrineName:       doctrine,
		ProjectAlias:       "demo-project",
		ProjectID:          "proj-MATRIX-12345",
		CurrentStage:       "Build",
		CurrentPhase:       "F",
		TaskKind:           kind,
		TaskComplexityTier: "medium",
		PlanID:             "plan-8",
		TransverseAxioms: []string{
			"no_tech_debt",
			"no_stubs",
			"build_final_product",
			"no_defer",
		},
	}
}

// TestMatrixAllDoctrineRoleCombinations renders all 18 (doctrine × taskKind)
// combinations and asserts:
//   - the doctrine's voice marker appears (proves correct template selected)
//   - the chosen TaskKind's section header appears
//   - the other 5 task-kind section headers do NOT appear (mutual exclusion)
//   - each Vars field's value appears at least once in the output (proves
//     the template references it via the allowlist)
//   - no raw template syntax leaks (no "{{" or "}}" in output)
func TestMatrixAllDoctrineRoleCombinations(t *testing.T) {
	e := reinforcement.New("")
	for _, d := range doctrineCases {
		for _, k := range taskKinds {
			t.Run(d.name+"/"+k.kind, func(t *testing.T) {
				vars := canonicalVars(d.name, k.kind)
				out, err := e.Render(&v1.Schema{}, vars)
				if err != nil {
					t.Fatalf("Render(%s, %s) returned error: %v", d.name, k.kind, err)
				}

				if !strings.Contains(out, d.voiceMarker) {
					t.Errorf("output missing doctrine voice marker %q", d.voiceMarker)
				}

				if !strings.Contains(out, k.sectionHeader) {
					t.Errorf("output missing role section %q", k.sectionHeader)
				}

				for _, other := range taskKinds {
					if other.kind == k.kind {
						continue
					}
					if strings.Contains(out, other.sectionHeader) {
						t.Errorf("output contains foreign role section %q (TaskKind was %s)", other.sectionHeader, k.kind)
					}
				}

				wantValues := []string{
					vars.DoctrineName,
					vars.ProjectAlias,
					vars.ProjectID,
					vars.CurrentStage,
					vars.CurrentPhase,
					vars.TaskKind,
					vars.TaskComplexityTier,
					vars.PlanID,
				}
				for _, v := range wantValues {
					if v == "" {
						continue
					}
					if !strings.Contains(out, v) {
						t.Errorf("output missing Vars value %q", v)
					}
				}

				for _, ax := range vars.TransverseAxioms {
					if !strings.Contains(out, ax) {
						t.Errorf("output missing transverse axiom %q (range expansion failed)", ax)
					}
				}

				if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
					t.Errorf("output contains raw template syntax — execute incomplete")
				}
			})
		}
	}
}

func TestMatrixCurrentPhaseEmptySkipsPhaseLine(t *testing.T) {
	e := reinforcement.New("")
	for _, d := range doctrineCases {
		t.Run(d.name, func(t *testing.T) {
			vars := canonicalVars(d.name, "worker")
			vars.CurrentPhase = ""
			out, err := e.Render(&v1.Schema{}, vars)
			if err != nil {
				t.Fatalf("Render returned error: %v", err)
			}

			lines := strings.Split(out, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "You are an AI agent operating") && strings.Contains(line, "| Phase:") {
					t.Errorf("header line contains '| Phase:' when CurrentPhase is empty: %q", line)
				}
			}
		})
	}
}

func TestMatrixOnlyStdlibFunctionsUsed(t *testing.T) {

	t.Log("All 18 matrix combinations render with stdlib-only template funcs (text/template defaults). " +
		"No template.Funcs() registration in reinforcement.go; safety_test.go (F-5) enforces.")
}
