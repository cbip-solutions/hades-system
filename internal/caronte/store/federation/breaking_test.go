//go:build cgo

package federation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func sampleBreakingChange() BreakingChange {
	return BreakingChange{
		ChangeID:       "bc-1",
		WorkspaceID:    "ws-1",
		EndpointID:     "ep-1",
		EndpointRepo:   "server-repo",
		Kind:           "param_added_required",
		Detail:         `{"param":"user_id","required":true}`,
		DetectedAt:     1_700_000_200,
		DetectorID:     "oasdiff",
		LoreAuthor:     "testuser",
		LoreCommitSHA:  "deadbeef",
		LoreADRRefs:    `["ADR-0114"]`,
		LoreSupersedes: `[]`,
	}
}

func TestInsertBreakingChangeRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	in := sampleBreakingChange()
	if err := db.InsertBreakingChange(ctx, in); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	got, err := db.GetBreakingChange(ctx, "bc-1")
	if err != nil {
		t.Fatalf("GetBreakingChange: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestInsertBreakingChangeWithoutLoreFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	in := sampleBreakingChange()
	in.LoreAuthor, in.LoreCommitSHA, in.LoreADRRefs, in.LoreSupersedes = "", "", "", ""
	if err := db.InsertBreakingChange(ctx, in); err != nil {
		t.Fatalf("InsertBreakingChange (no lore): %v", err)
	}
	got, err := db.GetBreakingChange(ctx, in.ChangeID)
	if err != nil {
		t.Fatalf("GetBreakingChange: %v", err)
	}
	if got.LoreAuthor != "" || got.LoreCommitSHA != "" || got.LoreADRRefs != "" || got.LoreSupersedes != "" {
		t.Errorf("expected empty lore fields, got %+v", got)
	}
}

func TestInsertBreakingChangeRejectsBadDetector(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	bad := sampleBreakingChange()
	bad.DetectorID = "my-bespoke-tool"
	err := db.InsertBreakingChange(ctx, bad)
	if err == nil {
		t.Fatal("InsertBreakingChange(bogus detector) returned nil err; want CHECK violation")
	}
	if !strings.Contains(err.Error(), "CHECK") && !strings.Contains(err.Error(), "constraint") {
		t.Errorf("err %q does not mention CHECK constraint", err)
	}
}

func TestGetBreakingChangeNotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetBreakingChange(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBreakingChange(missing) err = %v; want ErrNotFound", err)
	}
}

func TestListBreakingChangesByEndpoint(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for i, id := range []string{"bc-a", "bc-b", "bc-c"} {
		row := sampleBreakingChange()
		row.ChangeID = id
		row.DetectedAt = int64(1_700_000_300 + i)
		if err := db.InsertBreakingChange(ctx, row); err != nil {
			t.Fatalf("InsertBreakingChange[%d]: %v", i, err)
		}
	}
	rows, err := db.ListBreakingChangesByEndpoint(ctx, "ws-1", "ep-1", "server-repo")
	if err != nil {
		t.Fatalf("ListBreakingChangesByEndpoint: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("ListBreakingChangesByEndpoint returned %d rows; want 3", len(rows))
	}
}

func TestDeleteBreakingChangesByWorkspaceFanout(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.InsertBreakingChange(ctx, sampleBreakingChange()); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	n, err := db.DeleteBreakingChangesByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("DeleteBreakingChangesByWorkspace: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteBreakingChangesByWorkspace n = %d; want 1", n)
	}
}

func TestBreakingChangeCascadesOnWorkspaceDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.InsertBreakingChange(ctx, sampleBreakingChange()); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	if _, err := db.RemoveWorkspace(ctx, "ws-1"); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}

	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("re-RegisterWorkspace: %v", err)
	}
	if _, err := db.GetBreakingChange(ctx, "bc-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBreakingChange after RemoveWorkspace err = %v; want ErrNotFound (CASCADE)", err)
	}
}

func TestGetBreakingChangeWithConsumersAtomic(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.InsertBreakingChange(ctx, sampleBreakingChange()); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	for _, id := range []string{"c-a", "c-b"} {
		if err := db.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{ChangeID: "bc-1", CallID: id, CallRepo: "client-repo"}); err != nil {
			t.Fatalf("InsertBreakingChangeConsumer(%s): %v", id, err)
		}
	}
	change, consumers, err := db.GetBreakingChangeWithConsumers(ctx, "bc-1")
	if err != nil {
		t.Fatalf("GetBreakingChangeWithConsumers: %v", err)
	}
	if change.ChangeID != "bc-1" {
		t.Errorf("change.ChangeID = %q; want bc-1", change.ChangeID)
	}
	if len(consumers) != 2 {
		t.Fatalf("len(consumers) = %d; want 2", len(consumers))
	}
}

func TestGetBreakingChangeWithConsumersMissing(t *testing.T) {
	db := openTestDB(t)
	_, _, err := db.GetBreakingChangeWithConsumers(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBreakingChangeWithConsumers(missing) err = %v; want ErrNotFound", err)
	}
}

func TestListRecentBreakingChangesOrdering(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for i, id := range []string{"bc-old", "bc-mid", "bc-new"} {
		row := sampleBreakingChange()
		row.ChangeID = id
		row.DetectedAt = int64(1_700_000_400 + i)
		if err := db.InsertBreakingChange(ctx, row); err != nil {
			t.Fatalf("InsertBreakingChange[%d]: %v", i, err)
		}
	}
	got, err := db.ListRecentBreakingChanges(ctx, "ws-1", 0)
	if err != nil {
		t.Fatalf("ListRecentBreakingChanges: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListRecentBreakingChanges len = %d; want 3", len(got))
	}
	wantOrder := []string{"bc-new", "bc-mid", "bc-old"}
	for i, r := range got {
		if r.ChangeID != wantOrder[i] {
			t.Errorf("got[%d].ChangeID = %q; want %q", i, r.ChangeID, wantOrder[i])
		}
	}
	capped, err := db.ListRecentBreakingChanges(ctx, "ws-1", 1)
	if err != nil {
		t.Fatalf("ListRecentBreakingChanges(limit=1): %v", err)
	}
	if len(capped) != 1 || capped[0].ChangeID != "bc-new" {
		t.Errorf("ListRecentBreakingChanges(limit=1) = %+v; want [bc-new]", capped)
	}
}

func TestToStoreBreakingChangeRoundTrip(t *testing.T) {
	in := sampleBreakingChange()
	got := ToStoreBreakingChange(in)
	if got.ChangeID != in.ChangeID ||
		got.WorkspaceID != in.WorkspaceID ||
		got.EndpointID != in.EndpointID ||
		got.EndpointRepo != in.EndpointRepo ||
		got.Kind != in.Kind ||
		string(got.Detail) != in.Detail ||
		got.DetectedAt != in.DetectedAt ||
		got.DetectorID != in.DetectorID ||
		got.LoreAuthor != in.LoreAuthor ||
		got.LoreCommitSHA != in.LoreCommitSHA ||
		string(got.LoreADRRefs) != in.LoreADRRefs ||
		string(got.LoreSupersedes) != in.LoreSupersedes {
		t.Errorf("ToStoreBreakingChange round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}
