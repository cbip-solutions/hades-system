// go:build a7_full

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestPreflightOrchestrationProducesThreeResults(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	pf := preflight.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, err := pf.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Run returned %d results, want 3 (hermes, plugin_format, daemon)", len(results))
	}

	for i, r := range results {
		if r.Name == "" {
			t.Errorf("result[%d].Name empty", i)
		}
		switch r.Status {
		case preflight.StatusPass, preflight.StatusWarn, preflight.StatusFail, preflight.StatusSkip:

		default:
			t.Errorf("result[%d].Status = %v; not in {Pass,Warn,Fail,Skip}", i, r.Status)
		}
	}
}

func TestPreflightStatusSerializableAcrossPlatforms(t *testing.T) {
	cases := []preflight.Status{
		preflight.StatusPass,
		preflight.StatusWarn,
		preflight.StatusFail,
		preflight.StatusSkip,
	}
	for _, s := range cases {
		if s.String() == "" {
			t.Errorf("preflight.Status(%d).String() = empty; must render canonical label", s)
		}
	}
}

func TestPreflightHonorsContextCancellation(t *testing.T) {
	td := testhelpers.NewOnboardTestDaemon(t)
	defer td.Stop()

	pf := preflight.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pf.Run(ctx)

	if err == nil {
		t.Error("Run with pre-canceled ctx: expected non-nil error, got nil")
	}
}
