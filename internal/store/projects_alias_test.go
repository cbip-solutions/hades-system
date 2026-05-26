package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func openMigratedAliasStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "alias.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigration057ProjectsAliasTableExists(t *testing.T) {
	s := openMigratedAliasStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='projects_alias'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("projects_alias table missing after Migrate: %v", err)
	}
	if name != "projects_alias" {
		t.Errorf("expected projects_alias table, got %q", name)
	}
}

func TestMigration057PathHistoryTableExists(t *testing.T) {
	s := openMigratedAliasStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='path_history'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("path_history table missing after Migrate: %v", err)
	}
}

func TestMigration057SchemaVersionAtLeast24(t *testing.T) {
	s := openMigratedAliasStore(t)
	v, err := s.currentVersion()
	if err != nil {
		t.Fatalf("currentVersion: %v", err)
	}
	if v < 24 {
		t.Errorf("schemaVersion = %d, want >= 24 (Phase A migration 057 must be applied)", v)
	}
}

func TestInsertProjectAliasSuccess(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	row := ProjectAliasRow{
		IDSha256:      "a" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}
	if err := InsertProjectAlias(s.DB(), row); err != nil {
		t.Fatalf("InsertProjectAlias: %v", err)
	}
}

func TestInsertProjectAliasDuplicateIDReturnsErr(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	row := ProjectAliasRow{
		IDSha256:      "b" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "test1",
		CanonicalPath: "/tmp/test1",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}
	if err := InsertProjectAlias(s.DB(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	row.Alias = "test1-dup"
	row.CanonicalPath = "/tmp/test1-different"
	err := InsertProjectAlias(s.DB(), row)
	if !errors.Is(err, ErrDuplicateProjectID) {
		t.Fatalf("want ErrDuplicateProjectID, got %v", err)
	}
}

func TestInsertProjectAliasDuplicateAliasReturnsErr(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	row := ProjectAliasRow{
		IDSha256:      "c" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "shared-alias",
		CanonicalPath: "/tmp/shared1",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}
	if err := InsertProjectAlias(s.DB(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	row.IDSha256 = "d" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"
	row.CanonicalPath = "/tmp/shared2"
	err := InsertProjectAlias(s.DB(), row)
	if !errors.Is(err, ErrDuplicateAlias) {
		t.Fatalf("want ErrDuplicateAlias, got %v", err)
	}
}

func TestGetProjectAliasByAlias(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	original := ProjectAliasRow{
		IDSha256:      "e" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "lookup-test",
		CanonicalPath: "/tmp/lookup",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}
	if err := InsertProjectAlias(s.DB(), original); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := GetProjectAliasByAlias(s.DB(), "lookup-test")
	if err != nil {
		t.Fatalf("GetProjectAliasByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("got nil; want non-nil row")
	}
	if got.IDSha256 != original.IDSha256 ||
		got.Alias != original.Alias ||
		got.CanonicalPath != original.CanonicalPath {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, original)
	}
}

func TestGetProjectAliasByAliasNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	got, err := GetProjectAliasByAlias(s.DB(), "nonexistent")
	if err != nil {
		t.Fatalf("GetProjectAliasByAlias: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for not-found, got %+v", got)
	}
}

func TestGetProjectAliasByID(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "f" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"
	original := ProjectAliasRow{
		IDSha256:      id,
		Alias:         "by-id-test",
		CanonicalPath: "/tmp/by-id",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}
	if err := InsertProjectAlias(s.DB(), original); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := GetProjectAliasByID(s.DB(), id)
	if err != nil {
		t.Fatalf("GetProjectAliasByID: %v", err)
	}
	if got == nil || got.Alias != "by-id-test" {
		t.Errorf("GetProjectAliasByID round-trip failed: %+v", got)
	}
}

func TestInsertPathHistoryEntry(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "1" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"

	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "ph-entry",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertProjectAlias parent: %v", err)
	}
	entry := PathHistoryRow{
		IDSha256:    id,
		Path:        "/path/to/projects/internal-platform-x",
		FirstSeenAt: now.UnixMilli(),
		LastSeenAt:  now.UnixMilli(),
	}
	if err := InsertPathHistory(s.DB(), entry); err != nil {
		t.Fatalf("InsertPathHistory: %v", err)
	}
}

func TestInsertPathHistoryUpsertSemantics(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "2" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"

	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "ph-upsert",
		CanonicalPath: "/path/one",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertProjectAlias parent: %v", err)
	}
	first := PathHistoryRow{
		IDSha256:    id,
		Path:        "/path/one",
		FirstSeenAt: now.UnixMilli(),
		LastSeenAt:  now.UnixMilli(),
	}
	if err := InsertPathHistory(s.DB(), first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	later := time.Now().Add(1 * time.Hour)
	first.LastSeenAt = later.UnixMilli()
	if err := InsertPathHistory(s.DB(), first); err != nil {
		t.Fatalf("second insert (upsert): %v", err)
	}
	rows, err := QueryPathHistoryByID(s.DB(), id)
	if err != nil {
		t.Fatalf("QueryPathHistoryByID: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].LastSeenAt != later.UnixMilli() {
		t.Errorf("last_seen not updated by upsert: got %d, want %d",
			rows[0].LastSeenAt, later.UnixMilli())
	}
	if rows[0].FirstSeenAt != now.UnixMilli() {
		t.Errorf("first_seen mutated by upsert: got %d, want %d",
			rows[0].FirstSeenAt, now.UnixMilli())
	}
}

func TestQueryPathHistoryByIDMultiplePaths(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "3" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"

	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "ph-multipath",
		CanonicalPath: "/new/path3",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertProjectAlias parent: %v", err)
	}
	paths := []string{
		"/old/path1",
		"/old/path2",
		"/new/path3",
	}
	for i, p := range paths {
		entry := PathHistoryRow{
			IDSha256:    id,
			Path:        p,
			FirstSeenAt: now.Add(time.Duration(i) * time.Hour).UnixMilli(),
			LastSeenAt:  now.Add(time.Duration(i) * time.Hour).UnixMilli(),
		}
		if err := InsertPathHistory(s.DB(), entry); err != nil {
			t.Fatalf("InsertPathHistory[%d]: %v", i, err)
		}
	}
	rows, err := QueryPathHistoryByID(s.DB(), id)
	if err != nil {
		t.Fatalf("QueryPathHistoryByID: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("len = %d, want 3", len(rows))
	}
}

func TestDeleteProjectAliasCascadesPathHistory(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "4" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"
	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "cascade-test",
		CanonicalPath: "/tmp/cascade",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertProjectAlias: %v", err)
	}
	if err := InsertPathHistory(s.DB(), PathHistoryRow{
		IDSha256:    id,
		Path:        "/tmp/cascade",
		FirstSeenAt: now.UnixMilli(),
		LastSeenAt:  now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertPathHistory: %v", err)
	}
	if err := DeleteProjectAlias(s.DB(), "cascade-test"); err != nil {
		t.Fatalf("DeleteProjectAlias: %v", err)
	}
	rows, err := QueryPathHistoryByID(s.DB(), id)
	if err != nil {
		t.Fatalf("QueryPathHistoryByID: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("path_history rows remained after DeleteProjectAlias: %d", len(rows))
	}
}

func TestArchiveProjectAlias(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	id := "5" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde"
	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "archive-test",
		CanonicalPath: "/tmp/archive",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("InsertProjectAlias: %v", err)
	}
	if err := ArchiveProjectAlias(s.DB(), "archive-test", now.UnixMilli()); err != nil {
		t.Fatalf("ArchiveProjectAlias: %v", err)
	}
	got, err := GetProjectAliasByAlias(s.DB(), "archive-test")
	if err != nil {
		t.Fatalf("GetProjectAliasByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("expected row after archive (archive ≠ delete)")
	}
	if got.ArchivedAt == 0 {
		t.Errorf("archived_at not set: %d", got.ArchivedAt)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idem.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open[%d]: %v", i, err)
		}
		if err := s.Migrate(); err != nil {
			t.Fatalf("Migrate[%d]: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close[%d]: %v", i, err)
		}
	}
}

func TestInsertProjectAliasValidation(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now()
	full := ProjectAliasRow{
		IDSha256:      "9" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "valid",
		CanonicalPath: "/tmp/valid",
		FirstSeenAt:   now.UnixMilli(),
		LastSeenAt:    now.UnixMilli(),
	}

	bad := full
	bad.IDSha256 = ""
	if err := InsertProjectAlias(s.DB(), bad); err == nil {
		t.Errorf("empty IDSha256: want error, got nil")
	}

	bad = full
	bad.IDSha256 = "abcd"
	if err := InsertProjectAlias(s.DB(), bad); err == nil {
		t.Errorf("short IDSha256: want error, got nil")
	}

	bad = full
	bad.Alias = ""
	if err := InsertProjectAlias(s.DB(), bad); err == nil {
		t.Errorf("empty alias: want error, got nil")
	}

	bad = full
	bad.CanonicalPath = ""
	if err := InsertProjectAlias(s.DB(), bad); err == nil {
		t.Errorf("empty canonical_path: want error, got nil")
	}
}

func TestInsertProjectAliasArchivedAtNonZeroPersists(t *testing.T) {

	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()
	row := ProjectAliasRow{
		IDSha256:      "8" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcdef" + "0123456789abcde",
		Alias:         "born-archived",
		CanonicalPath: "/tmp/archived-from-birth",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		ArchivedAt:    now,
	}
	if err := InsertProjectAlias(s.DB(), row); err != nil {
		t.Fatalf("InsertProjectAlias with ArchivedAt: %v", err)
	}
	got, err := GetProjectAliasByID(s.DB(), row.IDSha256)
	if err != nil {
		t.Fatalf("GetProjectAliasByID: %v", err)
	}
	if got == nil || got.ArchivedAt != now {
		t.Errorf("ArchivedAt round-trip failed: got=%+v", got)
	}
}

func TestGetProjectAliasByIDNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	got, err := GetProjectAliasByID(s.DB(), strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("GetProjectAliasByID: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for not-found, got %+v", got)
	}
}

func TestListProjectAliases(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()

	rowA := ProjectAliasRow{
		IDSha256:      "a" + strings.Repeat("0", 63),
		Alias:         "alpha",
		CanonicalPath: "/tmp/alpha",
		FirstSeenAt:   now,
		LastSeenAt:    now + 100,
	}
	rowB := ProjectAliasRow{
		IDSha256:      "b" + strings.Repeat("0", 63),
		Alias:         "beta",
		CanonicalPath: "/tmp/beta",
		FirstSeenAt:   now,
		LastSeenAt:    now + 200,
	}
	rowC := ProjectAliasRow{
		IDSha256:      "c" + strings.Repeat("0", 63),
		Alias:         "gamma-archived",
		CanonicalPath: "/tmp/gamma",
		FirstSeenAt:   now,
		LastSeenAt:    now + 300,
		ArchivedAt:    now + 500,
	}
	for _, r := range []ProjectAliasRow{rowA, rowB, rowC} {
		if err := InsertProjectAlias(s.DB(), r); err != nil {
			t.Fatalf("Insert %s: %v", r.Alias, err)
		}
	}

	active, err := ListProjectAliases(s.DB(), false)
	if err != nil {
		t.Fatalf("ListProjectAliases(false): %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active len = %d, want 2", len(active))
	}
	if active[0].Alias != "beta" || active[1].Alias != "alpha" {
		t.Errorf("ORDER BY last_seen_at DESC violated: got %s,%s",
			active[0].Alias, active[1].Alias)
	}

	all, err := ListProjectAliases(s.DB(), true)
	if err != nil {
		t.Fatalf("ListProjectAliases(true): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all len = %d, want 3", len(all))
	}
	if all[0].Alias != "gamma-archived" {
		t.Errorf("first row should be archived gamma (highest last_seen_at): got %s",
			all[0].Alias)
	}
}

func TestListProjectAliasesEmpty(t *testing.T) {
	s := openMigratedAliasStore(t)
	rows, err := ListProjectAliases(s.DB(), false)
	if err != nil {
		t.Fatalf("ListProjectAliases empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty store: want 0 rows, got %d", len(rows))
	}
}

func TestUpdateProjectAliasLastSeen(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()
	row := ProjectAliasRow{
		IDSha256:      "6" + strings.Repeat("0", 63),
		Alias:         "lastseen-test",
		CanonicalPath: "/tmp/lastseen",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	if err := InsertProjectAlias(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	updated := now + 5000
	if err := UpdateProjectAliasLastSeen(s.DB(), "lastseen-test", updated); err != nil {
		t.Fatalf("UpdateProjectAliasLastSeen: %v", err)
	}
	got, err := GetProjectAliasByAlias(s.DB(), "lastseen-test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil")
	}
	if got.LastSeenAt != updated {
		t.Errorf("LastSeenAt = %d, want %d", got.LastSeenAt, updated)
	}
}

func TestUpdateProjectAliasLastSeenNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	err := UpdateProjectAliasLastSeen(s.DB(), "no-such-alias", time.Now().UnixMilli())
	if err == nil {
		t.Fatal("want error for not-found, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want sql.ErrNoRows wrapped, got %v", err)
	}
}

func TestArchiveProjectAliasInvalidTimestamp(t *testing.T) {
	s := openMigratedAliasStore(t)
	if err := ArchiveProjectAlias(s.DB(), "any", 0); err == nil {
		t.Errorf("zero archivedAt: want error, got nil")
	}
	if err := ArchiveProjectAlias(s.DB(), "any", -1); err == nil {
		t.Errorf("negative archivedAt: want error, got nil")
	}
}

func TestArchiveProjectAliasNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	err := ArchiveProjectAlias(s.DB(), "no-such-alias", time.Now().UnixMilli())
	if err == nil {
		t.Errorf("want error for not-found, got nil")
	}
}

func TestArchiveProjectAliasReArchivePreservesTimestamp(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()
	id := "a" + strings.Repeat("0", 62) + "1"

	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "rearchive-test",
		CanonicalPath: "/tmp/rearchive",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}); err != nil {
		t.Fatalf("InsertProjectAlias: %v", err)
	}

	t1 := now + 1000
	if err := ArchiveProjectAlias(s.DB(), "rearchive-test", t1); err != nil {
		t.Fatalf("first ArchiveProjectAlias: %v", err)
	}
	got, err := GetProjectAliasByAlias(s.DB(), "rearchive-test")
	if err != nil {
		t.Fatalf("GetProjectAliasByAlias after first archive: %v", err)
	}
	if got == nil || got.ArchivedAt != t1 {
		t.Fatalf("after first archive: ArchivedAt = %d, want %d", got.ArchivedAt, t1)
	}

	// 3. Second archive at t2 (t2 > t1) MUST NOT overwrite — it must
	//    return an error (not-found-or-already-archived) and leave the
	//    original archived_at intact.
	t2 := now + 5000
	err = ArchiveProjectAlias(s.DB(), "rearchive-test", t2)
	if err == nil {
		t.Errorf("re-archive: want error, got nil (silent overwrite breaks audit semantic)")
	}

	got, err = GetProjectAliasByAlias(s.DB(), "rearchive-test")
	if err != nil {
		t.Fatalf("GetProjectAliasByAlias after re-archive attempt: %v", err)
	}
	if got == nil {
		t.Fatal("row missing after re-archive attempt")
	}
	if got.ArchivedAt != t1 {
		t.Errorf("ArchivedAt mutated by re-archive: got %d, want %d (preserved t1)",
			got.ArchivedAt, t1)
	}
}

func TestDeleteProjectAliasNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	err := DeleteProjectAlias(s.DB(), "no-such-alias")
	if err == nil {
		t.Errorf("want error for not-found, got nil")
	}
}

func TestInsertPathHistoryValidation(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()

	if err := InsertPathHistory(s.DB(), PathHistoryRow{
		Path:        "/some/path",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err == nil {
		t.Errorf("empty IDSha256: want error, got nil")
	}

	if err := InsertPathHistory(s.DB(), PathHistoryRow{
		IDSha256:    strings.Repeat("0", 64),
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err == nil {
		t.Errorf("empty Path: want error, got nil")
	}
}

func TestQueryPathHistoryByAlias(t *testing.T) {
	s := openMigratedAliasStore(t)
	now := time.Now().UnixMilli()
	id := "7" + strings.Repeat("0", 63)
	if err := InsertProjectAlias(s.DB(), ProjectAliasRow{
		IDSha256:      id,
		Alias:         "qph-alias",
		CanonicalPath: "/tmp/qph",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}); err != nil {
		t.Fatalf("InsertProjectAlias: %v", err)
	}
	if err := InsertPathHistory(s.DB(), PathHistoryRow{
		IDSha256:    id,
		Path:        "/tmp/qph",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("InsertPathHistory: %v", err)
	}

	got, err := QueryPathHistoryByAlias(s.DB(), "qph-alias")
	if err != nil {
		t.Fatalf("QueryPathHistoryByAlias: %v", err)
	}
	if len(got) != 1 || got[0].Path != "/tmp/qph" {
		t.Errorf("QueryPathHistoryByAlias result: %+v", got)
	}
}

func TestQueryPathHistoryByAliasNotFound(t *testing.T) {
	s := openMigratedAliasStore(t)
	got, err := QueryPathHistoryByAlias(s.DB(), "no-such-alias")
	if err != nil {
		t.Fatalf("QueryPathHistoryByAlias: %v", err)
	}
	if got != nil {
		t.Errorf("want nil for unknown alias, got %+v", got)
	}
}

func openClosedDB(t *testing.T) *sql.DB {
	t.Helper()
	s := openMigratedAliasStore(t)
	db := s.DB()
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return db
}

func TestProjectsAliasErrorWrapping(t *testing.T) {
	db := openClosedDB(t)

	if _, err := GetProjectAliasByAlias(db, "x"); err == nil {
		t.Errorf("GetProjectAliasByAlias on closed db: want error, got nil")
	}

	if _, err := GetProjectAliasByID(db, strings.Repeat("0", 64)); err == nil {
		t.Errorf("GetProjectAliasByID on closed db: want error, got nil")
	}

	if _, err := ListProjectAliases(db, false); err == nil {
		t.Errorf("ListProjectAliases on closed db: want error, got nil")
	}

	if err := UpdateProjectAliasLastSeen(db, "x", 1); err == nil {
		t.Errorf("UpdateProjectAliasLastSeen on closed db: want error, got nil")
	}

	if err := ArchiveProjectAlias(db, "x", 1); err == nil {
		t.Errorf("ArchiveProjectAlias on closed db: want error, got nil")
	}

	if err := DeleteProjectAlias(db, "x"); err == nil {
		t.Errorf("DeleteProjectAlias on closed db: want error, got nil")
	}

	row := ProjectAliasRow{
		IDSha256:      strings.Repeat("0", 64),
		Alias:         "x",
		CanonicalPath: "/x",
		FirstSeenAt:   1,
		LastSeenAt:    1,
	}
	if err := InsertProjectAlias(db, row); err == nil {
		t.Errorf("InsertProjectAlias on closed db: want error, got nil")
	}

	if err := InsertPathHistory(db, PathHistoryRow{
		IDSha256: strings.Repeat("0", 64),
		Path:     "/x",
	}); err == nil {
		t.Errorf("InsertPathHistory on closed db: want error, got nil")
	}

	if _, err := QueryPathHistoryByID(db, strings.Repeat("0", 64)); err == nil {
		t.Errorf("QueryPathHistoryByID on closed db: want error, got nil")
	}

	if _, err := QueryPathHistoryByAlias(db, "x"); err == nil {
		t.Errorf("QueryPathHistoryByAlias on closed db: want error, got nil")
	}
}

// TestIsProjectAliasPKViolationStringFallbacks documents the contract
// that the PK-discriminator predicate matches a synthesized error
// carrying "projects_alias.id_sha256" or "PRIMARY KEY" even when the
// typed sqlite3.CONSTRAINT_PRIMARYKEY is absent. Mirrors the
// cost_ledger.go isUniqueViolation fallback test pattern: future
// driver upgrades that drop the typed code MUST still surface
// ErrDuplicateProjectID via the message text.
func TestIsProjectAliasPKViolationStringFallbacks(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"column-qualified", errors.New("UNIQUE constraint failed: projects_alias.id_sha256"), true},
		{"primary-key-tail", errors.New("constraint failed: PRIMARY KEY violation"), true},
		{"unrelated-error", errors.New("disk full"), false},
		{"empty-error", errors.New(""), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := isProjectAliasPKViolation(tc.err)
			if got != tc.want {
				t.Errorf("isProjectAliasPKViolation(%q) = %v, want %v",
					tc.err, got, tc.want)
			}
		})
	}
}

func TestIsProjectAliasUniqueViolationStringFallbacks(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"column-qualified", errors.New("UNIQUE constraint failed: projects_alias.alias"), true},
		{"generic-unique", errors.New("sqlite3: UNIQUE constraint failed"), true},
		{"unrelated-error", errors.New("disk full"), false},
		{"empty-error", errors.New(""), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := isProjectAliasUniqueViolation(tc.err)
			if got != tc.want {
				t.Errorf("isProjectAliasUniqueViolation(%q) = %v, want %v",
					tc.err, got, tc.want)
			}
		})
	}
}

func TestInsertProjectAliasErrorChainPreservesTypedCode(t *testing.T) {
	t.Run("DuplicatePK_PreservesPrimaryKeyTypedCode", func(t *testing.T) {
		s := openMigratedAliasStore(t)
		now := time.Now().UnixMilli()
		row := ProjectAliasRow{
			IDSha256:      "1" + strings.Repeat("a", 63),
			Alias:         "typedpk-1",
			CanonicalPath: "/tmp/typedpk-1",
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}
		if err := InsertProjectAlias(s.DB(), row); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		row.Alias = "typedpk-1-dup"
		row.CanonicalPath = "/tmp/typedpk-1-different"
		err := InsertProjectAlias(s.DB(), row)
		if !errors.Is(err, ErrDuplicateProjectID) {
			t.Fatalf("want ErrDuplicateProjectID, got %v", err)
		}

		_, rawErr := s.DB().Exec(
			`INSERT INTO projects_alias (
				id_sha256, alias, canonical_path,
				first_seen_at, last_seen_at, archived_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
			row.IDSha256, "typedpk-1-raw", "/tmp/typedpk-1-raw",
			now, now, nil,
		)
		if rawErr == nil {
			t.Fatal("raw duplicate insert: want error, got nil")
		}
		if !errors.Is(rawErr, sqlite3.CONSTRAINT_PRIMARYKEY) {
			t.Errorf("driver should surface CONSTRAINT_PRIMARYKEY typed code; got %v", rawErr)
		}
	})

	t.Run("DuplicateAlias_PreservesUniqueTypedCode", func(t *testing.T) {
		s := openMigratedAliasStore(t)
		now := time.Now().UnixMilli()
		row := ProjectAliasRow{
			IDSha256:      "2" + strings.Repeat("b", 63),
			Alias:         "typedunique-1",
			CanonicalPath: "/tmp/typedunique-1",
			FirstSeenAt:   now,
			LastSeenAt:    now,
		}
		if err := InsertProjectAlias(s.DB(), row); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		row.IDSha256 = "3" + strings.Repeat("c", 63)
		row.CanonicalPath = "/tmp/typedunique-1-different"
		err := InsertProjectAlias(s.DB(), row)
		if !errors.Is(err, ErrDuplicateAlias) {
			t.Fatalf("want ErrDuplicateAlias, got %v", err)
		}

		_, rawErr := s.DB().Exec(
			`INSERT INTO projects_alias (
				id_sha256, alias, canonical_path,
				first_seen_at, last_seen_at, archived_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
			"4"+strings.Repeat("d", 63), "typedunique-1", "/tmp/raw-different",
			now, now, nil,
		)
		if rawErr == nil {
			t.Fatal("raw duplicate-alias insert: want error, got nil")
		}
		if !errors.Is(rawErr, sqlite3.CONSTRAINT_UNIQUE) {
			t.Errorf("driver should surface CONSTRAINT_UNIQUE typed code; got %v", rawErr)
		}
	})
}
