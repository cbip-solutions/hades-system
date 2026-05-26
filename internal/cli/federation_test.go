package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeFederationClient struct {
	healthResp *client.FederationHealthResponse
	impactResp *client.APIImpactResponse
	err        error
}

func (f *fakeFederationClient) FederationHealth(_ context.Context, _ client.FederationHealthRequest) (*client.FederationHealthResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.healthResp, nil
}
func (f *fakeFederationClient) APIImpact(_ context.Context, _ client.APIImpactRequest) (*client.APIImpactResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.impactResp, nil
}

func TestRunFederationHealthHappyPathText(t *testing.T) {
	c := &fakeFederationClient{healthResp: &client.FederationHealthResponse{
		WorkspaceID:        "ws-1",
		Reachable:          true,
		GateLatencyP95Ms:   1.2,
		ContractLinksCount: 5,
	}}
	var buf bytes.Buffer
	if err := RunFederationHealth(context.Background(), c, FederationHealthFlags{WorkspaceID: "ws-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunFederationHealth: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"workspace ws-1", "reachable:                 yes", "gate_latency_p95_ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}

func TestRunFederationHealthDaemonWideText(t *testing.T) {
	c := &fakeFederationClient{healthResp: &client.FederationHealthResponse{Reachable: true}}
	var buf bytes.Buffer
	if err := RunFederationHealth(context.Background(), c, FederationHealthFlags{Format: "text"}, &buf); err != nil {
		t.Fatalf("RunFederationHealth: %v", err)
	}
	if !strings.Contains(buf.String(), "daemon-wide") {
		t.Errorf("output = %q; want daemon-wide marker", buf.String())
	}
}

func TestRunFederationHealthHappyPathJSON(t *testing.T) {
	c := &fakeFederationClient{healthResp: &client.FederationHealthResponse{WorkspaceID: "ws-1", Reachable: true}}
	var buf bytes.Buffer
	if err := RunFederationHealth(context.Background(), c, FederationHealthFlags{WorkspaceID: "ws-1", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunFederationHealth: %v", err)
	}
	if !strings.Contains(buf.String(), `"reachable"`) {
		t.Errorf("JSON output = %q", buf.String())
	}
}

func TestRunFederationHealthCapaFirewallHint(t *testing.T) {
	c := &fakeFederationClient{err: fmt.Errorf("wrap: %w", store.ErrCrossProjectDenied)}
	var buf bytes.Buffer
	err := RunFederationHealth(context.Background(), c, FederationHealthFlags{WorkspaceID: "ws-1", Format: "text"}, &buf)
	if err == nil || !errors.Is(err, ErrRecoverable) || !strings.Contains(err.Error(), "workspace privacy policy") {
		t.Errorf("err = %v; want capa-firewall hint", err)
	}
}

func TestRunAPIImpactMissingDiffRef(t *testing.T) {
	c := &fakeFederationClient{}
	var buf bytes.Buffer
	err := RunAPIImpact(context.Background(), c, APIImpactFlags{Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<diff-ref> is required") {
		t.Errorf("err = %v", err)
	}
}

func TestRunAPIImpactHappyPathText(t *testing.T) {
	c := &fakeFederationClient{impactResp: &client.APIImpactResponse{
		DiffRef:       "HEAD~3..HEAD",
		WorkspaceID:   "ws-1",
		AffectedCount: 2,
		Consumers: []client.APIImpactConsumer{
			{Repo: "repo-b", CallID: "call-1", Severity: "BREAKING"},
			{Repo: "repo-c", CallID: "call-2", Severity: "DANGEROUS"},
		},
	}}
	var buf bytes.Buffer
	if err := RunAPIImpact(context.Background(), c, APIImpactFlags{DiffRef: "HEAD~3..HEAD", WorkspaceID: "ws-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunAPIImpact: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"HEAD~3..HEAD", "affected_count:  2", "repo-b/call-1", "BREAKING"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}

func TestRunAPIImpactEmptyConsumers(t *testing.T) {
	c := &fakeFederationClient{impactResp: &client.APIImpactResponse{
		DiffRef: "HEAD~1", AffectedCount: 0,
	}}
	var buf bytes.Buffer
	if err := RunAPIImpact(context.Background(), c, APIImpactFlags{DiffRef: "HEAD~1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunAPIImpact: %v", err)
	}
	if !strings.Contains(buf.String(), "no affected consumers") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunAPIImpactHappyPathJSON(t *testing.T) {
	c := &fakeFederationClient{impactResp: &client.APIImpactResponse{DiffRef: "x"}}
	var buf bytes.Buffer
	if err := RunAPIImpact(context.Background(), c, APIImpactFlags{DiffRef: "x", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunAPIImpact: %v", err)
	}
	if !strings.Contains(buf.String(), `"diff_ref"`) {
		t.Errorf("JSON output = %q", buf.String())
	}
}

func TestRunAPIImpactCapaFirewallHint(t *testing.T) {
	c := &fakeFederationClient{err: fmt.Errorf("wrap: %w", store.ErrUnauthorizedProject)}
	var buf bytes.Buffer
	err := RunAPIImpact(context.Background(), c, APIImpactFlags{DiffRef: "HEAD~1", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not on workspace roster") {
		t.Errorf("err = %v; want unauthorized-project hint", err)
	}
}
