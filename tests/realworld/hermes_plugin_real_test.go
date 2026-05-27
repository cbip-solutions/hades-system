// go:build realworld
//go:build realworld
// +build realworld

package realworld_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func skipIfNotReal(t *testing.T) {
	t.Helper()
	if os.Getenv("ZEN_REALWORLD_HERMES") != "1" {
		t.Skip("set ZEN_REALWORLD_HERMES=1 to run Tier 3 real-binary smoke")
	}
	if _, err := exec.LookPath("hermes"); err != nil {
		t.Fatalf("hermes binary not on PATH: %v (install: brew install hermes-agent)", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not find repo root from %s", wd)
		}
		wd = parent
	}
}

func startDaemon(t *testing.T) (string, func()) {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "zen-swarm-ctld")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("daemon binary not found at %s (run: make build): %v", bin, err)
	}

	tmp := t.TempDir()
	uds := filepath.Join("/tmp", fmt.Sprintf("zsrw-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(uds) })
	db := filepath.Join(tmp, "state.db")
	cmd := exec.Command(bin, "-uds", uds, "-db", db)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"ZEN_BYPASS_DISABLE_KEYCHAIN=1",
		"ZEN_KEYCHAIN_DISABLE=1",
	)
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("daemon start: %v\n%s", err, stderr.String())
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			t.Fatalf("daemon exited before socket was ready: %v\nstdout:\n%s\nstderr:\n%s",
				err, stdout.String(), stderr.String())
		case <-ticker.C:
			if st, err := os.Stat(uds); err == nil && st.Mode()&os.ModeSocket != 0 {
				cleanup := func() {
					select {
					case <-done:
					default:
						_ = cmd.Process.Kill()
						<-done
					}
				}
				return uds, cleanup
			}
		case <-deadline.C:
			_ = cmd.Process.Kill()
			err := <-done
			t.Fatalf("daemon socket not ready at %s: %v\nstdout:\n%s\nstderr:\n%s",
				uds, err, stdout.String(), stderr.String())
		}
	}
}

func getHermesProbe(t *testing.T, uds, check string) map[string]string {
	t.Helper()
	dialer := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", uds)
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dialer},
	}
	url := "http://unix/v1/hermes/probe?check=" + check
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s via %s: %v", url, uds, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", url, resp.StatusCode, body)
	}
	var out map[string]string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode hermes probe body %q: %v", body, err)
	}
	return out
}

func TestHermesPluginReal_DaemonProbe(t *testing.T) {
	skipIfNotReal(t)
	uds, stop := startDaemon(t)
	defer stop()

	for _, check := range []string{"plugin_installed", "transport_reachable"} {
		got := getHermesProbe(t, uds, check)
		if got["status"] != "ok" {
			t.Fatalf("probe %s status=%q detail=%q", check, got["status"], got["detail"])
		}
		if strings.Contains(got["detail"], "unknown check name") {
			t.Fatalf("probe %s returned unknown-check false positive: %v", check, got)
		}
	}
}

func TestHermesPluginReal_MCPConfigListed(t *testing.T) {
	skipIfNotReal(t)
	cmd := exec.Command("hermes", "mcp", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hermes mcp list failed: %v\n%s", err, out)
	}
	text := string(out)
	for _, name := range []string{
		"zen-mcp-research",
		"zen-mcp-budget",
		"zen-mcp-audit",
		"zen-mcp-sshexec",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("required MCP %s not in hermes mcp list:\n%s", name, text)
		}
	}
	if strings.Contains(text, "zen-mcp-codegen") {
		t.Fatalf("retired MCP zen-mcp-codegen still appears in hermes mcp list:\n%s", text)
	}
}

func TestHermesPluginReal_PluginListed(t *testing.T) {
	skipIfNotReal(t)
	cmd := exec.Command("hermes", "plugins", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hermes plugins list failed: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "hades") {
		t.Fatalf("hades not in plugin list:\n%s", out)
	}
	if !strings.Contains(text, "caronte") {
		t.Fatalf("installed HADES plugin metadata is stale (missing caronte):\n%s", out)
	}
	if strings.Contains(text, "gitnexus") {
		t.Fatalf("installed HADES plugin metadata still references gitnexus:\n%s", out)
	}
}
