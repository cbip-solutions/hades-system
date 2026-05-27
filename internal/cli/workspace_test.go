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

type auditEventCapture struct {
	EventType string
	Payload   map[string]any
}

type fakeWorkspaceClient struct {
	initResp        *client.WorkspaceInitResponse
	listResp        *client.WorkspaceListResponse
	membersResp     *client.WorkspaceMembersResponse
	linkResp        *client.WorkspaceLinkResponse
	removeResp      *client.WorkspaceRemoveResponse
	policyGetResp   *client.WorkspacePolicyGetResponse
	policyChange    *client.WorkspacePolicySetResponse
	auditEvents     []auditEventCapture
	policyAPICalled bool
	err             error
}

func (f *fakeWorkspaceClient) WorkspaceInit(_ context.Context, _ client.WorkspaceInitRequest) (*client.WorkspaceInitResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.initResp, nil
}
func (f *fakeWorkspaceClient) WorkspaceList(_ context.Context, _ client.WorkspaceListRequest) (*client.WorkspaceListResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.listResp, nil
}
func (f *fakeWorkspaceClient) WorkspaceMembers(_ context.Context, _ client.WorkspaceMembersRequest) (*client.WorkspaceMembersResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.membersResp, nil
}
func (f *fakeWorkspaceClient) WorkspaceLink(_ context.Context, _ client.WorkspaceLinkRequest) (*client.WorkspaceLinkResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.linkResp, nil
}
func (f *fakeWorkspaceClient) WorkspaceRemove(_ context.Context, _ client.WorkspaceRemoveRequest) (*client.WorkspaceRemoveResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.removeResp, nil
}
func (f *fakeWorkspaceClient) WorkspacePolicyGet(_ context.Context, _ client.WorkspacePolicyGetRequest) (*client.WorkspacePolicyGetResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.policyGetResp, nil
}
func (f *fakeWorkspaceClient) WorkspacePolicySet(_ context.Context, _ client.WorkspacePolicySetRequest) (*client.WorkspacePolicySetResponse, error) {
	f.policyAPICalled = true
	if f.err != nil {
		return nil, f.err
	}
	return f.policyChange, nil
}
func (f *fakeWorkspaceClient) EmitAudit(_ context.Context, eventType string, payload map[string]any) error {
	f.auditEvents = append(f.auditEvents, auditEventCapture{eventType, payload})
	return nil
}

func TestRunWorkspaceInitMissingWorkspaceID(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	err := RunWorkspaceInit(context.Background(), c, WorkspaceInitFlags{OwningProject: "proj-a", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<workspace_id> is required") {
		t.Errorf("err = %v; want validation error", err)
	}
}

func TestRunWorkspaceInitMissingOwner(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	err := RunWorkspaceInit(context.Background(), c, WorkspaceInitFlags{WorkspaceID: "ws-1", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "--owner is required") {
		t.Errorf("err = %v; want owner-required validation error", err)
	}
}

func TestRunWorkspaceInitHappyPathText(t *testing.T) {
	c := &fakeWorkspaceClient{initResp: &client.WorkspaceInitResponse{
		WorkspaceID: "ws-1", CreatedAt: 1700000000, SchemaVersion: 1,
	}}
	var buf bytes.Buffer
	err := RunWorkspaceInit(context.Background(), c, WorkspaceInitFlags{
		WorkspaceID: "ws-1", OwningProject: "proj-a", Format: "text",
	}, &buf)
	if err != nil {
		t.Fatalf("RunWorkspaceInit: %v", err)
	}
	if !strings.Contains(buf.String(), "ws-1") || !strings.Contains(buf.String(), "schema v1") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspaceInitHappyPathJSON(t *testing.T) {
	c := &fakeWorkspaceClient{initResp: &client.WorkspaceInitResponse{WorkspaceID: "ws-1"}}
	var buf bytes.Buffer
	err := RunWorkspaceInit(context.Background(), c, WorkspaceInitFlags{
		WorkspaceID: "ws-1", OwningProject: "proj-a", Format: "json",
	}, &buf)
	if err != nil {
		t.Fatalf("RunWorkspaceInit: %v", err)
	}
	if !strings.Contains(buf.String(), `"workspace_id"`) {
		t.Errorf("JSON output = %q", buf.String())
	}
}

func TestRunWorkspaceListHappyPathText(t *testing.T) {
	c := &fakeWorkspaceClient{listResp: &client.WorkspaceListResponse{
		Workspaces: []client.WorkspaceListEntry{
			{WorkspaceID: "ws-1", OwningProject: "proj-a", PolicyLocked: true, CreatedAt: 1700000000, SchemaVersion: 1},
		},
	}}
	var buf bytes.Buffer
	err := RunWorkspaceList(context.Background(), c, WorkspaceListFlags{Format: "text"}, &buf)
	if err != nil {
		t.Fatalf("RunWorkspaceList: %v", err)
	}
	if !strings.Contains(buf.String(), "ws-1") || !strings.Contains(buf.String(), "yes") {
		t.Errorf("output = %q; want ws-1 + yes (locked)", buf.String())
	}
	if !strings.Contains(buf.String(), "SCHEMA") || !strings.Contains(buf.String(), "1") {
		t.Errorf("output = %q; want schema-version column", buf.String())
	}
}

func TestRunWorkspaceListEmpty(t *testing.T) {
	c := &fakeWorkspaceClient{listResp: &client.WorkspaceListResponse{}}
	var buf bytes.Buffer
	if err := RunWorkspaceList(context.Background(), c, WorkspaceListFlags{Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWorkspaceList: %v", err)
	}
	if !strings.Contains(buf.String(), "no workspaces") {
		t.Errorf("output = %q; want empty marker", buf.String())
	}
}

func TestRunWorkspaceMembersMissingWorkspaceID(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	err := RunWorkspaceMembers(context.Background(), c, WorkspaceMembersFlags{Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<workspace_id> is required") {
		t.Errorf("err = %v", err)
	}
}

func TestRunWorkspaceMembersHappyPathText(t *testing.T) {
	c := &fakeWorkspaceClient{membersResp: &client.WorkspaceMembersResponse{
		Members: []client.WorkspaceMemberRow{
			{WorkspaceID: "ws-1", ProjectID: "proj-a", RegisteredAt: 1700000000},
		},
	}}
	var buf bytes.Buffer
	if err := RunWorkspaceMembers(context.Background(), c, WorkspaceMembersFlags{WorkspaceID: "ws-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWorkspaceMembers: %v", err)
	}
	if !strings.Contains(buf.String(), "proj-a") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspaceLinkHappyPathText(t *testing.T) {
	c := &fakeWorkspaceClient{linkResp: &client.WorkspaceLinkResponse{
		WorkspaceID: "ws-1", ProjectID: "proj-c", RegisteredAt: 1700000200,
	}}
	var buf bytes.Buffer
	if err := RunWorkspaceLink(context.Background(), c, WorkspaceLinkFlags{
		WorkspaceID: "ws-1", ProjectID: "proj-c", Format: "text",
	}, &buf); err != nil {
		t.Fatalf("RunWorkspaceLink: %v", err)
	}
	if !strings.Contains(buf.String(), "proj-c") || !strings.Contains(buf.String(), "ws-1") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspaceLinkMissingProject(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	err := RunWorkspaceLink(context.Background(), c, WorkspaceLinkFlags{WorkspaceID: "ws-1", Format: "text"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "<project_id> is required") {
		t.Errorf("err = %v", err)
	}
}

func TestRunWorkspaceRemoveNonInteractiveRequiresYes(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspaceRemove(context.Background(), c, WorkspaceRemoveFlags{
		WorkspaceID: "ws-1", Format: "text",
	}, in, &buf, false)
	if err == nil || !strings.Contains(err.Error(), "--yes required in non-interactive mode") {
		t.Errorf("err = %v", err)
	}
}

func TestRunWorkspaceRemoveWithYesBypass(t *testing.T) {
	c := &fakeWorkspaceClient{removeResp: &client.WorkspaceRemoveResponse{
		WorkspaceID: "ws-1", RowsAffected: 1,
	}}
	var buf bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspaceRemove(context.Background(), c, WorkspaceRemoveFlags{
		WorkspaceID: "ws-1", AssumeYes: true, Format: "text",
	}, in, &buf, false)
	if err != nil {
		t.Fatalf("RunWorkspaceRemove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspaceRemoveConfirmYes(t *testing.T) {
	c := &fakeWorkspaceClient{removeResp: &client.WorkspaceRemoveResponse{
		WorkspaceID: "ws-1", RowsAffected: 1,
	}}
	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := RunWorkspaceRemove(context.Background(), c, WorkspaceRemoveFlags{
		WorkspaceID: "ws-1", Format: "text",
	}, in, &buf, true)
	if err != nil {
		t.Fatalf("RunWorkspaceRemove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspaceRemoveConfirmNoAborts(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var buf bytes.Buffer
	in := strings.NewReader("n\n")
	err := RunWorkspaceRemove(context.Background(), c, WorkspaceRemoveFlags{
		WorkspaceID: "ws-1", Format: "text",
	}, in, &buf, true)
	if err != nil {
		t.Fatalf("RunWorkspaceRemove: %v", err)
	}
	if !strings.Contains(buf.String(), "aborted") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspacePolicyGetEmptyPolicy(t *testing.T) {
	c := &fakeWorkspaceClient{policyGetResp: &client.WorkspacePolicyGetResponse{WorkspaceID: "ws-1", Policy: ""}}
	var buf bytes.Buffer
	if err := RunWorkspacePolicyGet(context.Background(), c, WorkspacePolicyGetFlags{WorkspaceID: "ws-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWorkspacePolicyGet: %v", err)
	}
	if !strings.Contains(buf.String(), "no policy set") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspacePolicyGetWithPolicy(t *testing.T) {
	c := &fakeWorkspaceClient{policyGetResp: &client.WorkspacePolicyGetResponse{
		WorkspaceID: "ws-1", Policy: `{"locked":true}`,
	}}
	var buf bytes.Buffer
	if err := RunWorkspacePolicyGet(context.Background(), c, WorkspacePolicyGetFlags{WorkspaceID: "ws-1", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWorkspacePolicyGet: %v", err)
	}
	if !strings.Contains(buf.String(), `"locked":true`) {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRunWorkspacePolicySetConfirmYesEmitsTwoAuditRows(t *testing.T) {
	c := &fakeWorkspaceClient{policyChange: &client.WorkspacePolicySetResponse{
		WorkspaceID: "ws-1", NewPolicy: "locked",
	}}
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "locked", Format: "text",
	}, in, &out, true)
	if err != nil {
		t.Fatalf("RunWorkspacePolicySet: %v", err)
	}
	if len(c.auditEvents) != 2 {
		t.Fatalf("audit events len = %d; want 2", len(c.auditEvents))
	}
	if c.auditEvents[0].EventType != "policy_change_requested" {
		t.Errorf("audit[0] = %q; want policy_change_requested", c.auditEvents[0].EventType)
	}
	if c.auditEvents[1].EventType != "policy_change_committed" {
		t.Errorf("audit[1] = %q; want policy_change_committed", c.auditEvents[1].EventType)
	}
	if !c.policyAPICalled {
		t.Error("API not called after confirm; want called")
	}
}

func TestRunWorkspacePolicySetConfirmNoAbortsButAudits(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var out bytes.Buffer
	in := strings.NewReader("n\n")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "locked", Format: "text",
	}, in, &out, true)
	if err != nil {
		t.Fatalf("RunWorkspacePolicySet: %v", err)
	}
	if len(c.auditEvents) != 2 {
		t.Fatalf("audit events len = %d; want 2", len(c.auditEvents))
	}
	if c.auditEvents[1].EventType != "policy_change_aborted" {
		t.Errorf("audit[1] = %q; want policy_change_aborted", c.auditEvents[1].EventType)
	}
	if c.policyAPICalled {
		t.Error("API called after abort; want NOT called")
	}
}

func TestRunWorkspacePolicySetYesFlagBypasses(t *testing.T) {
	c := &fakeWorkspaceClient{policyChange: &client.WorkspacePolicySetResponse{
		WorkspaceID: "ws-1", NewPolicy: "locked",
	}}
	var out bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "locked", AssumeYes: true, Format: "text",
	}, in, &out, true)
	if err != nil {
		t.Fatalf("RunWorkspacePolicySet: %v", err)
	}
	if len(c.auditEvents) != 2 || c.auditEvents[1].EventType != "policy_change_committed" {
		t.Errorf("audit events = %v; want 2 events ending in committed", c.auditEvents)
	}
	if !c.policyAPICalled {
		t.Error("API not called with --yes; want called")
	}
}

func TestRunWorkspacePolicySetNonInteractiveRequiresYes(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var out bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "locked", AssumeYes: false, Format: "text",
	}, in, &out, false)
	if err == nil || !strings.Contains(err.Error(), "--yes required in non-interactive mode") {
		t.Errorf("err = %v; want --yes-required validation error", err)
	}
	if c.policyAPICalled {
		t.Error("API called in non-interactive without --yes; want NOT called")
	}
}

func TestRunWorkspacePolicySetNonInteractiveWithYesProceeds(t *testing.T) {
	c := &fakeWorkspaceClient{policyChange: &client.WorkspacePolicySetResponse{
		WorkspaceID: "ws-1", NewPolicy: "permissive",
	}}
	var out bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "permissive", AssumeYes: true, Format: "text",
	}, in, &out, false)
	if err != nil {
		t.Fatalf("RunWorkspacePolicySet: %v", err)
	}
	if len(c.auditEvents) != 2 {
		t.Errorf("audit events len = %d; want 2", len(c.auditEvents))
	}
	if !c.policyAPICalled {
		t.Error("API not called with --yes; want called")
	}
}

func TestRunWorkspacePolicySetInvalidPolicy(t *testing.T) {
	c := &fakeWorkspaceClient{}
	var out bytes.Buffer
	in := strings.NewReader("")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "bogus", AssumeYes: true, Format: "text",
	}, in, &out, true)
	if err == nil || !strings.Contains(err.Error(), "must be locked|permissive") {
		t.Errorf("err = %v; want policy validation error", err)
	}
}

func TestRunWorkspacePolicySetCapaFirewallHint(t *testing.T) {
	c := &fakeWorkspaceClient{err: fmt.Errorf("wrap: %w", store.ErrCrossProjectDenied)}
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	err := RunWorkspacePolicySet(context.Background(), c, WorkspacePolicySetFlags{
		WorkspaceID: "ws-1", Policy: "locked", Format: "text",
	}, in, &out, true)
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v; want recoverable capa-firewall hint", err)
	}
	if !strings.Contains(err.Error(), "workspace privacy policy") {
		t.Errorf("err = %v; want actionable hint", err)
	}

	if len(c.auditEvents) < 2 {
		t.Fatalf("audit events = %v; want at least 2 (request + failure)", c.auditEvents)
	}
	if got, want := c.auditEvents[0].EventType, "policy_change_requested"; got != want {
		t.Errorf("auditEvents[0].EventType = %q; want %q", got, want)
	}
	if got, want := c.auditEvents[1].EventType, "policy_change_failed"; got != want {
		t.Errorf("auditEvents[1].EventType = %q; want %q (4th event-type enumerated in workspace.go header)", got, want)
	}
}
