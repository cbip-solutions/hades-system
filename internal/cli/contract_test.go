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

type fakeContractClient struct {
	contractResp *client.ContractResponse
	validateResp *client.ContractValidateResponse
	whyResp      *client.ContractWhyResponse
	err          error
}

func (f *fakeContractClient) Contract(_ context.Context, _ client.ContractRequest) (*client.ContractResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.contractResp, nil
}
func (f *fakeContractClient) ContractValidate(_ context.Context, _ client.ContractValidateRequest) (*client.ContractValidateResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.validateResp, nil
}
func (f *fakeContractClient) ContractWhy(_ context.Context, _ client.ContractWhyRequest) (*client.ContractWhyResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.whyResp, nil
}

func TestRunContractMissingEndpoint(t *testing.T) {
	c := &fakeContractClient{}
	var buf bytes.Buffer
	err := RunContract(context.Background(), c, ContractFlags{Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<endpoint> is required") {
		t.Errorf("err = %v; want validation error", err)
	}
}

func TestRunContractInvalidFormat(t *testing.T) {
	c := &fakeContractClient{}
	var buf bytes.Buffer
	err := RunContract(context.Background(), c, ContractFlags{Endpoint: "x", Format: "yaml"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "must be text or json") {
		t.Errorf("err = %v; want format validation error", err)
	}
}

func TestRunContractHappyPathText(t *testing.T) {
	c := &fakeContractClient{contractResp: &client.ContractResponse{
		EndpointID: "endpoint-1", Method: "GET", PathTemplate: "/users/{id}",
		HandlerNodeID: "node-1", ExtractorID: "oasdiff", ExtractedAt: 1700000000,
	}}
	var buf bytes.Buffer
	if err := RunContract(context.Background(), c, ContractFlags{Endpoint: "endpoint-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunContract: %v", err)
	}
	if !strings.Contains(buf.String(), "endpoint-1") || !strings.Contains(buf.String(), "/users/{id}") {
		t.Errorf("output = %q; want endpoint-1 + path template", buf.String())
	}
}

func TestRunContractHappyPathJSON(t *testing.T) {
	c := &fakeContractClient{contractResp: &client.ContractResponse{EndpointID: "endpoint-1"}}
	var buf bytes.Buffer
	if err := RunContract(context.Background(), c, ContractFlags{Endpoint: "endpoint-1", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunContract: %v", err)
	}
	if !strings.Contains(buf.String(), `"endpoint_id"`) {
		t.Errorf("JSON output = %q; want endpoint_id field", buf.String())
	}
}

func TestRunContractCapaFirewallHintRendered(t *testing.T) {
	c := &fakeContractClient{err: fmt.Errorf("wrap: %w", store.ErrCrossProjectDenied)}
	var buf bytes.Buffer
	err := RunContract(context.Background(), c, ContractFlags{Endpoint: "endpoint-1", Format: "text"}, &buf)
	if err == nil {
		t.Fatal("RunContract returned nil; want capa-firewall hint")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v; want wraps ErrRecoverable", err)
	}
	if !strings.Contains(err.Error(), "workspace privacy policy") {
		t.Errorf("err msg = %q; want actionable hint", err.Error())
	}
}

func TestRunContractUnauthorizedProjectHint(t *testing.T) {
	c := &fakeContractClient{err: fmt.Errorf("wrap: %w", store.ErrUnauthorizedProject)}
	var buf bytes.Buffer
	err := RunContract(context.Background(), c, ContractFlags{Endpoint: "endpoint-1", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not on workspace roster") {
		t.Errorf("err = %v; want unauthorized-project hint", err)
	}
}

func TestRunContractValidateMissingRepo(t *testing.T) {
	c := &fakeContractClient{}
	var buf bytes.Buffer
	err := RunContractValidate(context.Background(), c, ContractValidateFlags{Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<repo> is required") {
		t.Errorf("err = %v; want validation error", err)
	}
}

func TestRunContractValidateHappyPathText(t *testing.T) {
	c := &fakeContractClient{validateResp: &client.ContractValidateResponse{
		Valid: true, SchemaVersion: 1,
		Services: []client.ContractValidateService{{BaseURLRef: "${X}", TargetRepo: "repo-b"}},
	}}
	var buf bytes.Buffer
	if err := RunContractValidate(context.Background(), c, ContractValidateFlags{Repo: "/r", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunContractValidate: %v", err)
	}
	if !strings.Contains(buf.String(), "valid") || !strings.Contains(buf.String(), "repo-b") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunContractValidateInvalidReturnsError(t *testing.T) {
	c := &fakeContractClient{validateResp: &client.ContractValidateResponse{
		Valid: false, Errors: []client.ContractValidateError{{Code: "schema_version_missing", Message: "x"}},
	}}
	var buf bytes.Buffer
	err := RunContractValidate(context.Background(), c, ContractValidateFlags{Repo: "/r", Format: "text"}, &buf)
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v; want recoverable validate-failed error", err)
	}
	if !strings.Contains(buf.String(), "schema_version_missing") {
		t.Errorf("output = %q; want error code in body", buf.String())
	}
}

func TestRunContractValidateJSON(t *testing.T) {
	c := &fakeContractClient{validateResp: &client.ContractValidateResponse{Valid: true, SchemaVersion: 1}}
	var buf bytes.Buffer
	if err := RunContractValidate(context.Background(), c, ContractValidateFlags{Repo: "/r", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunContractValidate: %v", err)
	}
	if !strings.Contains(buf.String(), `"schema_version"`) {
		t.Errorf("JSON output = %q", buf.String())
	}
}

func TestRunContractWhyMissingChangeID(t *testing.T) {
	c := &fakeContractClient{}
	var buf bytes.Buffer
	err := RunContractWhy(context.Background(), c, ContractWhyFlags{Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<change_id> is required") {
		t.Errorf("err = %v; want validation error", err)
	}
}

func TestRunContractWhyHappyPathText(t *testing.T) {
	c := &fakeContractClient{whyResp: &client.ContractWhyResponse{
		ChangeID: "chg-1", EndpointID: "endpoint-1", EndpointRepo: "repo-a",
		LoreAuthor: "alice@example.com", LoreCommitSHA: "abc1234567",
		LoreADRRefs: []string{"ADR-0114"},
	}}
	var buf bytes.Buffer
	if err := RunContractWhy(context.Background(), c, ContractWhyFlags{ChangeID: "chg-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunContractWhy: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"chg-1", "alice@example.com", "ADR-0114"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}

func TestRunContractWhyHappyPathJSON(t *testing.T) {
	c := &fakeContractClient{whyResp: &client.ContractWhyResponse{ChangeID: "chg-1"}}
	var buf bytes.Buffer
	if err := RunContractWhy(context.Background(), c, ContractWhyFlags{ChangeID: "chg-1", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunContractWhy: %v", err)
	}
	if !strings.Contains(buf.String(), `"change_id"`) {
		t.Errorf("JSON output = %q", buf.String())
	}
}

func TestRunContractWhyCapaFirewallHint(t *testing.T) {
	c := &fakeContractClient{err: fmt.Errorf("wrap: %w", store.ErrCrossProjectDenied)}
	var buf bytes.Buffer
	err := RunContractWhy(context.Background(), c, ContractWhyFlags{ChangeID: "chg-1", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "workspace privacy policy") {
		t.Errorf("err = %v; want capa-firewall hint", err)
	}
}
