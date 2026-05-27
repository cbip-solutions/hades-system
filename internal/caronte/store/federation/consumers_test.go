// go:build cgo
package federation

import (
	"context"
	"errors"
	"testing"
)

func setupConsumersTest(t *testing.T) *WorkspaceFederationDB {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.RegisterWorkspace(ctx, sampleWorkspaceRow()); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.InsertBreakingChange(ctx, sampleBreakingChange()); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	return db
}

func TestInsertBreakingChangeConsumerRoundTrip(t *testing.T) {
	db := setupConsumersTest(t)
	ctx := context.Background()
	in := BreakingChangeConsumer{ChangeID: "bc-1", CallID: "c-a", CallRepo: "client-repo"}
	if err := db.InsertBreakingChangeConsumer(ctx, in); err != nil {
		t.Fatalf("InsertBreakingChangeConsumer: %v", err)
	}
	got, err := db.ListBreakingChangeConsumers(ctx, "bc-1")
	if err != nil {
		t.Fatalf("ListBreakingChangeConsumers: %v", err)
	}
	if len(got) != 1 || got[0] != in {
		t.Errorf("ListBreakingChangeConsumers = %v; want [%+v]", got, in)
	}
}

func TestInsertBreakingChangeConsumerDuplicateRefused(t *testing.T) {
	db := setupConsumersTest(t)
	ctx := context.Background()
	in := BreakingChangeConsumer{ChangeID: "bc-1", CallID: "c-a", CallRepo: "client-repo"}
	if err := db.InsertBreakingChangeConsumer(ctx, in); err != nil {
		t.Fatalf("InsertBreakingChangeConsumer 1: %v", err)
	}
	if err := db.InsertBreakingChangeConsumer(ctx, in); err == nil {
		t.Error("InsertBreakingChangeConsumer duplicate returned nil err; want PK violation")
	}
}

func TestDeleteConsumersByChange(t *testing.T) {
	db := setupConsumersTest(t)
	ctx := context.Background()
	for _, id := range []string{"c-a", "c-b", "c-c"} {
		if err := db.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{ChangeID: "bc-1", CallID: id, CallRepo: "client-repo"}); err != nil {
			t.Fatalf("InsertBreakingChangeConsumer(%s): %v", id, err)
		}
	}
	n, err := db.DeleteConsumersByChange(ctx, "bc-1")
	if err != nil {
		t.Fatalf("DeleteConsumersByChange: %v", err)
	}
	if n != 3 {
		t.Errorf("DeleteConsumersByChange n = %d; want 3", n)
	}
}

func TestConsumerCascadesOnBreakingChangeDelete(t *testing.T) {
	db := setupConsumersTest(t)
	ctx := context.Background()
	if err := db.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{ChangeID: "bc-1", CallID: "c-a", CallRepo: "client-repo"}); err != nil {
		t.Fatalf("InsertBreakingChangeConsumer: %v", err)
	}

	if _, err := db.DeleteBreakingChangesByWorkspace(ctx, "ws-1"); err != nil {
		t.Fatalf("DeleteBreakingChangesByWorkspace: %v", err)
	}
	rows, err := db.ListBreakingChangeConsumers(ctx, "bc-1")
	if err != nil {
		t.Fatalf("ListBreakingChangeConsumers: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListBreakingChangeConsumers post-cascade returned %d rows; want 0", len(rows))
	}

	err = db.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{ChangeID: "bc-1", CallID: "c-a", CallRepo: "client-repo"})
	if err == nil {
		t.Error("InsertBreakingChangeConsumer post-cascade returned nil err; want FK violation")
	}
	_ = errors.Is
}
