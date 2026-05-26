package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCtldStartArgs_EnablesLoopbackHTTP(t *testing.T) {
	args := ctldStartArgs("/tmp/zen-swarm.sock")
	want := []string{"-uds", "/tmp/zen-swarm.sock", "-http", defaultDaemonHTTPAddr}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("ctldStartArgs = %v, want %v", args, want)
	}
	if defaultDaemonHTTPAddr != "127.0.0.1:8080" {
		t.Errorf("defaultDaemonHTTPAddr = %q, want 127.0.0.1:8080 (must match plugin/hades/_constants.py DEFAULT_DAEMON_BASE_URL)", defaultDaemonHTTPAddr)
	}

	if strings.HasPrefix(defaultDaemonHTTPAddr, "0.0.0.0") || strings.HasPrefix(defaultDaemonHTTPAddr, ":") {
		t.Errorf("defaultDaemonHTTPAddr %q must bind loopback only", defaultDaemonHTTPAddr)
	}
}

func TestLaunchdTemplate_EnablesLoopbackHTTP(t *testing.T) {
	tmpl := filepath.Join("..", "..", "configs", "launchd.plist.tmpl")
	b, err := os.ReadFile(tmpl)
	if err != nil {
		t.Fatalf("read %s: %v", tmpl, err)
	}
	s := string(b)
	if !strings.Contains(s, "-http") || !strings.Contains(s, "127.0.0.1:8080") {
		t.Errorf("%s must pass -http 127.0.0.1:8080 so launchd-managed daemons serve the Hermes TCP port", tmpl)
	}
}
