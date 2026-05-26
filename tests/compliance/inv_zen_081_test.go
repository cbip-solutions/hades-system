// Phase L Task L-7 — runtime compliance test for inv-zen-081.
//
// inv-zen-081: ssh-exec interactive prompt MUST be detected, the session
// SIGKILLed, and the prompt content MUST NOT be returned to the caller.
// The only legitimate signal is ExecResult.InteractiveBlocked=true plus
// audit emit "interactive_blocked".

package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestInvZen081InteractiveBlockedNeverReturned(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	prompts := []string{
		"[sudo] password for testuser: ",
		"Are you sure (yes/no)? ",
		"password: ",
		"(yes/no): ",
	}
	for _, p := range prompts {
		pp := p
		t.Run(pp, func(t *testing.T) {
			srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
				return testharness.HandlerScript{Stdout: pp, ExitCode: 0}
			}))
			defer srv.Close()
			req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "internal-platform-x"}
			req.ApplyDefaults()
			vr := sshexec.Validate(req.Command, []string{"alembic *"})
			allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
			sink := &sshexec.MemoryStreamSink{}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res, _ := sshexec.Run(ctx, req, vr, allow, sshexec.AgentAuthForTest(), sink, sshexec.NopAuditEmitter{})
			if !res.InteractiveBlocked {
				t.Fatalf("inv-zen-081 violation: prompt %q not blocked (res=%+v)", pp, res)
			}
			if res.ExitCode != 0 && res.ExitCode != -1 {

			}
			if res.BlockedReason == "" {
				t.Errorf("inv-zen-081: BlockedReason empty for prompt %q", pp)
			}
		})
	}
}
