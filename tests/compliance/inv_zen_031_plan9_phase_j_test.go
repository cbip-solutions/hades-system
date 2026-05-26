package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var plan9PhaseJPackagesForbiddenStoreImport = []string{
	"internal/lint",
	"internal/audit/recovery",
}

func TestInvZen031Plan9PhaseJPackagesNoStoreImport(t *testing.T) {
	root := repoRoot(t)
	for _, pkg := range plan9PhaseJPackagesForbiddenStoreImport {
		t.Run(pkg, func(t *testing.T) {
			pkgPath := filepath.Join(root, pkg)
			if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
				t.Skipf("package %s not yet present (Phase J in flight)", pkg)
			}
			if err := walkAndCheckImports(pkgPath); err != nil {
				t.Errorf("inv-zen-031 violated in Plan 9 Phase J package %s: %v", pkg, err)
			}
		})
	}
}

func TestInvZen031Plan9LintAnalyzersNoStoreImport(t *testing.T) {
	root := repoRoot(t)

	analyzerFiles := []string{
		filepath.Join(root, "internal", "lint", "no_web_in_aggregator.go"),
		filepath.Join(root, "internal", "lint", "no_auto_promote.go"),
		filepath.Join(root, "internal", "lint", "no_cross_project_at_tessera.go"),
	}

	for _, path := range analyzerFiles {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Skipf("analyzer file %s not yet present (Phase J in flight)", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			if strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) ||
				strings.Contains(text, "`github.com/cbip-solutions/hades-system/internal/store`") {
				t.Errorf("inv-zen-031 violated: %s imports internal/store directly; "+
					"lint analyzers must not reach into the daemon store layer", path)
			}
		})
	}
}

func TestInvZen031Plan9AuditRecoveryNoStoreImport(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "audit", "recovery", "tamper_dispatcher_activation.go")
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Skipf("tamper_dispatcher_activation.go not yet present (Phase J in flight)")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	text := string(data)
	if strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) {
		t.Errorf("inv-zen-031 violated: tamper_dispatcher_activation.go imports internal/store directly; " +
			"audit/recovery packages must not cross the store boundary; bridge via dispatcheradapter")
	}
}

func TestInvZen031Plan9DoctrineSchemasNoStoreImport(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "doctrine", "audit_schemas.go")
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Skipf("audit_schemas.go not yet present (Phase J in flight)")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	text := string(data)
	if strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) {
		t.Errorf("inv-zen-031 violated: internal/doctrine/audit_schemas.go imports internal/store directly; " +
			"doctrine schema types must be persistence-agnostic (inv-zen-031 + inv-zen-133)")
	}
}
