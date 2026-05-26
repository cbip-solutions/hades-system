//go:build adversarial
// +build adversarial

// Phase L Task L-2 — adversarial test runner for the ssh-exec validator.
// Reads tests/adversarial/payloads/cmd_injection.txt and asserts every
// payload is rejected by sshexec.Validate under a permissive allowlist.
// Failure of any payload is a security regression.

package adversarial

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func TestSSHExecCmdInjectionCorpus(t *testing.T) {
	allowlist := []string{
		"alembic *",
		"pytest *",
		"psql *",
		"docker compose -f docker/docker-compose.yml *",
		"git status",
		"git log",
	}
	f, err := os.Open("payloads/cmd_injection.txt")
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()
	count := 0
	failures := []string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
		r := sshexec.Validate(line, allowlist)
		if r.OK {
			failures = append(failures, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}
	if count < 50 {
		t.Fatalf("corpus contains %d entries; want >=50", count)
	}
	if len(failures) > 0 {
		t.Errorf("%d adversarial payloads accepted (security regression):", len(failures))
		for _, p := range failures {
			t.Errorf("  ACCEPTED: %q", p)
		}
	}
}
