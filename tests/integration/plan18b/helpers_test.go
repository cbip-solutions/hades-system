// go:build integration
package plan18b_integration_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func markerInt(t *testing.T, out, marker string) int {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(marker) + `:(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("marker %q:<int> not found in probe output:\n%s", marker, out)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("marker %q value %q not an int: %v", marker, m[1], err)
	}
	return n
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

func pluginHadesDir(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "plugin", "hades")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("plugin/hades/ not at expected path: %v", err)
	}
	return dir
}

func buildHadesBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "hades")
	root := repoRoot(t)

	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", out, "./cmd/hades")
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cmd/hades: %v\n%s", err, buildOut)
	}
	return out
}

func buildZenBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zen")
	root := repoRoot(t)

	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", out, "./cmd/zen")
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cmd/zen: %v\n%s", err, buildOut)
	}
	return out
}

func buildZenSwarmCtldBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zen-swarm-ctld")
	root := repoRoot(t)
	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", out,
		"./cmd/zen-swarm-ctld",
	)
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cmd/zen-swarm-ctld: %v\n%s", err, buildOut)
	}
	return out
}

type stubInvocation struct {
	Argv []string          `json:"argv"`
	Env  map[string]string `json:"env"`
}

func buildStubBinaryAt(t *testing.T, outDir, stubName, recordPath string, exitCode int) string {
	t.Helper()
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "main.go")
	writeStubSource(t, srcPath, recordPath, exitCode)
	binPath := filepath.Join(outDir, stubName)
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub %q: %v\n%s", stubName, err, buildOut)
	}
	return binPath
}

func writeStubSource(t *testing.T, srcPath, recordPath string, exitCode int) {
	t.Helper()
	src := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"
)

func main() {
	envMap := map[string]string{}
	for _, e := range os.Environ() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				envMap[e[:i]] = e[i+1:]
				break
			}
		}
	}
	rec := struct {
		Argv []string          %s
		Env  map[string]string %s
	}{
		Argv: os.Args,
		Env:  envMap,
	}
	f, err := os.OpenFile(%q, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(rec); err != nil {
		os.Exit(2)
	}
	os.Exit(%d)
}
`, "`json:\"argv\"`", "`json:\"env\"`", recordPath, exitCode)
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write stub source: %v", err)
	}
}

func readStubInvocations(t *testing.T, recordPath string) []stubInvocation {
	t.Helper()
	f, err := os.Open(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open stub record %q: %v", recordPath, err)
	}
	defer f.Close()
	var out []stubInvocation
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec stubInvocation
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("malformed stub record line %q: %v", string(line), err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan stub record %q: %v", recordPath, err)
	}
	return out
}

func newSandboxEnv(t *testing.T, pathPrepend string) []string {
	t.Helper()
	sandbox := t.TempDir()
	home := filepath.Join(sandbox, "home")
	xdgConfig := filepath.Join(home, ".config")
	xdgState := filepath.Join(home, ".local", "state")
	xdgCache := filepath.Join(home, ".cache")
	xdgData := filepath.Join(home, ".local", "share")
	for _, d := range []string{home, xdgConfig, xdgState, xdgCache, xdgData} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", d, err)
		}
	}
	var clean []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "XDG_") ||
			strings.HasPrefix(e, "HERMES_") ||
			strings.HasPrefix(e, "ZEN_") ||
			strings.HasPrefix(e, "HADES_") {
			continue
		}
		clean = append(clean, e)
	}
	clean = append(clean,
		"HOME="+home,
		"XDG_CONFIG_HOME="+xdgConfig,
		"XDG_STATE_HOME="+xdgState,
		"XDG_CACHE_HOME="+xdgCache,
		"XDG_DATA_HOME="+xdgData,
	)
	if pathPrepend != "" {
		clean = append(clean, "PATH="+pathPrepend+string(os.PathListSeparator)+os.Getenv("PATH"))
	} else {
		clean = append(clean, "PATH="+os.Getenv("PATH"))
	}
	return clean
}

func quoteForPython(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
