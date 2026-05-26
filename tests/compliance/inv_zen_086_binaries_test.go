package compliance

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen086MCPBinariesStdioOnly(t *testing.T) {
	root := repoRoot(t)

	mcpBinaries := []string{
		"cmd/zen-mcp-research",
		"cmd/zen-mcp-budget",
		"cmd/zen-mcp-audit",
		"cmd/zen-mcp-sshexec",
	}

	type forbiddenCall struct {
		pkg  string
		fn   string
		desc string
	}
	forbidden := []forbiddenCall{
		{"http", "ListenAndServe", "http.ListenAndServe"},
		{"http", "ListenAndServeTLS", "http.ListenAndServeTLS"},
		{"http", "Serve", "http.Serve"},
	}

	forbiddenNetListen := []string{"tcp", "tcp4", "tcp6", "unix"}

	for _, dir := range mcpBinaries {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			mainPath := filepath.Join(root, dir, "main.go")
			if _, err := os.Stat(mainPath); os.IsNotExist(err) {
				t.Skipf("main.go not yet created at %s (Phase I-M prerequisite)", mainPath)
				return
			}

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, mainPath, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", mainPath, err)
			}

			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}

				for _, fc := range forbidden {
					if pkgIdent.Name == fc.pkg && sel.Sel.Name == fc.fn {
						t.Errorf("inv-zen-086: %s/main.go contains forbidden call %s at %s",
							dir, fc.desc, fset.Position(call.Pos()))
					}
				}

				if pkgIdent.Name == "net" && sel.Sel.Name == "Listen" {
					if len(call.Args) > 0 {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok {
							network := strings.Trim(lit.Value, `"`)
							for _, banned := range forbiddenNetListen {
								if network == banned {
									t.Errorf("inv-zen-086: %s/main.go contains net.Listen(%q) at %s",
										dir, network, fset.Position(call.Pos()))
								}
							}
						}
					}
				}
				return true
			})
		})
	}
}

func TestInvZen086MCPBinariesUseMCPPackage(t *testing.T) {
	root := repoRoot(t)

	type mcpBinary struct {
		dir          string
		importSuffix string
	}
	binaries := []mcpBinary{
		{"cmd/zen-mcp-research", "internal/mcp/research"},
		{"cmd/zen-mcp-budget", "internal/mcp/budget"},
		{"cmd/zen-mcp-audit", "internal/mcp/audit"},
		{"cmd/zen-mcp-sshexec", "internal/mcp/sshexec"},
	}

	for _, b := range binaries {
		b := b
		t.Run(b.dir, func(t *testing.T) {
			t.Parallel()
			mainPath := filepath.Join(root, b.dir, "main.go")
			if _, err := os.Stat(mainPath); os.IsNotExist(err) {
				t.Skipf("main.go not yet created at %s", mainPath)
				return
			}

			data, err := os.ReadFile(mainPath)
			if err != nil {
				t.Fatalf("read %s: %v", mainPath, err)
			}

			if !bytes.Contains(data, []byte(b.importSuffix)) {
				t.Errorf("inv-zen-086: %s/main.go does not import %q; binary must wire real MCP server",
					b.dir, b.importSuffix)
			}
		})
	}
}
