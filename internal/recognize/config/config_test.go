package config

import (
	"embed"
	"io/fs"
	"testing"
	"testing/fstest"
)

//go:embed testdata
var testdataFS embed.FS

func subFS(t *testing.T, subdir string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(testdataFS, "testdata/"+subdir)
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	return sub
}

func TestDetect_NextJS(t *testing.T) {
	fsys := subFS(t, "next")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("Detect err: %v", err)
	}
	var found *FrameworkConfig
	for i := range fcs {
		if fcs[i].Framework == "next.js" {
			found = &fcs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Detect did not return next.js framework; got %+v", fcs)
	}
	if found.Confidence != 1.0 {
		t.Errorf("Confidence = %v; want 1.0 (config+dep both present)", found.Confidence)
	}
}

func TestDetect_ViteReact(t *testing.T) {
	fsys := subFS(t, "vite-react")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "vite-react") {
		t.Errorf("missing vite-react; got %v", frameworkNames(fcs))
	}
	if containsFramework(fcs, "vite-vue") || containsFramework(fcs, "vite-svelte") {
		t.Errorf("unexpected vite-vue or vite-svelte in %v", frameworkNames(fcs))
	}
}

func TestDetect_ViteVue(t *testing.T) {
	fsys := subFS(t, "vite-vue")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "vite-vue") {
		t.Errorf("missing vite-vue; got %v", frameworkNames(fcs))
	}
}

func TestDetect_ViteSvelte(t *testing.T) {
	fsys := subFS(t, "vite-svelte")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "vite-svelte") {
		t.Errorf("missing vite-svelte; got %v", frameworkNames(fcs))
	}
}

func TestDetect_ViteAstro(t *testing.T) {
	fsys := subFS(t, "vite-astro")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if !containsFramework(fcs, "astro") && !containsFramework(fcs, "vite-astro") {
		t.Errorf("missing astro or vite-astro; got %v", frameworkNames(fcs))
	}
}

func TestDetect_Nuxt(t *testing.T) {
	fsys := subFS(t, "nuxt")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "nuxt") {
		t.Errorf("missing nuxt; got %v", frameworkNames(fcs))
	}
}

func TestDetect_Astro_Canonical(t *testing.T) {
	fsys := subFS(t, "astro")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "astro") {
		t.Errorf("missing astro; got %v", frameworkNames(fcs))
	}
}

func TestDetect_Remix(t *testing.T) {
	fsys := subFS(t, "remix")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "remix") {
		t.Errorf("missing remix; got %v", frameworkNames(fcs))
	}
}

func TestDetect_SvelteKit(t *testing.T) {
	fsys := subFS(t, "svelte")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "sveltekit") {
		t.Errorf("missing sveltekit; got %v", frameworkNames(fcs))
	}
}

func TestDetect_Angular(t *testing.T) {
	fsys := subFS(t, "angular")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "angular") {
		t.Errorf("missing angular; got %v", frameworkNames(fcs))
	}
}

func TestDetect_Gatsby(t *testing.T) {
	fsys := subFS(t, "gatsby")
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !containsFramework(fcs, "gatsby") {
		t.Errorf("missing gatsby; got %v", frameworkNames(fcs))
	}
}

func TestDetect_ViteVanillaNoDeps(t *testing.T) {
	fsys := fstest.MapFS{
		"vite.config.ts": &fstest.MapFile{Data: []byte(`import { defineConfig } from 'vite'; export default defineConfig({});`)},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"vanilla","devDependencies":{"vite":"^5.0.0"}}`)},
	}
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(fcs) > 0 {
		for _, fc := range fcs {
			if fc.Confidence > 0.85 {
				t.Errorf("Confidence = %v for vanilla vite + no framework deps; want ≤0.85", fc.Confidence)
			}
		}
	}
}

func TestDetect_NoFrameworkConfig(t *testing.T) {
	fsys := fstest.MapFS{
		"main.go": &fstest.MapFile{Data: []byte("package main\n")},
	}
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fcs) != 0 {
		t.Errorf("Detect returned %d framework(s); want 0", len(fcs))
	}
}

func TestDetect_ConfigOnlyNoDeps_LowConfidence(t *testing.T) {
	fsys := fstest.MapFS{
		"next.config.js": &fstest.MapFile{Data: []byte("module.exports = {};")},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{}}`)},
	}
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var found *FrameworkConfig
	for i := range fcs {
		if fcs[i].Framework == "next.js" {
			found = &fcs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("next.js missing; want low-confidence row")
	}
	if found.Confidence != 0.7 {
		t.Errorf("Confidence = %v; want 0.7", found.Confidence)
	}
}

func TestDetect_ViteMultipleFrameworkDeps_Ambiguous(t *testing.T) {
	fsys := fstest.MapFS{
		"vite.config.ts": &fstest.MapFile{Data: []byte(`export default {};`)},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"react":"^18","react-dom":"^18","vue":"^3"}}`)},
	}
	fcs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	var rConf, vConf float64
	for _, fc := range fcs {
		if fc.Framework == "vite-react" {
			rConf = fc.Confidence
		}
		if fc.Framework == "vite-vue" {
			vConf = fc.Confidence
		}
	}
	if rConf == 0 || vConf == 0 {
		t.Errorf("expected both vite-react + vite-vue; got %v", frameworkNames(fcs))
	}
	if rConf > 0.85 || vConf > 0.85 {
		t.Errorf("ambiguous confidence should be ≤0.85; got vite-react=%v vite-vue=%v", rConf, vConf)
	}
}

func TestDetect_MalformedPackageJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"next.config.js": &fstest.MapFile{Data: []byte("module.exports = {};")},
		"package.json":   &fstest.MapFile{Data: []byte("not json at all")},
	}
	fcs, err := Detect(fsys)

	if err != nil {
		t.Fatalf("Detect err: %v (should tolerate malformed package.json)", err)
	}
	if !containsFramework(fcs, "next.js") {
		t.Errorf("missing next.js (config-only); got %v", frameworkNames(fcs))
	}
}

func containsFramework(fcs []FrameworkConfig, name string) bool {
	for _, fc := range fcs {
		if fc.Framework == name {
			return true
		}
	}
	return false
}

func frameworkNames(fcs []FrameworkConfig) []string {
	out := make([]string, 0, len(fcs))
	for _, fc := range fcs {
		out = append(out, fc.Framework)
	}
	return out
}
