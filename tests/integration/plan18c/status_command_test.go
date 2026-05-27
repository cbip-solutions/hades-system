// go:build integration
package plan18c_integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

func shortSocketPath(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("zen-int-%d.sock", time.Now().UnixNano()%1_000_000_000)
	path := filepath.Join("/tmp", name)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func startTestDaemon(t *testing.T, daemonBin, udsPath string) (cancel func()) {
	t.Helper()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 60*time.Second)
	cmd := exec.CommandContext(ctx, daemonBin, "--uds", udsPath)
	cmd.Env = append(os.Environ(), "ZEN_BYPASS_DISABLE_KEYCHAIN=1")

	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancelCtx()
		t.Fatalf("start daemon: %v", err)
	}

	stop := func() {
		cancelCtx()
		_ = cmd.Wait()
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(udsPath); err == nil {
			if c, err := net.Dial("unix", udsPath); err == nil {
				_ = c.Close()
				return stop
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	stop()
	t.Fatalf("daemon UDS did not appear within 20s at %s", udsPath)
	return stop
}

func TestStatusSlashCommand_IntegrationAgainstRealDaemon(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("UDS only supported on darwin/linux")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}

	root := repoRoot(t)
	daemonBin := filepath.Join(root, "bin", "zen-swarm-ctld")
	if _, err := os.Stat(daemonBin); err != nil {
		t.Skipf("daemon binary not built (%s); run 'make build' first", daemonBin)
	}

	udsPath := shortSocketPath(t)

	stop := startTestDaemon(t, daemonBin, udsPath)
	defer stop()

	// Invoke the Python handler via subprocess in --json mode. The handler is
	// imported from plugin/hades directly via sys.path injection so we do not
	// need the plugin installed in the test environment's site-packages.
	snippet := strings.Join([]string{
		"import sys, os",
		"sys.path.insert(0, " + strconv.Quote(filepath.Join(root, "plugin", "hades")) + ")",
		"os.environ['ZEN_SWARM_UDS'] = " + strconv.Quote(udsPath),
		"from commands.status import handle_status",
		"result = handle_status('--json')",
		"sys.stdout.write(result or '')",
	}, "; ")

	pyCmd := exec.Command("python3", "-c", snippet)
	pyCmd.Env = append(os.Environ(),
		"ZEN_SWARM_UDS="+udsPath,
		"NO_COLOR=1",
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	)
	out, err := pyCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python handler failed: %v\noutput:\n%s", err, out)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("json output not parseable: %v\noutput:\n%s", err, out)
	}

	sv, ok := payload["schema_version"].(float64)
	if !ok || sv != 1 {
		t.Errorf("schema_version != 1 (got %v); output:\n%s", payload["schema_version"], out)
	}

	fields, ok := payload["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields key missing or wrong type in JSON output; output:\n%s", out)
	}
	expectedFields := []string{
		"daemon", "model", "cascade", "bypass",
		"cost_24h", "context", "profile", "cwd",
	}
	for _, name := range expectedFields {
		if _, ok := fields[name]; !ok {
			t.Errorf("fields key %q missing from JSON output\noutput:\n%s", name, out)
		}
	}
}

func TestStatusSlashCommand_TextModeAgainstRealDaemon(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("UDS only supported on darwin/linux")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}

	root := repoRoot(t)
	daemonBin := filepath.Join(root, "bin", "zen-swarm-ctld")
	if _, err := os.Stat(daemonBin); err != nil {
		t.Skipf("daemon binary not built (%s); run 'make build' first", daemonBin)
	}

	udsPath := shortSocketPath(t)

	stop := startTestDaemon(t, daemonBin, udsPath)
	defer stop()

	snippet := strings.Join([]string{
		"import sys, os",
		"sys.path.insert(0, " + strconv.Quote(filepath.Join(root, "plugin", "hades")) + ")",
		"os.environ['ZEN_SWARM_UDS'] = " + strconv.Quote(udsPath),
		"from commands.status import handle_status",
		"result = handle_status('')",
		"sys.stdout.write(result or '')",
	}, "; ")

	pyCmd := exec.Command("python3", "-c", snippet)
	pyCmd.Env = append(os.Environ(),
		"ZEN_SWARM_UDS="+udsPath,
		"NO_COLOR=1",
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
	)
	out, err := pyCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python handler failed (text mode): %v\noutput:\n%s", err, out)
	}

	text := strings.ToLower(string(out))

	expectedLabels := []string{
		"daemon", "model", "cascade", "bypass",
		"cost 24h", "context", "profile", "cwd",
	}
	for _, label := range expectedLabels {
		if !strings.Contains(text, label) {
			t.Errorf("text output missing label %q\noutput:\n%s", label, out)
		}
	}
}
