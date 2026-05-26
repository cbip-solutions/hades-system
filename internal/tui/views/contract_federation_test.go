package views

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeContractFederationClient struct {
	workspaces []WorkspaceRow
	wsErr      error
	breaking   []BreakingChangeRow
	bcErr      error
	dispatch   []DispatchDecisionRow
	ddErr      error
}

func (f *fakeContractFederationClient) ListWorkspaces(_ context.Context) ([]WorkspaceRow, error) {
	return f.workspaces, f.wsErr
}
func (f *fakeContractFederationClient) ListRecentBreakingChanges(_ context.Context, _ int) ([]BreakingChangeRow, error) {
	return f.breaking, f.bcErr
}
func (f *fakeContractFederationClient) ListRecentDispatchDecisions(_ context.Context, _ int) ([]DispatchDecisionRow, error) {
	return f.dispatch, f.ddErr
}

func TestContractFederationView_Empty_RendersAllThreeSectionsMuted(t *testing.T) {
	v := NewContractFederationView(nil)
	out := v.View()
	if !strings.Contains(out, "CONTRACT FEDERATION") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "workspaces") {
		t.Errorf("missing roster section label: %q", out)
	}
	if !strings.Contains(out, "(no workspaces registered)") {
		t.Errorf("missing roster empty-state: %q", out)
	}
	if !strings.Contains(out, "recent BREAKING") {
		t.Errorf("missing breaking section label: %q", out)
	}
	if !strings.Contains(out, "(no recent breaking changes)") {
		t.Errorf("missing breaking empty-state: %q", out)
	}
	if !strings.Contains(out, "L10 dispatch") {
		t.Errorf("missing dispatch section label: %q", out)
	}
	if !strings.Contains(out, "(no dispatch decisions yet)") {
		t.Errorf("missing dispatch empty-state: %q", out)
	}
}

func TestContractFederationView_FullData_RendersAllThreeSectionsWithRows(t *testing.T) {
	c := &fakeContractFederationClient{
		workspaces: []WorkspaceRow{
			{WorkspaceID: "ws-alpha", Members: []string{"proj-a", "proj-b"}, Policy: "locked"},
		},
		breaking: []BreakingChangeRow{
			{
				ChangeID:       "ch-001",
				BreakingKind:   "param_added_required",
				Severity:       "high",
				SourceEndpoint: "GET /v1/items/{id}",
				LoreAuthor:     "alice",
				LoreCommitSHA:  "abc1234deadbeef",
				DetectedAt:     time.Unix(1700000000, 0),
			},
		},
		dispatch: []DispatchDecisionRow{
			{
				ChangeID:        "ch-001",
				Mode:            "Surface",
				DispatchedRepos: []string{"proj-x", "proj-y"},
				AuditID:         "leaf-42",
				DecidedAt:       time.Unix(1700000010, 0),
			},
		},
	}
	v := NewContractFederationView(c)

	v.workspaces = c.workspaces
	v.breaking = c.breaking
	v.dispatch = c.dispatch

	out := v.View()
	if !strings.Contains(out, "ws-alpha") || !strings.Contains(out, "locked") {
		t.Errorf("missing workspace row: %q", out)
	}
	if !strings.Contains(out, "ch-001") || !strings.Contains(out, "param_added_required") {
		t.Errorf("missing breaking-change row: %q", out)
	}
	if !strings.Contains(out, "alice@abc1234") {
		t.Errorf("missing Lore attribution (alice@abc1234): %q", out)
	}
	if !strings.Contains(out, "mode=Surface") || !strings.Contains(out, "proj-x,proj-y") {
		t.Errorf("missing dispatch decision row: %q", out)
	}
	if !strings.Contains(out, "audit=leaf-42") {
		t.Errorf("missing dispatch audit-id: %q", out)
	}
}

func TestContractFederationView_Update_AppliesContractFederationDataMsg(t *testing.T) {
	v := NewContractFederationView(nil)
	msg := contractFederationDataMsg{
		workspaces: []WorkspaceRow{{WorkspaceID: "ws-1", Policy: "permissive"}},
		breaking:   []BreakingChangeRow{{ChangeID: "ch-9", BreakingKind: "removed_field"}},
		dispatch:   []DispatchDecisionRow{{ChangeID: "ch-9", Mode: "Autonomy", AuditID: "leaf-9"}},
	}
	v2, _ := v.Update(msg)
	got, ok := v2.(*ContractFederationView)
	if !ok {
		t.Fatalf("Update returned non-*ContractFederationView: %T", v2)
	}
	out := got.View()
	if !strings.Contains(out, "ws-1") || !strings.Contains(out, "ch-9") || !strings.Contains(out, "mode=Autonomy") {
		t.Errorf("Update did not apply msg payload: %q", out)
	}
}

func TestContractFederationView_Update_AppliesLastErr(t *testing.T) {
	v := NewContractFederationView(nil)
	msg := contractFederationDataMsg{err: errors.New("daemon unreachable")}
	v2, _ := v.Update(msg)
	out := v2.View()
	if !strings.Contains(out, "daemon unreachable") {
		t.Errorf("error-state render missing the error message: %q", out)
	}

	if strings.Contains(out, "(no workspaces registered)") {
		t.Errorf("error state must NOT render the empty roster line: %q", out)
	}
}

func TestContractFederationView_Breaking_RendersLoreAttribution(t *testing.T) {
	v := NewContractFederationView(nil)
	v.breaking = []BreakingChangeRow{
		{
			ChangeID:       "ch-7",
			BreakingKind:   "removed_endpoint",
			Severity:       "critical",
			SourceEndpoint: "POST /v1/widgets",
			LoreAuthor:     "bob",
			LoreCommitSHA:  "0123456789abcdef",
		},
	}
	out := v.View()
	if !strings.Contains(out, "bob@0123456") {
		t.Errorf("Lore-attribution preview missing (bob@0123456): %q", out)
	}
}

func TestContractFederationView_Breaking_NoLore_RendersMutedFallback(t *testing.T) {
	v := NewContractFederationView(nil)
	v.breaking = []BreakingChangeRow{
		{ChangeID: "ch-8", BreakingKind: "kind", Severity: "low", SourceEndpoint: "GET /x"},
	}
	out := v.View()
	if !strings.Contains(out, "(no Lore evidence)") {
		t.Errorf("missing muted fallback for empty Lore attribution: %q", out)
	}
}

func TestContractFederationView_Dispatch_EmptyReposRendersNonePlaceholder(t *testing.T) {
	v := NewContractFederationView(nil)
	v.dispatch = []DispatchDecisionRow{
		{ChangeID: "ch-9", Mode: "Surface", DispatchedRepos: nil, AuditID: "leaf-x"},
	}
	out := v.View()
	if !strings.Contains(out, "repos=[(none)]") {
		t.Errorf("missing repos=[(none)] for empty DispatchedRepos: %q", out)
	}
}

func TestContractFederationView_Refetch_NilClientReturnsEmptyMsg(t *testing.T) {
	v := NewContractFederationView(nil)
	cmd := v.Refetch()
	if cmd == nil {
		t.Fatal("Refetch returned nil cmd (must return a tea.Cmd even when client is nil)")
	}
	msg := cmd()
	got, ok := msg.(contractFederationDataMsg)
	if !ok {
		t.Fatalf("expected contractFederationDataMsg, got %T", msg)
	}
	if len(got.workspaces) != 0 || len(got.breaking) != 0 || len(got.dispatch) != 0 || got.err != nil {
		t.Errorf("nil-client Refetch must return zero-payload msg, got %+v", got)
	}
}

func TestContractFederationView_Refetch_ClientCallsAllThreeMethods(t *testing.T) {
	c := &fakeContractFederationClient{
		workspaces: []WorkspaceRow{{WorkspaceID: "ws-1"}},
		breaking:   []BreakingChangeRow{{ChangeID: "ch-1"}},
		dispatch:   []DispatchDecisionRow{{ChangeID: "ch-1", Mode: "Surface"}},
	}
	v := NewContractFederationView(c)
	cmd := v.Refetch()
	if cmd == nil {
		t.Fatal("Refetch returned nil cmd")
	}
	msg := cmd()
	got, ok := msg.(contractFederationDataMsg)
	if !ok {
		t.Fatalf("expected contractFederationDataMsg, got %T", msg)
	}
	if got.err != nil {
		t.Fatalf("unexpected err: %v", got.err)
	}
	if len(got.workspaces) != 1 || got.workspaces[0].WorkspaceID != "ws-1" {
		t.Errorf("missing workspaces: %+v", got.workspaces)
	}
	if len(got.breaking) != 1 || got.breaking[0].ChangeID != "ch-1" {
		t.Errorf("missing breaking: %+v", got.breaking)
	}
	if len(got.dispatch) != 1 || got.dispatch[0].Mode != "Surface" {
		t.Errorf("missing dispatch: %+v", got.dispatch)
	}
}

func TestContractFederationView_Refetch_FirstErrorWinsGracefulDegrade(t *testing.T) {
	c := &fakeContractFederationClient{
		wsErr:    errors.New("ws fail"),
		bcErr:    errors.New("bc fail"),
		ddErr:    errors.New("dd fail"),
		dispatch: []DispatchDecisionRow{{ChangeID: "ch-1"}},
	}
	v := NewContractFederationView(c)
	cmd := v.Refetch()
	msg := cmd()
	got := msg.(contractFederationDataMsg)
	if got.err == nil {
		t.Fatal("expected non-nil err")
	}
	if !strings.Contains(got.err.Error(), "ws fail") {
		t.Errorf("expected first error to be 'ws fail', got %q", got.err.Error())
	}
}

func TestContractFederationView_Init_ReturnsNil(t *testing.T) {
	v := NewContractFederationView(nil)
	if cmd := v.Init(); cmd != nil {
		t.Errorf("Init must return nil; got non-nil tea.Cmd")
	}
}

func TestContractFederationView_Breaking_RendersDetectedAtField(t *testing.T) {
	v := NewContractFederationView(nil)

	v.breaking = []BreakingChangeRow{
		{
			ChangeID:       "ch-time-1",
			BreakingKind:   "removed_endpoint",
			Severity:       "high",
			SourceEndpoint: "GET /v1/items",
			LoreAuthor:     "alice",
			LoreCommitSHA:  "abc1234",
			DetectedAt:     time.Now().Add(-5 * time.Minute),
		},
	}
	out := v.View()

	if !strings.Contains(out, "ago") {
		t.Errorf("missing relative-time render fragment 'ago' for DetectedAt: %q", out)
	}
}

func TestContractFederationView_Dispatch_RendersDecidedAtField(t *testing.T) {
	v := NewContractFederationView(nil)
	v.dispatch = []DispatchDecisionRow{
		{
			ChangeID:        "ch-time-2",
			Mode:            "Surface",
			DispatchedRepos: []string{"proj-r"},
			AuditID:         "leaf-time-2",
			DecidedAt:       time.Now().Add(-30 * time.Second),
		},
	}
	out := v.View()
	if !strings.Contains(out, "ago") {
		t.Errorf("missing relative-time render fragment 'ago' for DecidedAt: %q", out)
	}
}

func TestRelativeTimeRender_ZeroValueReturnsUnknown(t *testing.T) {
	got := relativeTimeRender(time.Time{})
	if got != "(unknown)" {
		t.Errorf("relativeTimeRender(zero) = %q; want %q (IsZero guard regressed)", got, "(unknown)")
	}
}

func TestRelativeTimeRender_FutureTimestampReturnsJustNow(t *testing.T) {
	future := time.Now().Add(30 * time.Second)
	got := relativeTimeRender(future)
	if got != "just now" {
		t.Errorf("relativeTimeRender(future +30s) = %q; want %q (negative-skew guard regressed)", got, "just now")
	}
}

func TestContractFederationView_Update_UnknownMsgNoOp(t *testing.T) {
	v := NewContractFederationView(nil)
	v.workspaces = []WorkspaceRow{{WorkspaceID: "pinned"}}
	v2, cmd := v.Update(tea.KeyMsg{})
	if cmd != nil {
		t.Errorf("unknown msg must not produce a cmd; got %T", cmd)
	}
	got := v2.(*ContractFederationView)
	if len(got.workspaces) != 1 || got.workspaces[0].WorkspaceID != "pinned" {
		t.Errorf("unknown msg must not mutate state; lost workspaces: %+v", got.workspaces)
	}
}
