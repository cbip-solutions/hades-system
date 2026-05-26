//go:build cgo

package federation

import (
	"context"
	"testing"
)

func TestAddMemberRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	in := MemberRow{WorkspaceID: "ws-1", ProjectID: "proj-a", RegisteredAt: 1_700_000_001}
	if err := db.AddMember(ctx, in); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	got, err := db.ListWorkspaceMembers(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListWorkspaceMembers: %v", err)
	}
	if len(got) != 1 || got[0] != in {
		t.Errorf("ListWorkspaceMembers = %+v; want [%+v]", got, in)
	}
}

func TestAddMemberDuplicateRefused(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	in := MemberRow{WorkspaceID: "ws-1", ProjectID: "proj-a", RegisteredAt: 1_700_000_001}
	if err := db.AddMember(ctx, in); err != nil {
		t.Fatalf("AddMember 1: %v", err)
	}

	if err := db.AddMember(ctx, in); err == nil {
		t.Error("AddMember duplicate returned nil err; want PK violation")
	}
}

func TestRemoveMember(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	in := MemberRow{WorkspaceID: "ws-1", ProjectID: "proj-a", RegisteredAt: 1_700_000_001}
	if err := db.AddMember(ctx, in); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	n, err := db.RemoveMember(ctx, "ws-1", "proj-a")
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if n != 1 {
		t.Errorf("RemoveMember n = %d; want 1", n)
	}
	rows, err := db.ListWorkspaceMembers(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListWorkspaceMembers: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListWorkspaceMembers after remove = %v; want empty", rows)
	}
}

func TestRemoveMemberMissingIsNoOp(t *testing.T) {
	db := openTestDB(t)
	if err := db.RegisterWorkspace(context.Background(), sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	n, err := db.RemoveMember(context.Background(), "ws-1", "missing")
	if err != nil {
		t.Errorf("RemoveMember(missing) returned err: %v", err)
	}
	if n != 0 {
		t.Errorf("RemoveMember(missing) n = %d; want 0", n)
	}
}

func TestListWorkspaceMembersOrderedByRegistration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for i, id := range []string{"proj-c", "proj-a", "proj-b"} {

		if err := db.AddMember(ctx, MemberRow{
			WorkspaceID: "ws-1", ProjectID: id, RegisteredAt: int64(1_700_000_010 + i),
		}); err != nil {
			t.Fatalf("AddMember[%d]: %v", i, err)
		}
	}
	got, err := db.ListWorkspaceMembers(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListWorkspaceMembers: %v", err)
	}
	want := []string{"proj-c", "proj-a", "proj-b"}
	for i, w := range want {
		if got[i].ProjectID != w {
			t.Errorf("ListWorkspaceMembers[%d].ProjectID = %q; want %q", i, got[i].ProjectID, w)
		}
	}
}
