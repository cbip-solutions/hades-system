package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
)

func TestInvZen175HermesRequired(t *testing.T) {
	c := preflight.NewHermesCheckForTest(
		func(_ string) (string, error) { return "", preflight.ErrExecNotFound() },
		nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := c.Run(ctx)
	if r.Status != preflight.StatusFail {
		t.Fatalf("inv-zen-175: missing Hermes did not fail: %+v", r)
	}
	if r.ExitCode != 3 {
		t.Errorf("inv-zen-175: ExitCode = %d, want 3 (preflight failure)", r.ExitCode)
	}
	if r.RemediationHint == "" {
		t.Errorf("inv-zen-175: RemediationHint empty; operator-facing remediation required")
	}
}

func TestInvZen175VersionTooOldFails(t *testing.T) {
	c := preflight.NewHermesCheckForTest(
		func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.12.0", nil },
	)
	r := c.Run(context.Background())
	if r.Status != preflight.StatusFail {
		t.Errorf("inv-zen-175: Hermes 0.12.0 did not fail: %+v", r)
	}
	if r.ExitCode != 3 {
		t.Errorf("inv-zen-175: ExitCode = %d, want 3 (preflight floor) for version below minimum", r.ExitCode)
	}
}

func TestInvZen175VersionAtFloorPasses(t *testing.T) {
	c := preflight.NewHermesCheckForTest(
		func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		func(_ context.Context, _ string) (string, error) { return "hermes 0.13.0", nil },
	)
	r := c.Run(context.Background())
	if r.Status != preflight.StatusPass {
		t.Errorf("inv-zen-175: Hermes 0.13.0 did not pass: %+v", r)
	}
}

func TestInvZen175VersionAboveFloorPasses(t *testing.T) {
	cases := []string{"hermes 0.14.0", "hermes-agent v1.0.0", "Hermes Agent 0.13.99"}
	for _, raw := range cases {
		c := preflight.NewHermesCheckForTest(
			func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
			func(_ context.Context, _ string) (string, error) { return raw, nil },
		)
		r := c.Run(context.Background())
		if r.Status != preflight.StatusPass {
			t.Errorf("inv-zen-175: %q did not pass: %+v", raw, r)
		}
	}
}
