package integration

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestRecognize_GoModule_FullStack(t *testing.T) {
	dir := t.TempDir()
	mustWriteRecognize(t, dir, "go.mod", "module example\n\ngo 1.22\n")
	mustWriteRecognize(t, dir, "main.go", "package main\nfunc main(){}\n")
	mustWriteRecognize(t, dir, "README.md", "# example\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if res.SchemaVersion != recognize.SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", res.SchemaVersion, recognize.SchemaVersion)
	}
	if res.PrimaryLanguage != "Go" {
		t.Errorf("PrimaryLanguage = %q; want Go", res.PrimaryLanguage)
	}
	if res.PrimaryConfidence < 0.8 {
		t.Errorf("PrimaryConfidence = %v; want >=0.8 (Tier 1 should fire)", res.PrimaryConfidence)
	}
}

func TestRecognize_PolyglotMonorepo(t *testing.T) {
	dir := t.TempDir()
	mustWriteRecognize(t, dir, "go.mod", "module monorepo\n\ngo 1.22\n")
	mustWriteRecognize(t, dir, "package.json", `{"name":"web","dependencies":{"react":"^18","react-dom":"^18"}}`)
	mustWriteRecognize(t, dir, "main.go", "package main\nfunc main(){}\nfunc helper(){}\n")
	mustWriteRecognize(t, dir, "app.tsx", "export const App = () => <div>hi</div>;\n")
	mustWriteRecognize(t, dir, "pnpm-workspace.yaml", "packages:\n  - apps/*\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(res.Ecosystems) < 2 {
		t.Errorf("Ecosystems count = %d; want >=2 (Go + JavaScript)", len(res.Ecosystems))
	}
	if res.Monorepo == nil {
		t.Error("Monorepo nil; want pnpm-detected")
	} else if res.Monorepo.Tool != "pnpm" {
		t.Errorf("Monorepo.Tool = %q; want pnpm", res.Monorepo.Tool)
	}
}

func TestRecognize_ReactWithVite(t *testing.T) {
	dir := t.TempDir()
	mustWriteRecognize(t, dir, "vite.config.ts", "import { defineConfig } from 'vite'; export default defineConfig({});\n")
	mustWriteRecognize(t, dir, "package.json", `{"name":"app","dependencies":{"react":"^18","react-dom":"^18"},"devDependencies":{"vite":"^5"}}`)
	mustWriteRecognize(t, dir, "src/App.tsx", "export default () => <div/>;\n")

	r := recognize.New(recognize.Options{
		RootAbsPath: dir,
		NoAudit:     true,
	})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	found := false
	for _, f := range res.Frameworks {
		if f.Framework == "vite-react" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Frameworks missing vite-react; got %v", res.Frameworks)
	}
}

func TestRecognize_GoldenJSONSchema(t *testing.T) {
	goldenBytes, err := os.ReadFile("recognize_golden_output.json")
	if err != nil {
		t.Skipf("golden file missing (initial scaffold): %v", err)
	}
	var golden map[string]any
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("golden Unmarshal: %v", err)
	}

	for _, key := range []string{"schemaVersion", "primaryLanguage", "primaryConfidence", "maturity"} {
		if _, ok := golden[key]; !ok {
			t.Errorf("golden missing key %q (schema regression)", key)
		}
	}
}

func mustWriteRecognize(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

var _ fs.FS = (os.DirFS("."))
