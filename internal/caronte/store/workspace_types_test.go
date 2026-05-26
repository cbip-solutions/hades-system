package store

import (
	"reflect"
	"testing"
)

func TestFederatedQueryFieldSet(t *testing.T) {
	q := FederatedQuery{Kind: "contract_link", Scope: []string{"p1", "p2"}, NormalizedKey: "GET /users/{param}", Limit: 10}
	if q.Kind == "" || len(q.Scope) != 2 {
		t.Fatal("FederatedQuery field set incomplete")
	}
}

func TestFederatedResultLinkNilable(t *testing.T) {
	var r FederatedResult
	if r.Link != nil {
		t.Error("zero-value FederatedResult.Link must be nil (Plan 19 ships no links)")
	}
	r = FederatedResult{ProjectID: "p1", Kind: "contract_link", Link: &ContractLink{CallID: "c", EndpointID: "e", WorkspaceID: "w"}}
	if r.Link == nil || r.Link.CallID != "c" {
		t.Error("FederatedResult.Link must be a settable *ContractLink")
	}
}

func TestContractLinkFieldSet(t *testing.T) {
	l := ContractLink{CallID: "c", CallRepo: "a", EndpointID: "e", EndpointRepo: "b", Confidence: "spec_artifact", WorkspaceID: "w"}
	if l.CallRepo == "" || l.EndpointRepo == "" || l.WorkspaceID == "" {
		t.Fatal("ContractLink field set incomplete")
	}
}

func TestWorkspacePolicyInterface(t *testing.T) {
	var p WorkspacePolicy = stubPolicy{locked: true}
	if !p.PrivacyLocked() {
		t.Error("stubPolicy{locked:true}.PrivacyLocked() = false; want true")
	}
}

type stubPolicy struct{ locked bool }

func (s stubPolicy) PrivacyLocked() bool { return s.locked }

func TestContractLinkFieldCount(t *testing.T) {
	rv := reflect.ValueOf(ContractLink{})
	if got, want := rv.NumField(), 8; got != want {
		t.Errorf("ContractLink field count = %d; want %d (Plan 19 M 6 + Plan 20 A 2)", got, want)
	}
}

func TestContractLinkAdditiveFieldsZeroDefault(t *testing.T) {
	link := ContractLink{
		CallID: "c-1", CallRepo: "p1",
		EndpointID: "ep-1", EndpointRepo: "p2",
		Confidence: "static_path", WorkspaceID: "ws-1",
	}
	if link.ResolvedAt != 0 {
		t.Errorf("ContractLink{}.ResolvedAt = %d; want 0 (additive zero-default)", link.ResolvedAt)
	}
	if link.LinkMethod != "" {
		t.Errorf("ContractLink{}.LinkMethod = %q; want \"\" (additive zero-default)", link.LinkMethod)
	}
}
