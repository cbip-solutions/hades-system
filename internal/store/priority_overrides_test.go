package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStoreLocal(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationV25CreatesPriorityOverridesTable(t *testing.T) {
	s := openTestStoreLocal(t)
	rows, err := s.DB().Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='priority_overrides'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("priority_overrides table not present after Migrate")
	}

	colsQuery := `SELECT name FROM pragma_table_info('priority_overrides')`
	colRows, err := s.DB().Query(colsQuery)
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer colRows.Close()
	got := map[string]bool{}
	for colRows.Next() {
		var name string
		if err := colRows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		got[name] = true
	}
	for _, want := range []string{
		"id", "project_alias", "multiplier", "expires_at", "reason", "created_at",
	} {
		if !got[want] {
			t.Errorf("priority_overrides missing column %q (got %v)", want, got)
		}
	}

	var idxName string
	err = s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='priority_overrides' AND name='idx_priority_overrides_expires_at'`,
	).Scan(&idxName)
	if err != nil {
		t.Errorf("idx_priority_overrides_expires_at not found: %v", err)
	}
}

func TestUpsertPriorityOverrideTxFreshInsert(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Truncate(time.Second)
	row := PriorityOverrideRow{
		ProjectAlias: "alpha",
		Multiplier:   3.0,
		ExpiresAt:    now.Add(1 * time.Hour),
		Reason:       "demo",
		CreatedAt:    now,
	}
	replaced, err := s.UpsertPriorityOverrideTx(ctx, tx, row)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if replaced {
		t.Errorf("replaced = true on fresh insert, want false")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := s.GetPriorityOverride(ctx, "alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil after committed insert")
	}
}

func TestUpsertPriorityOverrideTxReplacePath(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for i, m := range []float64{2.0, 5.0} {
		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx %d: %v", i, err)
		}
		row := PriorityOverrideRow{
			ProjectAlias: "alpha",
			Multiplier:   m,
			ExpiresAt:    now.Add(1 * time.Hour),
			Reason:       "demo",
			CreatedAt:    now,
		}
		replaced, err := s.UpsertPriorityOverrideTx(ctx, tx, row)
		if err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
		wantReplaced := i > 0
		if replaced != wantReplaced {
			t.Errorf("Upsert %d replaced = %v, want %v", i, replaced, wantReplaced)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	got, _ := s.GetPriorityOverride(ctx, "alpha")
	if got == nil || got.Multiplier != 5.0 {
		t.Errorf("after replace, multiplier = %v, want 5.0", got)
	}
}

func TestUpsertPriorityOverrideTxValidation(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()

	_, err := s.UpsertPriorityOverrideTx(ctx, nil, PriorityOverrideRow{})
	if err == nil {
		t.Error("nil tx: want error, got nil")
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	_, err = s.UpsertPriorityOverrideTx(ctx, tx, PriorityOverrideRow{
		Multiplier: 1.0,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
		Reason:     "x",
		CreatedAt:  time.Now(),
	})
	if err == nil {
		t.Error("empty alias: want error, got nil")
	}

	for _, m := range []float64{0, -1} {
		_, err = s.UpsertPriorityOverrideTx(ctx, tx, PriorityOverrideRow{
			ProjectAlias: "x",
			Multiplier:   m,
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			Reason:       "x",
			CreatedAt:    time.Now(),
		})
		if err == nil {
			t.Errorf("multiplier=%v: want error, got nil", m)
		}
	}
}

func TestGetPriorityOverrideNotFound(t *testing.T) {
	s := openTestStoreLocal(t)
	got, err := s.GetPriorityOverride(context.Background(), "no-such-alias")
	if err != nil {
		t.Errorf("Get(unknown): err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Get(unknown): got %+v, want nil", got)
	}
}

func TestGetPriorityOverrideEmptyAlias(t *testing.T) {
	s := openTestStoreLocal(t)
	_, err := s.GetPriorityOverride(context.Background(), "")
	if err == nil {
		t.Error("Get(empty): want error, got nil")
	}
}

func TestDeletePriorityOverrideTxRemoves(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := s.UpsertPriorityOverrideTx(ctx, tx, PriorityOverrideRow{
		ProjectAlias: "alpha",
		Multiplier:   2.0,
		ExpiresAt:    now.Add(1 * time.Hour),
		Reason:       "demo",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit insert: %v", err)
	}

	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 2: %v", err)
	}
	if err := s.DeletePriorityOverrideTx(ctx, tx2, "alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit delete: %v", err)
	}

	got, _ := s.GetPriorityOverride(ctx, "alpha")
	if got != nil {
		t.Errorf("after delete: got %+v, want nil", got)
	}
}

func TestDeletePriorityOverrideTxIdempotent(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	if err := s.DeletePriorityOverrideTx(ctx, tx, "no-such-alias"); err != nil {
		t.Errorf("Delete(unknown): err = %v, want nil (idempotent)", err)
	}
}

func TestDeletePriorityOverrideTxValidation(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()

	if err := s.DeletePriorityOverrideTx(ctx, nil, "x"); err == nil {
		t.Error("nil tx: want error, got nil")
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	if err := s.DeletePriorityOverrideTx(ctx, tx, ""); err == nil {
		t.Error("empty alias: want error, got nil")
	}
}

func TestListPriorityOverridesEmpty(t *testing.T) {
	s := openTestStoreLocal(t)
	rows, err := s.ListPriorityOverrides(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len = %d on empty, want 0", len(rows))
	}
}

func TestListPriorityOverridesOrder(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for i, alias := range []string{"a", "b", "c"} {
		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}

		_, err = s.UpsertPriorityOverrideTx(ctx, tx, PriorityOverrideRow{
			ProjectAlias: alias,
			Multiplier:   float64(i + 1),
			ExpiresAt:    now.Add(1 * time.Hour),
			Reason:       "x",
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("Upsert %s: %v", alias, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}
	rows, err := s.ListPriorityOverrides(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}

	want := []string{"c", "b", "a"}
	for i, w := range want {
		if rows[i].ProjectAlias != w {
			t.Errorf("rows[%d].ProjectAlias = %q, want %q", i, rows[i].ProjectAlias, w)
		}
	}
}

func TestInsertEventTxAndListByKind(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := s.InsertEventTx(ctx, tx, "test.kind", `{"k":"v"}`); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if err := s.InsertEventTx(ctx, tx, "test.kind", `{"k":"v2"}`); err != nil {
		t.Fatalf("InsertEvent 2: %v", err)
	}
	if err := s.InsertEventTx(ctx, tx, "other.kind", `{"k":"x"}`); err != nil {
		t.Fatalf("InsertEvent other: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := s.ListEventsByKind(ctx, "test.kind")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("len = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Type != "test.kind" {
			t.Errorf("Type = %q, want test.kind", r.Type)
		}
	}
	other, _ := s.ListEventsByKind(ctx, "other.kind")
	if len(other) != 1 {
		t.Errorf("other.kind len = %d, want 1", len(other))
	}
	none, _ := s.ListEventsByKind(ctx, "no.such.kind")
	if len(none) != 0 {
		t.Errorf("unknown kind len = %d, want 0", len(none))
	}
}

func TestInsertEventTxValidation(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()

	if err := s.InsertEventTx(ctx, nil, "k", "{}"); err == nil {
		t.Error("nil tx: want error, got nil")
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	if err := s.InsertEventTx(ctx, tx, "", "{}"); err == nil {
		t.Error("empty kind: want error, got nil")
	}
}

func TestListEventsByKindEmptyKind(t *testing.T) {
	s := openTestStoreLocal(t)
	if _, err := s.ListEventsByKind(context.Background(), ""); err == nil {
		t.Error("empty kind: want error, got nil")
	}
}

func TestBeginTxReturnsTx(t *testing.T) {
	s := openTestStoreLocal(t)
	tx, err := s.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if tx == nil {
		t.Fatal("BeginTx returned nil tx")
	}
	if err := tx.Rollback(); err != nil {
		t.Errorf("Rollback: %v", err)
	}
}

func TestExecRawValidation(t *testing.T) {
	s := openTestStoreLocal(t)
	if _, err := s.ExecRaw(context.Background(), ""); err == nil {
		t.Error("empty sql: want error, got nil")
	}
}

func TestExecRawHappyPath(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()
	if _, err := s.ExecRaw(ctx, "CREATE TEMP TABLE x (a INTEGER)"); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := s.ExecRaw(ctx, "INSERT INTO x VALUES (?)", 42); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
}

func TestPriorityOverridesUniqueAliasFromRawInsertReplaceablesViaUpsert(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()

	if _, err := s.ExecRaw(ctx,
		`INSERT INTO priority_overrides (project_alias, multiplier, expires_at, reason)
		 VALUES (?, ?, ?, ?)`,
		"x", 2.0, time.Now().Add(1*time.Hour), "raw"); err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	replaced, err := s.UpsertPriorityOverrideTx(ctx, tx, PriorityOverrideRow{
		ProjectAlias: "x",
		Multiplier:   5.0,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Reason:       "upsert",
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !replaced {
		t.Errorf("replaced = false, want true (probe should have detected prior row)")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestPriorityOverridesCheckMultiplier(t *testing.T) {
	s := openTestStoreLocal(t)
	ctx := context.Background()

	_, err := s.ExecRaw(ctx,
		`INSERT INTO priority_overrides (project_alias, multiplier, expires_at, reason)
		 VALUES (?, ?, ?, ?)`,
		"x", -1.0, time.Now().Add(1*time.Hour), "bad")
	if err == nil {
		t.Error("CHECK(multiplier > 0) did not reject negative; got no error")
	}

	_, err = s.ExecRaw(ctx,
		`INSERT INTO priority_overrides (project_alias, multiplier, expires_at, reason)
		 VALUES (?, ?, ?, ?)`,
		"y", 0.0, time.Now().Add(1*time.Hour), "bad")
	if err == nil {
		t.Error("CHECK(multiplier > 0) did not reject zero; got no error")
	}
}
