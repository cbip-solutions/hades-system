package reinforcement_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	derrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func writeOverride(t *testing.T, overrideDir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("mkdir overrideDir: %v", err)
	}
	path := filepath.Join(overrideDir, name+".system-prompt.md.tmpl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write override file: %v", err)
	}
	return path
}

func TestOverrideSupersedesEmbedded(t *testing.T) {
	overrideDir := t.TempDir()
	const overrideMarker = "OPERATOR_OVERRIDE_MARKER_8FAE2C"
	writeOverride(t, overrideDir, "_test_doctrine",
		"# operator override\n"+overrideMarker+" project={{.ProjectAlias}}\n",
	)

	e := reinforcement.New(overrideDir)
	out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: "demo",
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !strings.Contains(out, overrideMarker) {
		t.Errorf("Render output missing override marker %q\noutput:\n%s", overrideMarker, out)
	}
	// Embedded fixture's marker MUST NOT appear when override is in effect.
	if strings.Contains(out, "## Worker role") {
		t.Errorf("Render output contains embedded fixture content while override should be in effect:\n%s", out)
	}
}

func TestOverrideAbsentFallsThroughToEmbedded(t *testing.T) {
	overrideDir := t.TempDir()

	writeOverride(t, overrideDir, "some-other-doctrine", "# unrelated override\n")

	e := reinforcement.New(overrideDir)
	out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName:     "_test_doctrine",
		ProjectAlias:     "demo",
		ProjectID:        "1",
		CurrentStage:     "Build",
		TaskKind:         "worker",
		PlanID:           "plan-8",
		TransverseAxioms: []string{"no_tech_debt"},
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(out, "## Worker role") {
		t.Errorf("Render output missing embedded fixture's '## Worker role' section\noutput:\n%s", out)
	}
}

func TestOverrideDirEmptyDisablesOverrideLookup(t *testing.T) {
	e := reinforcement.New("")
	out, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName:     "_test_doctrine",
		ProjectAlias:     "demo",
		ProjectID:        "1",
		CurrentStage:     "Build",
		TaskKind:         "worker",
		PlanID:           "plan-8",
		TransverseAxioms: []string{"no_tech_debt"},
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(out, "## Worker role") {
		t.Errorf("Render output missing embedded fixture's worker section\noutput:\n%s", out)
	}
}

func TestOverrideMalformedSurfacesParseError(t *testing.T) {
	overrideDir := t.TempDir()
	writeOverride(t, overrideDir, "_test_doctrine",
		"# operator override\nbroken={{.ProjectAlias",
	)
	e := reinforcement.New(overrideDir)
	_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: "demo",
	})
	if err == nil {
		t.Fatal("Render with malformed override returned nil error; want ErrReinforcementTemplateExec")
	}
	if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
		t.Errorf("Render error chain missing ErrReinforcementTemplateExec; got %v", err)
	}
}

func TestOverrideUndefinedFieldRejected(t *testing.T) {
	overrideDir := t.TempDir()
	writeOverride(t, overrideDir, "_test_doctrine",
		"unsafe={{.ShellOut}}",
	)
	e := reinforcement.New(overrideDir)
	_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
		ProjectAlias: "demo",
	})
	if err == nil {
		t.Fatal("Render with override referring to non-Vars field returned nil error; want ErrReinforcementTemplateExec")
	}
	if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
		t.Errorf("Render error chain missing ErrReinforcementTemplateExec; got %v", err)
	}
}

func TestOverrideCacheIdentity(t *testing.T) {
	overrideDir := t.TempDir()
	const marker = "CACHED_OVERRIDE_BODY"
	overridePath := writeOverride(t, overrideDir, "_test_doctrine",
		"# cached\n"+marker+"\n",
	)

	e := reinforcement.New(overrideDir)
	out1, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
	})
	if err != nil {
		t.Fatalf("first Render returned error: %v", err)
	}
	if !strings.Contains(out1, marker) {
		t.Fatalf("first Render output missing marker; output:\n%s", out1)
	}

	if err := os.Remove(overridePath); err != nil {
		t.Fatalf("rm override file: %v", err)
	}

	out2, err := e.Render(&v1.Schema{}, &reinforcement.Vars{
		DoctrineName: "_test_doctrine",
	})
	if err != nil {
		t.Fatalf("second Render after rm returned error: %v", err)
	}
	if !strings.Contains(out2, marker) {
		t.Errorf("second Render after rm missing marker (cache should retain); output:\n%s", out2)
	}
}
