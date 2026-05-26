package coordinated_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
)

func TestDispatchModeEnum(t *testing.T) {
	if coordinated.ModeAutonomy != "autonomy" {
		t.Errorf("ModeAutonomy: want %q, got %q", "autonomy", coordinated.ModeAutonomy)
	}
	if coordinated.ModeSurface != "surface" {
		t.Errorf("ModeSurface: want %q, got %q", "surface", coordinated.ModeSurface)
	}
}

func TestConsumerRefFieldSetReflect(t *testing.T) {
	want := map[string]string{
		"Repo":   "string",
		"CallID": "string",
		"NodeID": "string",
		"File":   "string",
		"Line":   "int",
	}
	got := fieldMap(reflect.TypeOf(coordinated.ConsumerRef{}))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConsumerRef fields drift:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestContractBreakageFieldSet(t *testing.T) {
	want := map[string]string{
		"Change":            "store.BreakingChange",
		"AffectedConsumers": "[]coordinated.ConsumerRef",
		"Workspace":         "*store.Workspace",
		"LoreAttribution":   "*coordinated.LoreAttribution",
	}
	got := fieldMap(reflect.TypeOf(coordinated.ContractBreakage{}))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ContractBreakage fields drift:\n got=%+v\nwant=%+v", got, want)
	}

	var b coordinated.ContractBreakage
	b.LoreAttribution = &coordinated.LoreAttribution{}
	if reflect.TypeOf(b.LoreAttribution).Elem().PkgPath() !=
		"github.com/cbip-solutions/hades-system/internal/caronte/coordinated" {
		t.Errorf("LoreAttribution must come from internal/caronte/coordinated; got %s",
			reflect.TypeOf(b.LoreAttribution).Elem().PkgPath())
	}
}

func TestDispatchResultFieldSet(t *testing.T) {
	want := map[string]string{
		"Mode":            "coordinated.DispatchMode",
		"DispatchedRepos": "[]string",
		"SurfaceMessage":  "string",
		"AuditID":         "tessera.LeafID",
	}
	got := fieldMap(reflect.TypeOf(coordinated.DispatchResult{}))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DispatchResult fields drift:\n got=%+v\nwant=%+v", got, want)
	}

	if reflect.TypeOf(tessera.LeafID("")).Kind() != reflect.String {
		t.Errorf("tessera.LeafID kind: want string, got %v", reflect.TypeOf(tessera.LeafID("")).Kind())
	}
}

func TestDispatchDecisionFieldSet(t *testing.T) {
	want := map[string]string{
		"ChangeID":        "string",
		"Mode":            "coordinated.DispatchMode",
		"DispatchedRepos": "[]string",
		"AuditID":         "tessera.LeafID",
		"DecidedAt":       "time.Time",
	}
	got := fieldMap(reflect.TypeOf(coordinated.DispatchDecision{}))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DispatchDecision fields drift:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestCoordinatorInterface(t *testing.T) {
	typ := reflect.TypeOf((*coordinated.Coordinator)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("Coordinator kind: want interface, got %v", typ.Kind())
	}
	if typ.NumMethod() != 1 {
		t.Fatalf("Coordinator: want 1 method, got %d", typ.NumMethod())
	}
	m := typ.Method(0)
	if m.Name != "Dispatch" {
		t.Errorf("method[0].Name: want Dispatch, got %q", m.Name)
	}

	if m.Type.NumIn() != 2 || m.Type.NumOut() != 2 {
		t.Fatalf("Dispatch shape: want 2 in / 2 out, got %d/%d", m.Type.NumIn(), m.Type.NumOut())
	}
	ctxIface := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !m.Type.In(0).Implements(ctxIface) {
		t.Errorf("Dispatch in[0]: want context.Context, got %v", m.Type.In(0))
	}
	if m.Type.In(1) != reflect.TypeOf(coordinated.ContractBreakage{}) {
		t.Errorf("Dispatch in[1]: want ContractBreakage, got %v", m.Type.In(1))
	}
	if m.Type.Out(0) != reflect.TypeOf(coordinated.DispatchResult{}) {
		t.Errorf("Dispatch out[0]: want DispatchResult, got %v", m.Type.Out(0))
	}
	errIface := reflect.TypeOf((*error)(nil)).Elem()
	if !m.Type.Out(1).Implements(errIface) {
		t.Errorf("Dispatch out[1]: want error, got %v", m.Type.Out(1))
	}
}

func fieldMap(t reflect.Type) map[string]string {
	out := make(map[string]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		out[f.Name] = shortType(f.Type)
	}
	return out
}

func shortType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + shortType(t.Elem())
	case reflect.Slice:
		return "[]" + shortType(t.Elem())
	default:
		name := t.Name()
		if pkg := t.PkgPath(); pkg != "" {

			parts := splitLast(pkg, "/")
			return parts + "." + name
		}
		return name
	}
}

func splitLast(s, sep string) string {
	idx := -1
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
		}
	}
	if idx < 0 {
		return s
	}
	return s[idx+len(sep):]
}
