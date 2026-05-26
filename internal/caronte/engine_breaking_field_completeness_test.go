//go:build cgo
// +build cgo

package caronte

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeFederationStore struct {
	workspace         FederationWorkspaceRow
	workspaceErr      error
	members           []FederationMemberRow
	membersErr        error
	policy            string
	policyErr         error
	links             []FederationLinkRow
	linksErr          error
	breakingChanges   []FederationBreakingChangeRow
	breakingChangeErr error
	bcWithConsumers   map[string]struct {
		row       FederationBreakingChangeRow
		consumers []FederationConsumerRow
		err       error
	}
	workspaces    []FederationWorkspaceRow
	workspacesErr error
}

func (f *fakeFederationStore) FederationGetWorkspace(_ context.Context, _ string) (FederationWorkspaceRow, error) {
	return f.workspace, f.workspaceErr
}
func (f *fakeFederationStore) FederationListWorkspaceMembers(_ context.Context, _ string) ([]FederationMemberRow, error) {
	return f.members, f.membersErr
}
func (f *fakeFederationStore) FederationGetWorkspacePolicy(_ context.Context, _ string) (string, error) {
	return f.policy, f.policyErr
}
func (f *fakeFederationStore) FederationListContractLinks(_ context.Context, _ string, _ int) ([]FederationLinkRow, error) {
	return f.links, f.linksErr
}
func (f *fakeFederationStore) FederationListRecentBreakingChanges(_ context.Context, _ string, _ int) ([]FederationBreakingChangeRow, error) {
	return f.breakingChanges, f.breakingChangeErr
}
func (f *fakeFederationStore) FederationGetBreakingChangeWithConsumers(_ context.Context, changeID string) (FederationBreakingChangeRow, []FederationConsumerRow, error) {
	if f.bcWithConsumers == nil {
		return FederationBreakingChangeRow{}, nil, nil
	}
	entry, ok := f.bcWithConsumers[changeID]
	if !ok {
		return FederationBreakingChangeRow{}, nil, nil
	}
	return entry.row, entry.consumers, entry.err
}
func (f *fakeFederationStore) FederationListWorkspaces(_ context.Context) ([]FederationWorkspaceRow, error) {
	return f.workspaces, f.workspacesErr
}

// TestGetBreakingChanges_PopulatesDetailAndLoreArrays is the I-1 sister-test:
// the engine's GetBreakingChanges op MUST populate Detail (json.RawMessage) +
// Lore.ADRRefs + Lore.Supersedes from the underlying federation row, mirroring
// the symmetry GetWhyBreakingChange already exhibits. A regression that drops
// any of the three fields would leave the MCP-facing payload silently empty
// despite the data being present in the store — the agent would observe
// incomplete attribution.
func TestGetBreakingChanges_PopulatesDetailAndLoreArrays(t *testing.T) {
	wantDetail := json.RawMessage(`{"removed_param":"x","severity":"BREAKING"}`)
	fake := &fakeFederationStore{
		breakingChanges: []FederationBreakingChangeRow{{
			ChangeID:       "bc-1",
			WorkspaceID:    "ws-1",
			EndpointID:     "ep-1",
			EndpointRepo:   "repo-a",
			Kind:           "param_added_required",
			Detail:         string(wantDetail),
			DetectedAt:     1700000000,
			DetectorID:     "oasdiff",
			LoreAuthor:     "alice",
			LoreCommitSHA:  "abc123",
			LoreADRRefs:    `["adr-0017","adr-0018"]`,
			LoreSupersedes: `["bc-0"]`,
		}},
		bcWithConsumers: map[string]struct {
			row       FederationBreakingChangeRow
			consumers []FederationConsumerRow
			err       error
		}{
			"bc-1": {consumers: []FederationConsumerRow{{ChangeID: "bc-1", CallID: "call-1", CallRepo: "repo-b"}}},
		},
	}
	e := &Engine{deps: Deps{FederationDB: fake}}

	got, err := e.GetBreakingChanges(context.Background(), "ws-1", 0)
	if err != nil {
		t.Fatalf("GetBreakingChanges: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d payloads; want 1", len(got))
	}
	p := got[0]

	if string(p.Detail) != string(wantDetail) {
		t.Errorf("Detail = %s; want %s", string(p.Detail), string(wantDetail))
	}
	if got, want := p.Lore.ADRRefs, []string{"adr-0017", "adr-0018"}; !equalStringSlice(got, want) {
		t.Errorf("Lore.ADRRefs = %v; want %v", got, want)
	}
	if got, want := p.Lore.Supersedes, []string{"bc-0"}; !equalStringSlice(got, want) {
		t.Errorf("Lore.Supersedes = %v; want %v", got, want)
	}
	if p.Lore.Author != "alice" || p.Lore.CommitSHA != "abc123" {
		t.Errorf("Lore Author/CommitSHA mismatch: got %q / %q; want alice / abc123", p.Lore.Author, p.Lore.CommitSHA)
	}
}

func TestGetBreakingChanges_ConsumerConfidenceAndLinkMethodEmptyUntilPhaseH(t *testing.T) {
	fake := &fakeFederationStore{
		breakingChanges: []FederationBreakingChangeRow{{
			ChangeID:    "bc-2",
			WorkspaceID: "ws-1",
			EndpointID:  "ep-2",
			Kind:        "field_removed",
		}},
		bcWithConsumers: map[string]struct {
			row       FederationBreakingChangeRow
			consumers []FederationConsumerRow
			err       error
		}{
			"bc-2": {consumers: []FederationConsumerRow{
				{ChangeID: "bc-2", CallID: "call-a", CallRepo: "repo-x"},
				{ChangeID: "bc-2", CallID: "call-b", CallRepo: "repo-y"},
			}},
		},
	}
	e := &Engine{deps: Deps{FederationDB: fake}}

	got, err := e.GetBreakingChanges(context.Background(), "ws-1", 0)
	if err != nil {
		t.Fatalf("GetBreakingChanges: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d payloads; want 1", len(got))
	}
	for i, c := range got[0].Consumers {
		if c.Confidence != "" {
			t.Errorf("Consumers[%d].Confidence = %q; want empty until phase h", i, c.Confidence)
		}
		if c.LinkMethod != "" {
			t.Errorf("Consumers[%d].LinkMethod = %q; want empty until phase h", i, c.LinkMethod)
		}
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEngineOps_PreservesErrFederationUnavailableType(t *testing.T) {
	if !errors.Is(ErrFederationUnavailable, ErrFederationUnavailable) {
		t.Fatalf("ErrFederationUnavailable lost identity under errors.Is")
	}
}
