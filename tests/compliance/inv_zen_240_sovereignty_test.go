package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen240NoGitnexusInGoMod(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if strings.Contains(strings.ToLower(string(b)), "gitnexus") {
		t.Errorf("inv-zen-240 violated: go.mod references gitnexus (sovereignty: no gitnexus dependency)")
	}
}

func TestInvZen240NoGitnexusInLicenseOrBrewOrHermes(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		rel    string
		forbid string
	}{
		{"THIRD_PARTY_LICENSES.md", "gitnexus"},
		{"plugin/hades/hermes-config-snippet.yaml", "gitnexus"},
	}
	for _, c := range cases {
		b, err := os.ReadFile(filepath.Join(root, c.rel))
		if err != nil {
			t.Fatalf("read %s: %v", c.rel, err)
		}
		if strings.Contains(strings.ToLower(string(b)), c.forbid) {
			t.Errorf("inv-zen-240 violated: %s references %q (sovereignty cutover incomplete)", c.rel, c.forbid)
		}
	}

}

func TestInvZen240NoGitnexusBinarySpawn(t *testing.T) {
	root := repoRoot(t)
	scanDirs := []string{"internal", "cmd"}
	fset := token.NewFileSet()
	for _, d := range scanDirs {
		base := filepath.Join(root, d)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Command" {
					return true
				}

				if len(call.Args) == 0 {
					return true
				}
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if strings.Contains(strings.ToLower(lit.Value), "gitnexus") {
						rel, _ := filepath.Rel(root, path)
						t.Errorf("inv-zen-240 violated: %s spawns a gitnexus process (%s)", rel, lit.Value)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", d, err)
		}
	}
}

func TestInvZen240KnownSubsystemsHasCaronteNotGitnexus(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "internal/daemon/mcpgateway/types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, `"caronte"`) {
		t.Errorf("inv-zen-240: KnownSubsystems() source lacks \"caronte\"")
	}

	if strings.Contains(src, `"gitnexus"`) {
		t.Errorf("inv-zen-240: internal/daemon/mcpgateway/types.go still has a \"gitnexus\" segment literal")
	}
}

func TestInvZen240AugmentAndCitationUseCaronteWireNames(t *testing.T) {
	root := repoRoot(t)

	pb, err := os.ReadFile(filepath.Join(root, "internal/augment/pipeline.go"))
	if err != nil {
		t.Fatalf("read pipeline.go: %v", err)
	}
	pipeline := string(pb)
	if strings.Contains(pipeline, "mcp_zen-swarm_gitnexus_") {
		t.Errorf("inv-zen-240: internal/augment/pipeline.go still has a mcp_zen-swarm_gitnexus_* wire name (L-3b incomplete)")
	}
	if !strings.Contains(pipeline, "mcp_zen-swarm_caronte_") {
		t.Errorf("inv-zen-240: internal/augment/pipeline.go lacks the caronte_* tool-name wire strings (L-3b)")
	}

	cb, err := os.ReadFile(filepath.Join(root, "internal/citation/types.go"))
	if err != nil {
		t.Fatalf("read citation/types.go: %v", err)
	}
	citation := string(cb)
	if !strings.Contains(citation, `"caronte_query"`) || !strings.Contains(citation, `"caronte_context"`) {
		t.Errorf("inv-zen-240: internal/citation/types.go lacks the caronte_query/caronte_context source values (L-3c)")
	}
}
