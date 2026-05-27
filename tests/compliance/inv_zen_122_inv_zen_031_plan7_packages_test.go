package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plan7PackagesForbiddenStoreImport lists the packages that are
// FORBIDDEN from importing internal/store. As Plans 7 B-G land, append
// to this slice (e.g., internal/quota/, internal/scheduler/, internal/inbox/,
// internal/zenday/, internal/knowledge/, internal/tmuxlife/).
//
// The exemption is internal/daemon/projectctxadapter/ (and future
// {quota,scheduler,inbox,zenday,knowledge,tmuxlife}adapter packages):
// they are the explicit bridge between the boundary-respecting package
// and *store.Store.
//
// J-3..J-6 deferred — knowledge / scheduler / inbox / tmuxlife now own
// concrete Prober types (internal/{knowledge,scheduler,inbox,tmuxlife}/
// prober.go). Per invariant + invariant the prober packages MUST NOT
// import internal/store; the daemon-side adapter package crosses the
// boundary on their behalf (e.g. inboxadapter, scheduleradapter).
var plan7PackagesForbiddenStoreImport = []string{
	"internal/projectctx",
	"internal/quota",
	"internal/tmuxlife",
	"internal/scheduler",
	"internal/inbox",
	"internal/knowledge",
}

func TestInvZen122Plan7PackagesDoNotImportStore(t *testing.T) {
	root := repoRoot(t)
	for _, pkg := range plan7PackagesForbiddenStoreImport {
		t.Run(pkg, func(t *testing.T) {
			pkgPath := filepath.Join(root, pkg)
			if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
				t.Skipf("package %s not yet present (future Plan 7 phase)", pkg)
			}
			if err := walkAndCheckImports(pkgPath); err != nil {
				t.Errorf("inv-zen-122 violated in %s: %v", pkg, err)
			}
		})
	}
}

func walkAndCheckImports(pkgPath string) error {
	var violations []string
	err := filepath.Walk(pkgPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)

		if strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) ||
			strings.Contains(text, "`github.com/cbip-solutions/hades-system/internal/store`") {
			violations = append(violations, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return &importViolation{paths: violations}
	}
	return nil
}

type importViolation struct{ paths []string }

func (v *importViolation) Error() string {
	return "forbidden internal/store import in: " + strings.Join(v.paths, ", ")
}

func TestInvZen122AdapterPackageIsTheBridge(t *testing.T) {
	root := repoRoot(t)
	adapterPath := filepath.Join(root, "internal", "daemon", "projectctxadapter", "adapter.go")
	if _, err := os.Stat(adapterPath); os.IsNotExist(err) {
		t.Skip("projectctxadapter not present yet")
	}
	data, err := os.ReadFile(adapterPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/projectctx"`) {
		t.Error("adapter.go missing import of internal/projectctx")
	}
	if !strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) {
		t.Error("adapter.go missing import of internal/store (it MUST cross the boundary)")
	}
}

func TestInvZen122QuotaAdapterIsTheBridge(t *testing.T) {
	root := repoRoot(t)
	adapterPath := filepath.Join(root, "internal", "daemon", "quotaadapter", "adapter.go")
	if _, err := os.Stat(adapterPath); os.IsNotExist(err) {
		t.Skip("quotaadapter not present yet")
	}
	data, err := os.ReadFile(adapterPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/quota"`) {
		t.Error("quotaadapter/adapter.go missing import of internal/quota")
	}
	if !strings.Contains(text, `"github.com/cbip-solutions/hades-system/internal/store"`) {
		t.Error("quotaadapter/adapter.go missing import of internal/store (it MUST cross the boundary)")
	}
}
