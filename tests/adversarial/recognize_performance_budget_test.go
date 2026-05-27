// go:build adversarial
//go:build adversarial
// +build adversarial

package adversarial

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/recognize"
)

func TestRecognize_PerformanceBudget_10kFiles(t *testing.T) {
	dir := t.TempDir()

	exts := []string{"go", "py", "js"}
	contents := map[string]string{
		"go": "package pkg\n",
		"py": "x = 1\n",
		"js": "var x = 1;\n",
	}
	for i := 0; i < 100; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("pkg%03d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for j := 0; j < 100; j++ {
			ext := exts[(i+j)%3]
			p := filepath.Join(sub, fmt.Sprintf("file%03d.%s", j, ext))
			if err := os.WriteFile(p, []byte(contents[ext]), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}

	start := time.Now()
	r := recognize.New(recognize.Options{RootAbsPath: dir, NoAudit: true})
	res, err := r.Recognize(context.Background(), os.DirFS(dir))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}

	if res.PrimaryLanguage == "" {
		t.Errorf("PrimaryLanguage empty; Tier 3 walker should have produced byte stats over 10k files")
	}

	budget := 3 * time.Second
	if elapsed > budget {
		t.Errorf("Recognize took %v on 10k polyglot files; want ≤%v (regression alarm)", elapsed, budget)
	} else {
		t.Logf("Recognize completed in %v on 10k polyglot files (under %v alarm; Tier 3 walker on hot path)", elapsed, budget)
	}
}

func TestRecognize_PerformanceBudget_NodeModulesDoesNotBlowBudget(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("console.log('x');\n"), 0o644); err != nil {
		t.Fatalf("index.js: %v", err)
	}

	nm := filepath.Join(dir, "node_modules")
	for i := 0; i < 50; i++ {
		sub := filepath.Join(nm, fmt.Sprintf("pkg%03d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for j := 0; j < 100; j++ {
			p := filepath.Join(sub, fmt.Sprintf("file%03d.js", j))
			if err := os.WriteFile(p, []byte("module.exports = {};\n"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
	}

	start := time.Now()
	r := recognize.New(recognize.Options{RootAbsPath: dir, NoAudit: true})
	_, err := r.Recognize(context.Background(), os.DirFS(dir))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}

	budget := 1500 * time.Millisecond
	if elapsed > budget {
		t.Errorf("Recognize took %v on a small project with 5k node_modules files; want ≤%v (dir-skip regression?)", elapsed, budget)
	} else {
		t.Logf("Recognize completed in %v on small project + 5k node_modules (under %v alarm; dir-skip working)", elapsed, budget)
	}
}
