// Tests for the package skeleton (Phase B Task B-1).
//
// These tests pin the sentinel + zero-value contracts that the rest of
// Phase B (B-2 .. B-8) builds on. Do not delete this file; B-2 keeps it
// alongside zenswarm_transport_test.go (the plan's "fold into B-2" note
// became unnecessary once the sentinel anchor remained at package surface).
package transport_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
)

func TestSingleEgressSentinelExists(t *testing.T) {
	err := transport.SingleEgressSentinel()
	if err == nil {
		t.Fatal("SingleEgressSentinel must return non-nil sentinel error proving the anchor is reachable")
	}
	if got := err.Error(); got == "" {
		t.Errorf("SingleEgressSentinel error message must be non-empty; got %q", got)
	}
}

func TestForwardedRequestZeroValueValid(t *testing.T) {
	var req transport.ForwardedRequest
	if req.Body != nil {
		t.Error("zero-value ForwardedRequest.Body must be nil")
	}
	if req.Headers != nil {
		t.Error("zero-value ForwardedRequest.Headers must be nil")
	}
	if req.SessionID != "" {
		t.Error("zero-value ForwardedRequest.SessionID must be empty")
	}
}

func TestForwardedResponseZeroValueValid(t *testing.T) {
	var resp transport.ForwardedResponse
	if resp.Status != 0 {
		t.Error("zero-value ForwardedResponse.Status must be 0")
	}
	if resp.Body != "" {
		t.Error("zero-value ForwardedResponse.Body must be empty string")
	}
}

func TestTierBackendInterfaceAnchorReachable(t *testing.T) {
	zero := transport.TierBackendInterfaceAnchor()
	if zero != nil {
		t.Errorf("TierBackendInterfaceAnchor() must return zero-value interface (nil); got %T", zero)
	}
}
