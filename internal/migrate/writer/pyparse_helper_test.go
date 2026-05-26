package writer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pyParseOrSkip(t *testing.T, body []byte) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable on this runner; sidecar parse-validity check skipped")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "module.py")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write tmp module.py: %v", err)
	}
	cmd := exec.Command(py, "-c",
		"import sys, py_compile; py_compile.compile(sys.argv[1], doraise=True)", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("python3 syntax check failed:\n%s\nbody:\n%s", out, body)
	}
}

func pyImportNoSideEffectOrSkip(t *testing.T, body []byte, canaryFilePath string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable; import-side-effect check skipped")
	}

	_ = os.Remove(canaryFilePath)
	dir := t.TempDir()
	modPath := filepath.Join(dir, "mod_under_test.py")
	if err := os.WriteFile(modPath, body, 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	cmd := exec.Command(py, "-c",
		"import sys; sys.path.insert(0, sys.argv[1]); import mod_under_test", dir)
	out, err := cmd.CombinedOutput()

	defer func() { _ = os.Remove(canaryFilePath) }()
	if err != nil {
		// Import-error itself is a security-positive outcome (hostile content
		// failed to parse cleanly + the side effect didn't happen). Surface
		// the message for visibility but don't fail.
		t.Logf("python3 import returned non-zero exit (acceptable; hostile body refused at import):\n%s", out)
	}
	if _, statErr := os.Stat(canaryFilePath); statErr == nil {
		t.Errorf("RCE: hostile body executed during import; canary file appeared at %s", canaryFilePath)
	}
}

func pyImportRegisterAgainstStubCtx(t *testing.T, body []byte, handlerNames []string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable on this runner; register-against-stub check skipped")
	}
	pkgDir := filepath.Join(t.TempDir(), "package_under_test")
	cmdDir := filepath.Join(pkgDir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}

	if err := os.WriteFile(filepath.Join(pkgDir, "__init__.py"), body, 0o644); err != nil {
		t.Fatalf("write __init__.py: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cmdDir, "__init__.py"), []byte(""), 0o644); err != nil {
		t.Fatalf("write commands/__init__.py: %v", err)
	}

	for _, n := range handlerNames {
		modPath := filepath.Join(cmdDir, n+".py")
		stub := "def " + n + "_handler(raw_args=None): return None\n"
		if err := os.WriteFile(modPath, []byte(stub), 0o644); err != nil {
			t.Fatalf("write stub commands/%s.py: %v", n, err)
		}
	}
	script := `
import sys
sys.path.insert(0, sys.argv[1])
import package_under_test
class StubCtx:
    def register_hook(self, *a, **kw): pass
    def register_skill(self, *a, **kw): pass
    def register_command(self, *a, **kw): pass
package_under_test.register(StubCtx())
print("REGISTER_OK")
`
	cmd := exec.Command(py, "-c", script, filepath.Dir(pkgDir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("register() raised an exception (Bug 1 reproducer):\n%s\nrendered __init__.py:\n%s", out, body)
		return
	}
	if !strings.Contains(string(out), "REGISTER_OK") {
		t.Errorf("register() did not complete (no REGISTER_OK sentinel):\n%s\nrendered __init__.py:\n%s", out, body)
	}
}
