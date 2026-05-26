// Phase L Task L-2 — runtime compliance test for inv-zen-082.
//
// inv-zen-082: ssh-exec allowlist enforcement. Forbidden chars +
// non-prefix-match commands MUST be rejected by the validator and never
// reach exec.
//
// Compile-check anchor: exec.Run signature (Task L-5) requires a
// ValidationResult with OK=true; this test exercises the runtime arm.

package compliance

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func TestInvZen082AllowlistEnforcement(t *testing.T) {
	allowlist := []string{"alembic *", "pytest *"}
	rejected := []string{
		"alembic upgrade ; rm -rf /",
		"alembic upgrade$(whoami)",
		"ls",
		"sudo apt update",
		"alembicX",
		"alembic\nupgrade",
		"",
	}
	for _, cmd := range rejected {
		r := sshexec.Validate(cmd, allowlist)
		if r.OK {
			t.Errorf("inv-zen-082 violation: %q accepted; want rejected", cmd)
		}
		if r.Reason == "" {
			t.Errorf("inv-zen-082: rejection of %q has empty reason", cmd)
		}

		if !strings.HasPrefix(r.Reason, "forbidden") &&
			!strings.HasPrefix(r.Reason, "command not in allowlist") &&
			!strings.HasPrefix(r.Reason, "empty command") {
			t.Errorf("inv-zen-082: unexpected reason for %q: %q", cmd, r.Reason)
		}
	}
	allowed := []string{
		"alembic upgrade head",
		"alembic",
		"pytest tests/integration/foo.py",
	}
	for _, cmd := range allowed {
		r := sshexec.Validate(cmd, allowlist)
		if !r.OK {
			t.Errorf("inv-zen-082 false-positive: %q rejected (%s); want allowed", cmd, r.Reason)
		}
	}
}
