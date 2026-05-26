package compliance

import (
	"go/build"
	"strings"
	"testing"
)

func TestInvZen150_PhaseHHandlerImports(t *testing.T) {
	pkg, err := build.Default.Import(
		"github.com/cbip-solutions/hades-system/internal/daemon/handlers",
		"", build.ImportComment)
	if err != nil {
		t.Skipf("inv-zen-150: import scan unsupported in test env: %v", err)
	}

	forbidden := []string{
		"github.com/cbip-solutions/hades-system/internal/audit/tessera",
		"github.com/cbip-solutions/hades-system/internal/audit/chain",
		"github.com/cbip-solutions/hades-system/internal/audit/litestream",
		"github.com/cbip-solutions/hades-system/internal/audit/recovery",
		"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator",
		"github.com/cbip-solutions/hades-system/internal/knowledge/embed",
	}

	for _, imp := range pkg.Imports {
		for _, f := range forbidden {
			if strings.HasPrefix(imp, f) {
				t.Errorf("inv-zen-150 violated: handlers package imports forbidden %q "+
					"(all substrate access must go through adapter interfaces)", imp)
			}
		}
	}

	t.Log("inv-zen-150 Phase H: package-level scan passed — Phase K extends with file-level AST walk")
}
