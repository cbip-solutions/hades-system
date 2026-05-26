//go:build realworld
// +build realworld

package realworld

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func TestSSHExec_Realworld_VPSSimple(t *testing.T) {
	host := os.Getenv("ZEN_REALWORLD_VPS_HOST")
	if host == "" {
		t.Skip("ZEN_REALWORLD_VPS_HOST not set; skipping realworld test")
	}
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set; agent unavailable")
	}

	allowlist := []string{"hostname", "uname *", "echo *"}
	if v := os.Getenv("ZEN_REALWORLD_VPS_ALLOWLIST"); v != "" {
		allowlist = strings.Split(v, ",")
		for i, s := range allowlist {
			allowlist[i] = strings.TrimSpace(s)
		}
	}

	auth, err := sshexec.AgentAuth()
	if err != nil {
		t.Fatalf("AgentAuth: %v", err)
	}

	allow := &sshexec.Allowlist{
		Project:  "internal-platform-x",
		Patterns: allowlist,
		Hosts:    []string{host},
		Source:   "realworld-test",
	}

	req := sshexec.ExecRequest{
		Host:    host,
		Command: "hostname",
		Project: "internal-platform-x",
	}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, allow.Patterns)
	if !vr.OK {
		t.Fatalf("validate setup wrong: %s", vr.Reason)
	}

	sink := &sshexec.MemoryStreamSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := sshexec.Run(ctx, req, vr, allow, auth, sink, sshexec.NopAuditEmitter{})
	if err != nil {
		t.Fatalf("real SSH exec failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}
	if strings.TrimSpace(sink.StdoutString()) == "" {
		t.Fatalf("expected stdout (hostname), got empty")
	}
	t.Logf("realworld OK: host=%s hostname=%q", host, strings.TrimSpace(sink.StdoutString()))
}
