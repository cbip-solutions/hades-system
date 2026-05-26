package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func openMigratedTmuxStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "tmux.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigration062TmuxSessionStateTableExists(t *testing.T) {
	s := openMigratedTmuxStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='tmux_session_state'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("tmux_session_state table missing after Migrate: %v", err)
	}
	if name != "tmux_session_state" {
		t.Errorf("expected tmux_session_state table, got %q", name)
	}
}

func TestMigration062TmuxSessionStateColumns(t *testing.T) {
	s := openMigratedTmuxStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(tmux_session_state)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = ctype
	}
	want := map[string]string{
		"name":           "TEXT",
		"alias":          "TEXT",
		"sha8":           "TEXT",
		"status":         "INTEGER",
		"created_at":     "INTEGER",
		"last_attach_at": "INTEGER",
		"expected_panes": "TEXT",
	}
	for col, ty := range want {
		if got[col] != ty {
			t.Errorf("column %q: got type %q, want %q", col, got[col], ty)
		}
	}
}

func TestMigration062TmuxSessionStateIndexes(t *testing.T) {
	s := openMigratedTmuxStore(t)
	for _, idx := range []string{
		"idx_tmux_session_state_alias",
		"idx_tmux_session_state_status_attach",
	} {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='tmux_session_state' AND name=?`,
			idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q missing: %v", idx, err)
		}
	}
}

func TestMigration062TmuxSessionStateStatusCheck(t *testing.T) {
	s := openMigratedTmuxStore(t)
	for _, bad := range []int{-1, 4, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO tmux_session_state
			 (name, alias, sha8, status, created_at, last_attach_at, expected_panes)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"zen-test-deadbeef", "test", "deadbeef", bad, 1, 0, "{}",
		)
		if err == nil {
			t.Errorf("status=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestSchemaVersionIs26AtLeast(t *testing.T) {
	t.Parallel()
	if schemaVersion < 26 {
		t.Errorf("schemaVersion = %d, want >= 26 (Plan 7 Phase C-11 migration 062 must remain applied)", schemaVersion)
	}
}

func TestInsertTmuxSessionStateSuccess(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-internal-platform-x-deadbeef",
		Alias:         "internal-platform-x",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		LastAttachAt:  time.Time{},
		ExpectedPanes: `{"orch":["%0"]}`,
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("InsertTmuxSessionState: %v", err)
	}
}

func TestInsertTmuxSessionStateDuplicateNameReturnsErr(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-dup-deadbeef",
		Alias:         "dup",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		LastAttachAt:  time.Time{},
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := InsertTmuxSessionState(s.DB(), row)
	if !errors.Is(err, ErrDuplicateTmuxSessionName) {
		t.Errorf("want ErrDuplicateTmuxSessionName, got %v", err)
	}

	if err != nil && !errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {

		_ = err
	}
}

func TestInsertTmuxSessionStateValidationErrors(t *testing.T) {
	s := openMigratedTmuxStore(t)
	cases := []struct {
		name string
		mut  func(r *TmuxSessionStateRow)
	}{
		{"empty name", func(r *TmuxSessionStateRow) { r.Name = "" }},
		{"empty alias", func(r *TmuxSessionStateRow) { r.Alias = "" }},
		{"sha8 wrong length", func(r *TmuxSessionStateRow) { r.Sha8 = "abc" }},
		{"sha8 invalid hex", func(r *TmuxSessionStateRow) { r.Sha8 = "ZZZZZZZZ" }},
		{"status out of range high", func(r *TmuxSessionStateRow) { r.Status = 4 }},
		{"status out of range low", func(r *TmuxSessionStateRow) { r.Status = -1 }},
		{"empty expected_panes", func(r *TmuxSessionStateRow) { r.ExpectedPanes = "" }},
	}
	now := time.Now().UTC()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := TmuxSessionStateRow{
				Name:          "zen-vtest-deadbeef",
				Alias:         "vtest",
				Sha8:          "deadbeef",
				Status:        0,
				CreatedAt:     now,
				ExpectedPanes: "{}",
			}
			tc.mut(&r)
			if err := InsertTmuxSessionState(s.DB(), r); err == nil {
				t.Errorf("expected validation error for %q, got nil", tc.name)
			}
		})
	}
}

func TestGetTmuxSessionStateRoundTrip(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	last := now.Add(-30 * time.Minute)
	row := TmuxSessionStateRow{
		Name:          "zen-rt-deadbeef",
		Alias:         "rt",
		Sha8:          "deadbeef",
		Status:        2,
		CreatedAt:     now.Add(-1 * time.Hour),
		LastAttachAt:  last,
		ExpectedPanes: `{"orch":["%0","%1"],"leads":["%2"]}`,
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := GetTmuxSessionState(s.DB(), row.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for inserted row")
	}
	if got.Name != row.Name || got.Alias != row.Alias || got.Sha8 != row.Sha8 {
		t.Errorf("identity mismatch: got %+v", got)
	}
	if got.Status != row.Status {
		t.Errorf("Status: got %d, want %d", got.Status, row.Status)
	}
	if !got.CreatedAt.Equal(row.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, row.CreatedAt)
	}
	if !got.LastAttachAt.Equal(row.LastAttachAt) {
		t.Errorf("LastAttachAt: got %v, want %v", got.LastAttachAt, row.LastAttachAt)
	}
	if got.ExpectedPanes != row.ExpectedPanes {
		t.Errorf("ExpectedPanes: got %q, want %q", got.ExpectedPanes, row.ExpectedPanes)
	}
}

func TestGetTmuxSessionStateAbsentReturnsNil(t *testing.T) {
	s := openMigratedTmuxStore(t)
	got, err := GetTmuxSessionState(s.DB(), "zen-missing-deadbeef")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for absent row, got %+v", got)
	}
}

func TestGetTmuxSessionStateZeroLastAttach(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-zero-deadbeef",
		Alias:         "zero",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		LastAttachAt:  time.Time{},
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := GetTmuxSessionState(s.DB(), row.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt: got %v, want zero (never attached)", got.LastAttachAt)
	}
}

func TestListTmuxSessionStatesEmpty(t *testing.T) {
	s := openMigratedTmuxStore(t)
	got, err := ListTmuxSessionStates(s.DB())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 on empty store", len(got))
	}
}

func TestListTmuxSessionStatesMulti(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i, alias := range []string{"alpha", "beta", "gamma"} {
		row := TmuxSessionStateRow{
			Name:          "zen-" + alias + "-1234567" + string(rune('a'+i)),
			Alias:         alias,
			Sha8:          "1234567" + string(rune('a'+i)),
			Status:        SessionStatusInt(i % 4),
			CreatedAt:     now.Add(time.Duration(i) * time.Second),
			LastAttachAt:  time.Time{},
			ExpectedPanes: "{}",
		}
		if err := InsertTmuxSessionState(s.DB(), row); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}
	got, err := ListTmuxSessionStates(s.DB())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestDeleteTmuxSessionStateSuccess(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-del-deadbeef",
		Alias:         "del",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := DeleteTmuxSessionState(s.DB(), row.Name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ := GetTmuxSessionState(s.DB(), row.Name)
	if got != nil {
		t.Errorf("row still present after Delete: %+v", got)
	}
}

func TestDeleteTmuxSessionStateAbsentReturnsErr(t *testing.T) {
	s := openMigratedTmuxStore(t)
	err := DeleteTmuxSessionState(s.DB(), "zen-missing-deadbeef")
	if !errors.Is(err, ErrTmuxSessionStateNotFound) {
		t.Errorf("want ErrTmuxSessionStateNotFound, got %v", err)
	}
}

func TestUpdateTmuxSessionStateLastAttach(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-ua-deadbeef",
		Alias:         "ua",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now.Add(-1 * time.Hour),
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := UpdateTmuxSessionLastAttach(s.DB(), row.Name, now); err != nil {
		t.Fatalf("Update last_attach: %v", err)
	}
	got, _ := GetTmuxSessionState(s.DB(), row.Name)
	if !got.LastAttachAt.Equal(now) {
		t.Errorf("LastAttachAt: got %v, want %v", got.LastAttachAt, now)
	}
}

func TestUpdateTmuxSessionStateLastAttachAbsent(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	err := UpdateTmuxSessionLastAttach(s.DB(), "zen-missing-deadbeef", now)
	if !errors.Is(err, ErrTmuxSessionStateNotFound) {
		t.Errorf("want ErrTmuxSessionStateNotFound, got %v", err)
	}
}

func TestUpdateTmuxSessionStateStatus(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-us-deadbeef",
		Alias:         "us",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := UpdateTmuxSessionStatus(s.DB(), row.Name, 2); err != nil {
		t.Fatalf("Update status: %v", err)
	}
	got, _ := GetTmuxSessionState(s.DB(), row.Name)
	if got.Status != 2 {
		t.Errorf("Status: got %d, want 2 (orphaned)", got.Status)
	}
}

func TestUpdateTmuxSessionStateStatusAbsent(t *testing.T) {
	s := openMigratedTmuxStore(t)
	err := UpdateTmuxSessionStatus(s.DB(), "zen-missing-deadbeef", 1)
	if !errors.Is(err, ErrTmuxSessionStateNotFound) {
		t.Errorf("want ErrTmuxSessionStateNotFound, got %v", err)
	}
}

func TestUpdateTmuxSessionStateStatusInvalid(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-usi-deadbeef",
		Alias:         "usi",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	for _, bad := range []int{-1, 4, 99} {
		err := UpdateTmuxSessionStatus(s.DB(), row.Name, bad)
		if err == nil {
			t.Errorf("status=%d: expected validation error, got nil", bad)
		}
	}
}

func TestUpdateTmuxSessionStateExpectedPanes(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-up-deadbeef",
		Alias:         "up",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	updated := `{"orch":["%0"],"leads":["%1","%2"]}`
	if err := UpdateTmuxSessionExpectedPanes(s.DB(), row.Name, updated); err != nil {
		t.Fatalf("Update expected_panes: %v", err)
	}
	got, _ := GetTmuxSessionState(s.DB(), row.Name)
	if got.ExpectedPanes != updated {
		t.Errorf("ExpectedPanes: got %q, want %q", got.ExpectedPanes, updated)
	}
}

func TestUpdateTmuxSessionStateExpectedPanesAbsent(t *testing.T) {
	s := openMigratedTmuxStore(t)
	err := UpdateTmuxSessionExpectedPanes(s.DB(), "zen-missing-deadbeef", "{}")
	if !errors.Is(err, ErrTmuxSessionStateNotFound) {
		t.Errorf("want ErrTmuxSessionStateNotFound, got %v", err)
	}
}

func TestUpdateTmuxSessionStateExpectedPanesEmpty(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-upe-deadbeef",
		Alias:         "upe",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := UpdateTmuxSessionExpectedPanes(s.DB(), row.Name, ""); err == nil {
		t.Error("expected validation error for empty json, got nil")
	}
}

func SessionStatusInt(i int) int {
	if i < 0 {
		return 0
	}
	if i > 3 {
		return 3
	}
	return i
}

func TestGetTmuxSessionStateEmptyName(t *testing.T) {
	s := openMigratedTmuxStore(t)
	if _, err := GetTmuxSessionState(s.DB(), ""); err == nil {
		t.Error("expected error on empty name, got nil")
	}
}

func TestDeleteTmuxSessionStateEmptyName(t *testing.T) {
	s := openMigratedTmuxStore(t)
	if err := DeleteTmuxSessionState(s.DB(), ""); err == nil {
		t.Error("expected error on empty name, got nil")
	}
}

func TestUpdateTmuxSessionLastAttachEmptyName(t *testing.T) {
	s := openMigratedTmuxStore(t)
	if err := UpdateTmuxSessionLastAttach(s.DB(), "", time.Now()); err == nil {
		t.Error("expected error on empty name, got nil")
	}
}

func TestUpdateTmuxSessionStatusEmptyName(t *testing.T) {
	s := openMigratedTmuxStore(t)
	if err := UpdateTmuxSessionStatus(s.DB(), "", 0); err == nil {
		t.Error("expected error on empty name, got nil")
	}
}

func TestUpdateTmuxSessionExpectedPanesEmptyName(t *testing.T) {
	s := openMigratedTmuxStore(t)
	if err := UpdateTmuxSessionExpectedPanes(s.DB(), "", "{}"); err == nil {
		t.Error("expected error on empty name, got nil")
	}
}

func TestIsTmuxSessionNamePKViolationStringFallback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"column-qualified", errors.New("UNIQUE constraint failed: tmux_session_state.name"), true},
		{"generic primary-key tail", errors.New("constraint failed: PRIMARY KEY"), true},
		{"unrelated", errors.New("disk full"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTmuxSessionNamePKViolation(tc.err); got != tc.want {
				t.Errorf("isTmuxSessionNamePKViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestUpdateTmuxSessionExpectedPanesMalformedJSON(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-mj-deadbeef",
		Alias:         "mj",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: "{}",
	}
	if err := InsertTmuxSessionState(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := UpdateTmuxSessionExpectedPanes(s.DB(), row.Name, `{"orch":["%0"`); err == nil {
		t.Error("expected JSON validity error, got nil")
	}
}

func TestInsertTmuxSessionStateMalformedJSON(t *testing.T) {
	s := openMigratedTmuxStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := TmuxSessionStateRow{
		Name:          "zen-imj-deadbeef",
		Alias:         "imj",
		Sha8:          "deadbeef",
		Status:        0,
		CreatedAt:     now,
		ExpectedPanes: `{not valid json}`,
	}
	if err := InsertTmuxSessionState(s.DB(), row); err == nil {
		t.Error("expected JSON validity error, got nil")
	}
}
