package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen232NoCommunityDetection(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "caronte", "structure")
	banned := []string{"leiden", "louvain", "modularity", "communitydetect", "community_detect"}
	fset := token.NewFileSet()
	scanned := 0
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		scanned++
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		lower := strings.ToLower(string(src))
		for _, b := range banned {
			if strings.Contains(lower, b) {
				t.Errorf("%s references banned community-detection term %q (inv-zen-232: Leiden/Louvain dropped)", p, b)
			}
		}

		if _, perr := parser.ParseFile(fset, p, src, parser.ImportsOnly); perr != nil {
			return perr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if scanned == 0 {
		t.Fatalf("inv-zen-232 scan covered 0 files under %s — wiring/path error", dir)
	}
}

func TestInvZen232ImportsTopoNotCommunity(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "caronte", "structure")
	fset := token.NewFileSet()
	sawTopo := false
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "gonum.org/v1/gonum/graph/community") {
				t.Errorf("%s imports a gonum community package (inv-zen-232: deterministic only)", p)
			}
			if path == "gonum.org/v1/gonum/graph/topo" {
				sawTopo = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !sawTopo {
		t.Error("structure package must import gonum.org/v1/gonum/graph/topo (k-core + Tarjan)")
	}
}
