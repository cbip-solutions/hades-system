//go:build cgo

package federation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func sampleLinkRow() LinkRow {
	return LinkRow{
		CallID: "call-1", CallRepo: "client-repo",
		EndpointID: "ep-1", EndpointRepo: "server-repo",
		Confidence:  "static_path",
		WorkspaceID: "ws-1",
		ResolvedAt:  1_700_000_100,
		LinkMethod:  "caronte_yaml",
	}
}

func linkStoreImplFor(t *testing.T) (*WorkspaceFederationDB, LinkStore) {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	return db, db.LinkStore()
}

func TestLinkStoreAppendRoundTrip(t *testing.T) {
	db, ls := linkStoreImplFor(t)
	ctx := context.Background()
	in := sampleLinkRow()
	if err := ls.Append(ctx, in); err != nil {
		t.Fatalf("LinkStore.Append: %v", err)
	}
	got, err := db.GetLink(ctx, "ws-1", "call-1", "ep-1")
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestLinkStoreAppendRejectsBadConfidence(t *testing.T) {
	_, ls := linkStoreImplFor(t)
	bad := sampleLinkRow()
	bad.Confidence = "totally_bogus"
	err := ls.Append(context.Background(), bad)
	if err == nil {
		t.Fatal("LinkStore.Append(bogus confidence) returned nil err; want CHECK violation")
	}
	if !strings.Contains(err.Error(), "CHECK") && !strings.Contains(err.Error(), "constraint") {
		t.Errorf("err %q does not mention CHECK constraint", err)
	}
}

func TestLinkStoreAppendRejectsBadLinkMethod(t *testing.T) {
	_, ls := linkStoreImplFor(t)
	bad := sampleLinkRow()
	bad.LinkMethod = "not_a_method"
	err := ls.Append(context.Background(), bad)
	if err == nil {
		t.Fatal("LinkStore.Append(bogus link_method) returned nil err; want CHECK violation")
	}
}

func TestLinkStoreAppendDuplicateRefused(t *testing.T) {
	_, ls := linkStoreImplFor(t)
	in := sampleLinkRow()
	if err := ls.Append(context.Background(), in); err != nil {
		t.Fatalf("Append 1: %v", err)
	}

	if err := ls.Append(context.Background(), in); err == nil {
		t.Error("Append duplicate returned nil err; want PK violation")
	}
}

func TestGetLinkNotFound(t *testing.T) {
	db, _ := linkStoreImplFor(t)
	_, err := db.GetLink(context.Background(), "ws-1", "missing", "also-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLink(missing) err = %v; want ErrNotFound", err)
	}
}

func TestListByEndpointAndListByCall(t *testing.T) {
	db, ls := linkStoreImplFor(t)
	ctx := context.Background()

	rows := []LinkRow{
		{CallID: "c-a", CallRepo: "client-repo", EndpointID: "ep-1", EndpointRepo: "server-repo", Confidence: "spec_artifact", WorkspaceID: "ws-1", ResolvedAt: 1, LinkMethod: "artifact"},
		{CallID: "c-b", CallRepo: "client-repo", EndpointID: "ep-1", EndpointRepo: "server-repo", Confidence: "static_path", WorkspaceID: "ws-1", ResolvedAt: 2, LinkMethod: "caronte_yaml"},
		{CallID: "c-c", CallRepo: "client-repo", EndpointID: "ep-2", EndpointRepo: "server-repo", Confidence: "fuzzy_path", WorkspaceID: "ws-1", ResolvedAt: 3, LinkMethod: "fuzzy"},
	}
	for i, r := range rows {
		if err := ls.Append(ctx, r); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	byEp, err := db.ListByEndpoint(ctx, "ws-1", "ep-1", "server-repo")
	if err != nil {
		t.Fatalf("ListByEndpoint: %v", err)
	}
	if len(byEp) != 2 {
		t.Errorf("ListByEndpoint returned %d rows; want 2", len(byEp))
	}
	byCall, err := db.ListByCall(ctx, "ws-1", "c-c", "client-repo")
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(byCall) != 1 {
		t.Errorf("ListByCall returned %d rows; want 1", len(byCall))
	}
}

func TestListContractLinksPaginated(t *testing.T) {
	db, ls := linkStoreImplFor(t)
	ctx := context.Background()
	rows := []LinkRow{
		{CallID: "c-a", CallRepo: "client-repo", EndpointID: "ep-1", EndpointRepo: "server-repo", Confidence: "spec_artifact", WorkspaceID: "ws-1", ResolvedAt: 1, LinkMethod: "artifact"},
		{CallID: "c-b", CallRepo: "client-repo", EndpointID: "ep-1", EndpointRepo: "server-repo", Confidence: "static_path", WorkspaceID: "ws-1", ResolvedAt: 2, LinkMethod: "caronte_yaml"},
		{CallID: "c-c", CallRepo: "client-repo", EndpointID: "ep-2", EndpointRepo: "server-repo", Confidence: "fuzzy_path", WorkspaceID: "ws-1", ResolvedAt: 3, LinkMethod: "fuzzy"},
	}
	for i, r := range rows {
		if err := ls.Append(ctx, r); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	got, err := db.ListContractLinks(ctx, "ws-1", 0)
	if err != nil {
		t.Fatalf("ListContractLinks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListContractLinks len = %d; want 3", len(got))
	}

	wantOrder := []string{"c-a", "c-b", "c-c"}
	for i, r := range got {
		if r.CallID != wantOrder[i] {
			t.Errorf("got[%d].CallID = %q; want %q", i, r.CallID, wantOrder[i])
		}
	}

	capped, err := db.ListContractLinks(ctx, "ws-1", 2)
	if err != nil {
		t.Fatalf("ListContractLinks(limit=2): %v", err)
	}
	if len(capped) != 2 {
		t.Errorf("ListContractLinks(limit=2) len = %d; want 2", len(capped))
	}
}

func TestRemoveWorkspaceCascadesToLinks(t *testing.T) {
	db, ls := linkStoreImplFor(t)
	ctx := context.Background()
	if err := ls.Append(ctx, sampleLinkRow()); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := db.RemoveWorkspace(ctx, "ws-1"); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}

	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("re-RegisterWorkspace: %v", err)
	}
	_, err := db.GetLink(ctx, "ws-1", "call-1", "ep-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLink after RemoveWorkspace err = %v; want ErrNotFound (CASCADE)", err)
	}
}

func TestDeleteByWorkspaceDropsAllLinks(t *testing.T) {
	db, ls := linkStoreImplFor(t)
	ctx := context.Background()
	if err := ls.Append(ctx, sampleLinkRow()); err != nil {
		t.Fatalf("Append: %v", err)
	}
	n, err := db.DeleteLinksByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("DeleteLinksByWorkspace: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteLinksByWorkspace n = %d; want 1", n)
	}
}
