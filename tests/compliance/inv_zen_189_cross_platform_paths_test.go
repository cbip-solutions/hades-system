package compliance

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInvZen189CrossPlatformPathsNoHardcodedSeparators(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "onboard")
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("internal/onboard absent at %q; nothing to scan", target)
		return
	}

	joinRe := regexp.MustCompile(`filepath\.Join\([^)]*"/[^"]*"[^)]*\)`)

	allowlist := []*regexp.Regexp{
		regexp.MustCompile(`"/tmp/zen-swarm\.sock"`),
		regexp.MustCompile(`"/\.config"`),
		regexp.MustCompile(`"/\.local"`),
		regexp.MustCompile(`"/\.cache"`),
		regexp.MustCompile(`"/\.hermes"`),
	}

	var bad []string
	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range joinRe.FindAllString(string(data), -1) {
			if isAllowlisted(m, allowlist) {
				continue
			}
			bad = append(bad, path+": "+m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", target, err)
	}
	if len(bad) > 0 {
		t.Errorf("inv-zen-189: hardcoded path separators detected in filepath.Join calls:\n%s\nUse filepath.Join arguments without leading-slash strings; use path/filepath helpers + allowlisted suffixes instead.", strings.Join(bad, "\n"))
	}
}

func TestInvZen189CrossPlatformCompileLinux(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "onboard")
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("internal/onboard absent; skipping cross-platform compile")
		return
	}
	runCrossPlatformBuild(t, root, "linux")
}

func TestInvZen189CrossPlatformCompileWindows(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "onboard")
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("internal/onboard absent; skipping cross-platform compile")
		return
	}
	runCrossPlatformBuild(t, root, "windows")
}

func TestInvZen189CrossPlatformCompileDarwin(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "onboard")
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("internal/onboard absent; skipping cross-platform compile")
		return
	}
	runCrossPlatformBuild(t, root, "darwin")
}

func runCrossPlatformBuild(t *testing.T, root, goos string) {
	t.Helper()
	cmd := exec.Command("go", "build", "./internal/onboard/...")
	cmd.Dir = root

	env := os.Environ()
	env = append(env, "GOOS="+goos, "CGO_ENABLED=0")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("GOOS=%s build failed:\n%s", goos, out)
	}
}

func isAllowlisted(match string, allow []*regexp.Regexp) bool {
	for _, re := range allow {
		if re.MatchString(match) {
			return true
		}
	}
	return false
}
