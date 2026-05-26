package compliance

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen269AuditEmitterIsSoleAppendLeafCaller(t *testing.T) {
	root := repoRoot(t)
	federationRoot := filepath.Join(root, "internal", "caronte", "store", "federation")

	allowedFile := "audit.go"
	scanned := 0
	_ = filepath.WalkDir(federationRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("inv-zen-269: read %s: %v", path, err)
			return nil
		}
		text := string(body)
		if strings.Contains(text, ".AppendLeaf(") {
			if filepath.Base(path) != allowedFile {
				t.Errorf("inv-zen-269 violated: %s calls .AppendLeaf — only %s "+
					"(the EmitAudit chokepoint) may invoke tessera.Adapter.AppendLeaf",
					path, allowedFile)
			}
		}
		return nil
	})
	if scanned == 0 {
		t.Fatal("inv-zen-269: sentinel failure — 0 production Go files scanned " +
			"under internal/caronte/store/federation/")
	}
}

func TestInvZen269ImportsScan(t *testing.T) {
	root := repoRoot(t)
	federationRoot := filepath.Join(root, "internal", "caronte", "store", "federation")
	const tesseraImport = "github.com/cbip-solutions/hades-system/internal/audit/tessera"
	const allowedFile = "audit.go"
	scanned := 0
	_ = filepath.WalkDir(federationRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("inv-zen-269: parse %s: %v", path, err)
			return nil
		}
		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			ip := strings.Trim(imp.Path.Value, `"`)
			if ip != tesseraImport {
				continue
			}
			if filepath.Base(path) != allowedFile {
				t.Errorf("inv-zen-269 violated: %s imports %q — only %s may; "+
					"every federation write must route through EmitAudit",
					path, ip, allowedFile)
			}
		}
		return nil
	})
	if scanned == 0 {
		t.Fatal("inv-zen-269: sentinel failure — 0 production Go files scanned")
	}
}
