package doctrine

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func ExampleMaxScopeBuiltin() {
	s := MaxScopeBuiltin()
	fmt.Println(s.Name, s.Research.Depth, s.Subprocess.PreWarmPoolSize)

}

func ExampleBuiltin() {
	s, err := Builtin("default")
	if err != nil {
		panic(err)
	}
	fmt.Println(s.Name, s.Research.Depth)

}

func TestMaxScopeBuiltinNonEmpty(t *testing.T) {
	s := MaxScopeBuiltin()
	if s.Name != "max-scope" {
		t.Errorf("Name = %q, want max-scope", s.Name)
	}
	if s.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", s.SchemaVersion)
	}
	if len(s.Research.CadencePerStage) == 0 {
		t.Error("Research.CadencePerStage empty")
	}
	if s.Research.Depth != "deep" {
		t.Errorf("Research.Depth = %q, want deep", s.Research.Depth)
	}
	if len(s.Research.Sources) < 4 {
		t.Errorf("Research.Sources len = %d, want >= 4", len(s.Research.Sources))
	}
	if s.Research.AgenticMaxIter < 5 {
		t.Errorf("Research.AgenticMaxIter = %d, want >= 5", s.Research.AgenticMaxIter)
	}
	if time.Duration(s.Subprocess.PersistentTTLSliding) != 8*time.Hour {
		t.Errorf("Subprocess.PersistentTTLSliding = %v, want 8h",
			time.Duration(s.Subprocess.PersistentTTLSliding))
	}
	if s.Subprocess.PreWarmPoolSize < 3 {
		t.Errorf("Subprocess.PreWarmPoolSize = %d, want >= 3", s.Subprocess.PreWarmPoolSize)
	}
	if len(s.Reviewer.FamilyDisjointPool) < 4 {
		t.Errorf("Reviewer.FamilyDisjointPool len = %d, want >= 4",
			len(s.Reviewer.FamilyDisjointPool))
	}
	if s.Budget.PauseMode != "descriptive" {
		t.Errorf("Budget.PauseMode = %q, want descriptive", s.Budget.PauseMode)
	}
	if s.Budget.AnomalyZThreshold != 4.0 {
		t.Errorf("Budget.AnomalyZThreshold = %f, want 4.0", s.Budget.AnomalyZThreshold)
	}
	if s.Budget.Caps.Project == "" {
		t.Error("Budget.Caps.Project empty")
	}
	if len(s.Budget.Caps.Stage) == 0 {
		t.Error("Budget.Caps.Stage empty")
	}
	if s.Workforce.WritablePathsPolicy == "" {
		t.Error("Workforce.WritablePathsPolicy empty")
	}
	if s.Apply.MergeStrategy == "" {
		t.Error("Apply.MergeStrategy empty")
	}
	if time.Duration(s.Watcher.Cadence) == 0 {
		t.Error("Watcher.Cadence = 0")
	}
}

func TestDefaultBuiltinNonEmpty(t *testing.T) {
	s := DefaultBuiltin()
	if s.Name != "default" {
		t.Errorf("Name = %q, want default", s.Name)
	}
	if s.Research.Depth != "medium" {
		t.Errorf("Research.Depth = %q, want medium", s.Research.Depth)
	}
	if s.Budget.PauseMode != "quiet" {
		t.Errorf("Budget.PauseMode = %q, want quiet", s.Budget.PauseMode)
	}
	if s.Budget.AnomalyZThreshold != 5.0 {
		t.Errorf("Budget.AnomalyZThreshold = %f, want 5.0", s.Budget.AnomalyZThreshold)
	}
	if len(s.Reviewer.FamilyDisjointPool) < 2 {
		t.Errorf("Reviewer.FamilyDisjointPool len = %d, want >= 2",
			len(s.Reviewer.FamilyDisjointPool))
	}
	if s.Workforce.WritablePathsPolicy == "" {
		t.Error("Workforce.WritablePathsPolicy empty")
	}
}

func TestCapaFirewallBuiltinPrivacyLocked(t *testing.T) {
	s := CapaFirewallBuiltin()
	if s.Name != "capa-firewall" {
		t.Errorf("Name = %q, want capa-firewall", s.Name)
	}
	for _, family := range s.Reviewer.FamilyDisjointPool {
		switch family {
		case "local-qwen", "deepseek":

		default:
			t.Errorf("CapaFirewallBuiltin pool contains %q (privacy violation)", family)
		}
	}
	if s.Budget.PauseMode != "fail_loud" {
		t.Errorf("CapaFirewallBuiltin PauseMode = %q, want fail_loud", s.Budget.PauseMode)
	}
}

func TestMaxScopeStrongerThanDefault(t *testing.T) {
	m := MaxScopeBuiltin()
	d := DefaultBuiltin()
	if m.Research.AgenticMaxIter < d.Research.AgenticMaxIter {
		t.Errorf("MaxScope.AgenticMaxIter (%d) < Default (%d)",
			m.Research.AgenticMaxIter, d.Research.AgenticMaxIter)
	}
	if time.Duration(m.Subprocess.PersistentTTLSliding) <
		time.Duration(d.Subprocess.PersistentTTLSliding) {
		t.Errorf("MaxScope.PersistentTTLSliding shorter than Default")
	}
	if len(m.Reviewer.FamilyDisjointPool) < len(d.Reviewer.FamilyDisjointPool) {
		t.Errorf("MaxScope reviewer pool smaller than Default")
	}
}

func TestBuiltinByName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"max-scope", "max-scope"},
		{"default", "default"},
		{"capa-firewall", "capa-firewall"},
	}
	for _, c := range cases {
		got, err := Builtin(c.name)
		if err != nil {
			t.Errorf("Builtin(%q): %v", c.name, err)
			continue
		}
		if got.Name != c.want {
			t.Errorf("Builtin(%q).Name = %q, want %q", c.name, got.Name, c.want)
		}
	}
}

func TestBuiltinUnknown(t *testing.T) {
	_, err := Builtin("frobnicate")
	if err == nil {
		t.Error("Builtin(unknown) returned nil error")
	}
}

func TestBuiltinNamesStable(t *testing.T) {
	names := BuiltinNames()
	want := []string{"max-scope", "default", "capa-firewall"}
	if len(names) != len(want) {
		t.Errorf("BuiltinNames len = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("BuiltinNames[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestBuiltinImmutable(t *testing.T) {
	s1 := MaxScopeBuiltin()
	s1.Research.CadencePerStage["design"] = "TAMPERED"
	s2 := MaxScopeBuiltin()
	if s2.Research.CadencePerStage["design"] == "TAMPERED" {
		t.Error("MaxScopeBuiltin() returns shared map (caller mutation visible)")
	}
}

func TestBuiltinSchemaVersion(t *testing.T) {
	for _, name := range BuiltinNames() {
		s, _ := Builtin(name)
		if s.SchemaVersion != 1 {
			t.Errorf("%s SchemaVersion = %d, want 1", name, s.SchemaVersion)
		}
	}
}

func TestDefaultBuiltinImmutable(t *testing.T) {
	s1 := DefaultBuiltin()
	s1.Research.Sources = append(s1.Research.Sources, "TAMPERED")
	s2 := DefaultBuiltin()
	for _, src := range s2.Research.Sources {
		if src == "TAMPERED" {
			t.Error("DefaultBuiltin() returns shared slice (caller mutation visible)")
		}
	}
}

func TestCapaFirewallBuiltinImmutable(t *testing.T) {
	s1 := CapaFirewallBuiltin()
	s1.Budget.Caps.Stage["design"] = Money("999.00 USD")
	s2 := CapaFirewallBuiltin()
	if s2.Budget.Caps.Stage["design"] == Money("999.00 USD") {
		t.Error("CapaFirewallBuiltin() returns shared map (caller mutation visible)")
	}
}

func TestPlan1DoctrineExtrasFullCoverage(t *testing.T) {
	ms := MaxScope{}
	if extras := ms.PreFlightExtras(); len(extras) == 0 {
		t.Error("MaxScope.PreFlightExtras returned empty slice")
	}
	if extras := ms.PreArchiveExtras(); len(extras) == 0 {
		t.Error("MaxScope.PreArchiveExtras returned empty slice")
	}
	d := Default{}
	if extras := d.PreFlightExtras(); extras != nil {
		t.Errorf("Default.PreFlightExtras = %v, want nil", extras)
	}
	if extras := d.PreArchiveExtras(); extras != nil {
		t.Errorf("Default.PreArchiveExtras = %v, want nil", extras)
	}
}

func TestBuiltinsPopulateAllAxes(t *testing.T) {
	cases := map[string]Schema{
		"max-scope":     MaxScopeBuiltin(),
		"default":       DefaultBuiltin(),
		"capa-firewall": CapaFirewallBuiltin(),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if s.SchemaVersion == 0 {
				t.Errorf("%s: SchemaVersion = 0", name)
			}
			if s.Name == "" {
				t.Errorf("%s: Name empty", name)
			}
			v := reflect.ValueOf(s)
			ty := v.Type()
			for i := 0; i < v.NumField(); i++ {
				field := ty.Field(i)

				if field.Type.Kind() != reflect.Struct {
					continue
				}
				if field.Name == "Future" {
					continue
				}
				if isZeroStruct(v.Field(i)) {
					t.Errorf("%s: nested axis %q is fully zero (drift — constructor missed adding values)",
						name, field.Name)
				}
			}
		})
	}
}

func isZeroStruct(v reflect.Value) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).IsZero() {
			return false
		}
	}
	return true
}
