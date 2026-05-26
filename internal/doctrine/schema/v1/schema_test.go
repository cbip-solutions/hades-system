package v1_test

import (
	"reflect"
	"strings"
	"testing"

	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestSchemaSectionsPresent(t *testing.T) {
	wantSections := []string{
		"SchemaVersion", "DoctrineVersion", "AutoUpgrade",
		"Workforce", "HRA", "Research", "Gates", "Review", "Transverse",
		"Autonomy", "Merge", "Caronte", "Notifications", "ZenDayCadence",
		"Quota", "Tmux", "Scheduling", "WFQ", "Knowledge",
	}
	rt := reflect.TypeOf(v1.Schema{})
	got := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		got[rt.Field(i).Name] = true
	}
	for _, want := range wantSections {
		if !got[want] {
			t.Errorf("Schema missing field %q (Plan 5/6/7 knob unreachable)", want)
		}
	}
}

func TestEveryLeafFieldHasTOMLAndTightenTag(t *testing.T) {
	walkLeaves(t, reflect.TypeOf(v1.Schema{}), "")
}

func walkLeaves(t *testing.T, rt reflect.Type, path string) {
	t.Helper()
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		fpath := path + "." + f.Name
		if path == "" {
			fpath = f.Name
		}
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {

			walkLeaves(t, ft, fpath)
			continue
		}

		if f.Tag.Get("toml") == "" {
			t.Errorf("leaf %q missing `toml:` tag", fpath)
		}
		if f.Tag.Get("tighten") == "" {
			t.Errorf("leaf %q missing `tighten:` tag", fpath)
		}
	}
}

func TestTightenTagDirectionsAreKnown(t *testing.T) {
	known := map[string]bool{
		"-":             true,
		"decrease":      true,
		"increase":      true,
		"truth":         true,
		"add-only":      true,
		"bidirectional": true,
	}
	checkDir := func(t *testing.T, fpath, raw string) {
		t.Helper()

		base := raw
		if i := strings.IndexByte(raw, ','); i >= 0 {
			base = raw[:i]
		}
		if strings.HasPrefix(base, "rank:") {

			vals := strings.TrimPrefix(base, "rank:")
			if vals == "" || strings.HasPrefix(vals, ",") {
				t.Errorf("leaf %q: rank tighten tag has empty value list", fpath)
			}
			return
		}
		if !known[base] {
			t.Errorf("leaf %q: unknown tighten direction %q (allowed: -|decrease|increase|truth|add-only|bidirectional|rank:...)", fpath, raw)
		}
	}
	walkLeavesWithTag(t, reflect.TypeOf(v1.Schema{}), "", checkDir)
}

func walkLeavesWithTag(t *testing.T, rt reflect.Type, path string, fn func(*testing.T, string, string)) {
	t.Helper()
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		fpath := path + "." + f.Name
		if path == "" {
			fpath = f.Name
		}
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			walkLeavesWithTag(t, ft, fpath, fn)
			continue
		}
		if tag := f.Tag.Get("tighten"); tag != "" {
			fn(t, fpath, tag)
		}
	}
}

func TestCaronteConfigReplacesGitnexusNoVendorMode(t *testing.T) {
	var o v1.Schema
	rt := reflect.TypeOf(o)
	f, ok := rt.FieldByName("Caronte")
	if !ok {
		t.Fatalf("Schema has no Caronte field (Plan 19 renames Gitnexus->Caronte)")
	}
	if tag := f.Tag.Get("toml"); tag != "caronte" {
		t.Errorf("Caronte toml tag = %q; want caronte", tag)
	}
	if _, exists := rt.FieldByName("Gitnexus"); exists {
		t.Errorf("Schema still has a Gitnexus field; Plan 19 renames it to Caronte")
	}

	ct := reflect.TypeOf(o.Caronte)
	if _, exists := ct.FieldByName("VendorMode"); exists {
		t.Errorf("CaronteConfig still has VendorMode; Plan 19 drops it (Caronte IS the engine)")
	}
}
