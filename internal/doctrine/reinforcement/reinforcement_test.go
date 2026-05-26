package reinforcement_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"text/template"

	derrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestEngineNew(t *testing.T) {
	e := reinforcement.New("")
	if e == nil {
		t.Fatal("New(\"\") returned nil engine")
	}
	e2 := reinforcement.New("/some/non/existent/path")
	if e2 == nil {
		t.Fatal("New(non-existent) returned nil engine")
	}
}

func TestVarsAllowlistShape(t *testing.T) {
	wantFields := []string{
		"DoctrineName",
		"ProjectAlias",
		"ProjectID",
		"CurrentStage",
		"CurrentPhase",
		"TaskKind",
		"TaskComplexityTier",
		"PlanID",
		"TransverseAxioms",
	}
	rt := reflect.TypeOf(reinforcement.Vars{})
	if rt.NumField() != len(wantFields) {
		t.Errorf("Vars has %d fields; allowlist expects %d", rt.NumField(), len(wantFields))
	}
	got := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		got[rt.Field(i).Name] = true
	}
	for _, w := range wantFields {
		if !got[w] {
			t.Errorf("Vars missing required field %q", w)
		}
	}

	for name := range got {
		ok := false
		for _, w := range wantFields {
			if w == name {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("Vars has unexpected extra field %q (extends allowlist without test update)", name)
		}
	}
	// Zero-method invariant — text/template exposes exported methods on the
	// data value as template-callable identifiers. Vars MUST have zero
	// methods so the allowlist truly equals the field set. Pointer receiver
	// methods on *Vars also count via reflect.PtrTo (Render passes *Vars).
	if n := rt.NumMethod(); n != 0 {
		t.Errorf("Vars MUST have zero methods (text/template exposes them as template-callable); found %d on Vars", n)
	}
	if n := reflect.PtrTo(rt).NumMethod(); n != 0 {
		t.Errorf("*Vars MUST have zero methods (Render passes *Vars; pointer-receiver methods would also be template-callable); found %d on *Vars", n)
	}
}

func TestRenderHappyPath(t *testing.T) {
	e := reinforcement.New("")
	tmpl := template.Must(template.New("test").Parse(
		"doctrine={{.DoctrineName}} kind={{.TaskKind}} project={{.ProjectAlias}}",
	))
	reinforcement.InjectTemplateForTest(e, "test", tmpl)

	schema := &v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.0.0",
	}
	vars := &reinforcement.Vars{
		DoctrineName: "test",
		ProjectAlias: "demo",
		TaskKind:     "worker",
	}
	out, err := e.Render(schema, vars)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	want := "doctrine=test kind=worker project=demo"
	if out != want {
		t.Errorf("Render = %q; want %q", out, want)
	}
}

func TestRenderUndefinedFieldReturnsTemplateExecError(t *testing.T) {
	e := reinforcement.New("")

	tmpl, err := template.New("test").Option("missingkey=error").Parse(
		"unsafe={{.ShellOut}}",
	)
	if err != nil {
		t.Fatalf("template parse failed: %v", err)
	}
	reinforcement.InjectTemplateForTest(e, "test", tmpl)

	_, err = e.Render(&v1.Schema{}, &reinforcement.Vars{DoctrineName: "test"})
	if err == nil {
		t.Fatal("Render with undefined field returned nil error; want ErrReinforcementTemplateExec")
	}
	if !errors.Is(err, derrors.ErrReinforcementTemplateExec) {
		t.Errorf("Render error chain missing ErrReinforcementTemplateExec; got %v", err)
	}
}

func TestRenderMissingTemplateReturnsErrTemplateNotFound(t *testing.T) {
	e := reinforcement.New("")
	_, err := e.Render(&v1.Schema{}, &reinforcement.Vars{DoctrineName: "nonexistent-doctrine"})
	if err == nil {
		t.Fatal("Render with unknown doctrine returned nil error; want ErrTemplateNotFound")
	}
	if !errors.Is(err, derrors.ErrTemplateNotFound) {
		t.Errorf("Render error chain missing ErrTemplateNotFound; got %v", err)
	}
}

func TestRenderNilSchemaIsAccepted(t *testing.T) {
	e := reinforcement.New("")
	tmpl := template.Must(template.New("test").Parse("ok"))
	reinforcement.InjectTemplateForTest(e, "test", tmpl)

	out, err := e.Render(nil, &reinforcement.Vars{DoctrineName: "test"})
	if err != nil {
		t.Fatalf("Render(nil schema) returned error: %v", err)
	}
	if out != "ok" {
		t.Errorf("Render(nil schema) = %q; want %q", out, "ok")
	}
}

func TestRenderNilVarsReturnsError(t *testing.T) {
	e := reinforcement.New("")
	tmpl := template.Must(template.New("test").Parse("ok"))
	reinforcement.InjectTemplateForTest(e, "test", tmpl)

	_, err := e.Render(&v1.Schema{}, nil)
	if err == nil {
		t.Fatal("Render(nil vars) returned nil error; want explicit error")
	}
	if !strings.Contains(err.Error(), "vars") {
		t.Errorf("Render(nil vars) error %q does not mention 'vars'", err.Error())
	}
}
