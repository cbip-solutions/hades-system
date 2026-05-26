package manifest

import (
	"reflect"
	"testing"
)

func TestCompileCheck_PublicAPIStable(t *testing.T) {
	exports := []any{

		Manifest{}, ZenSwarmSection{}, PlansSection{}, InvariantsSection{},
		DoctrinesSection{}, MCPsSection{}, MCPEntry{}, ADRSection{},
		AutonomousModeSection{}, Provenance{}, ManualField{}, SectionResult{},
		ManualFieldPath{}, AutoSourceMapping{},

		(*Schema)(nil), (*Walker)(nil), (*Regenerator)(nil), (*Differ)(nil),
		(*ManualTracker)(nil), (*AutonomyValidator)(nil), (*RegenerateWatcher)(nil),

		EventPayload{}, PinRequest{}, ChainAnchoredEvent{}, DiffReport{},
		WalkResult{}, WalkerConfig{}, WatcherConfig{},
		Plan9PrereqInputs{}, AutonomyReport{}, AutonomyResult{},
	}
	for _, e := range exports {
		v := reflect.ValueOf(e)
		if !v.IsValid() {
			t.Errorf("invalid exported value: %T", e)
		}
	}
}

func TestCompileCheck_EventAppenderSatisfiable(t *testing.T) {
	var _ EventAppender = (*fakeAppender)(nil)
}

func TestCompileCheck_PrereqProbesSatisfiable(t *testing.T) {
	var _ PrereqProbes = (*fakePrereqProbes)(nil)
}
