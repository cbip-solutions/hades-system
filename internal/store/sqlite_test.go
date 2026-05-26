package store

import (
	"database/sql"
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"

	zerrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func TestOpenAndClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "open.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s == nil {
		t.Fatal("Open returned nil")
	}
	if s.Path() != dbPath {
		t.Errorf("Path = %q, want %q", s.Path(), dbPath)
	}
	if s.DB() == nil {
		t.Fatal("DB() returned nil")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	_, err := Open("/no/such/dir/test.db")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestPragmasApplied(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "pragmas.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var journalMode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode = %q, want WAL", journalMode)
	}

	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(p, "/state.db") {
		t.Errorf("DefaultPath = %q, want suffix /state.db", p)
	}
}

func TestDefaultPathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/zen-test-xdg")
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := "/tmp/zen-test-xdg/zen-swarm/state.db"
	if p != want {
		t.Errorf("DefaultPath = %q, want %q", p, want)
	}
}

func TestMigrateCreatesAllTables(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "schema.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	expected := []string{
		"schema_version",
		"events",
		"llm_calls",
		"decisions",
		"memory_writes",
		"doc_versions",
		"postmortems",
		"task_state",
		"worktrees",
		"bypass_audit",
		"bypass_config_versions",
		"payg_spend",
		"notifications_queue",
		"projects",
		"sessions",
		"swarms",
		"tasks",
	}
	for _, table := range expected {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "idempotent.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		if err := s.Migrate(); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	var count int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != schemaVersion {
		t.Errorf("schema_version rows = %d, want %d (each migration applied exactly once)", count, schemaVersion)
	}
}

func TestMigrateRecordsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "version.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var v int
	if err := s.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("query: %v", err)
	}
	if v != schemaVersion {
		t.Errorf("schema_version = %d, want %d", v, schemaVersion)
	}
}

func TestInsertAndListEvents(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	id, err := s.InsertEvent(EventRow{
		Project:     "test-proj",
		SessionID:   "sess-1",
		Type:        "test.event",
		PayloadJSON: `{"k":"v"}`,
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	events, err := s.ListEvents(EventQuery{Project: "test-proj", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "test.event" {
		t.Errorf("Type = %q, want test.event", events[0].Type)
	}
	if events[0].TS == 0 {
		t.Error("TS should default to time.Now")
	}
}

func TestInsertEventsBatch(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "batch.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows := []EventRow{
		{Type: "a", PayloadJSON: "{}"},
		{Type: "b", PayloadJSON: "{}"},
		{Type: "c", PayloadJSON: "{}"},
	}
	n, err := s.InsertEventsBatch(rows)
	if err != nil {
		t.Fatalf("InsertEventsBatch: %v", err)
	}
	if n != 3 {
		t.Errorf("inserted = %d, want 3", n)
	}

	all, err := s.ListEvents(EventQuery{Limit: 100})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d events, want 3", len(all))
	}
}

func TestListEventsByType(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "by-type.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, typ := range []string{"a", "a", "b"} {
		_, _ = s.InsertEvent(EventRow{Type: typ, PayloadJSON: "{}"})
	}

	a, err := s.ListEvents(EventQuery{Type: "a", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(a) != 2 {
		t.Errorf("type=a count = %d, want 2", len(a))
	}
}

func TestLLMCallsShape(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "llm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	_, err = s.InsertLLMCall(LLMCallRow{Provider: "deepseek", Model: "v4", TokensIn: 100, TokensOut: 50})
	if !errIsNotImplementedPlan(err, 4) {
		t.Errorf("InsertLLMCall: want NotImplementedPlan4, got %v", err)
	}

	_, err = s.ListLLMCalls(LLMCallQuery{Project: "x", Limit: 10})
	if !errIsNotImplementedPlan(err, 4) {
		t.Errorf("ListLLMCalls: want NotImplementedPlan4, got %v", err)
	}
}

func TestDecisionsShape(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "dec.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	_, err = s.InsertDecision(DecisionRow{Scope: "archive", Decision: "approve", Actor: "operator"})
	if !errIsNotImplementedPlan(err, 9) {
		t.Errorf("InsertDecision: want NotImplementedPlan9, got %v", err)
	}
}

func errIsNotImplementedPlan(err error, plan int) bool {
	if err == nil {
		return false
	}
	var nie *zerrors.NotImplementedError
	if !stderrors.As(err, &nie) {
		return false
	}
	return nie.Plan == plan
}

func TestMemoryWritesShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "mw.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertMemoryWrite(MemoryWriteRow{Project: "p", FilePath: "/tmp/x", Action: "create", Runtime: "zen-swarm"})
	if !errIsNotImplementedPlan(err, 9) {
		t.Errorf("InsertMemoryWrite: want Plan9, got %v", err)
	}
}

func TestDocVersionsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "dv.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertDocVersion(DocVersionRow{
		Project: "p", Feature: "f", DocPath: "proposal.md", Content: "text", Author: "operator",
	})
	if !errIsNotImplementedPlan(err, 9) {
		t.Errorf("InsertDocVersion: want Plan9, got %v", err)
	}
}

func TestPostmortemsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "pm.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertPostmortem(PostmortemRow{Project: "p", SwarmID: "s", Outcome: "completed-with-intervention"})
	if !errIsNotImplementedPlan(err, 11) {
		t.Errorf("InsertPostmortem: want Plan11, got %v", err)
	}
}

func TestTaskStateShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "ts.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertTaskState(TaskStateRow{TaskID: "t", SwarmID: "s", AttemptN: 1, CurrentPhase: "codegen"})
	if !errIsNotImplementedPlan(err, 5) {
		t.Errorf("InsertTaskState: want Plan5, got %v", err)
	}
}

func TestWorktreesShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "wt.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertWorktree(WorktreeRow{Project: "p", Feature: "f", TaskID: "t", Path: "/tmp/p-f-t", Branch: "zen/f/t", Status: "active"})
	if !errIsNotImplementedPlan(err, 5) {
		t.Errorf("InsertWorktree: want Plan5, got %v", err)
	}
}

func TestBypassAuditShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "ba.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertBypassAudit(BypassAuditRow{
		RequestHash: "abc", ResponseHash: "def", Success: true, TierUsed: "in-house",
	})
	if !errIsNotImplementedPlan(err, 2) {
		t.Errorf("InsertBypassAudit: want Plan2, got %v", err)
	}
}

func TestBypassConfigVersionsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "bcv.db"))
	defer s.Close()
	_ = s.Migrate()

	err := s.RecordBypassConfigVersion("2026.04.29.1", "initial", "operator")
	if !errIsNotImplementedPlan(err, 2) {
		t.Errorf("RecordBypassConfigVersion: want Plan2, got %v", err)
	}
}

func TestPAYGSpendShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "pg.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.InsertPAYGSpend(PAYGSpendRow{
		Project: "p", TokensIn: 100, TokensOut: 50, CostUSD: 0.01,
	})
	if !errIsNotImplementedPlan(err, 5) {
		t.Errorf("InsertPAYGSpend: want Plan5, got %v", err)
	}
}

func TestNotificationsQueueShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "nq.db"))
	defer s.Close()
	_ = s.Migrate()

	_, err := s.EnqueueNotification(NotificationRow{
		Severity: "actionable", Title: "test", DedupeHash: "abc",
		ChannelsJSON: `["dashboard","bell"]`,
	})
	if !errIsNotImplementedPlan(err, 11) {
		t.Errorf("EnqueueNotification: want Plan11, got %v", err)
	}
}

func TestProjectsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "pr.db"))
	defer s.Close()
	_ = s.Migrate()

	err := s.UpsertProject(ProjectRow{
		ID: "internal-platform-x", Path: "/path/to/projects/internal-platform-x",
		Execution: "mac", Doctrine: "max-scope",
	})
	if !errIsNotImplementedPlan(err, 7) {
		t.Errorf("UpsertProject: want Plan7, got %v", err)
	}
}

func TestSessionsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "se.db"))
	defer s.Close()
	_ = s.Migrate()

	err := s.RegisterSession(SessionRow{
		ID: "sess-1", Project: "internal-platform-x", Runtime: "opencode",
	})
	if !errIsNotImplementedPlan(err, 7) {
		t.Errorf("RegisterSession: want Plan7, got %v", err)
	}
}

func TestSwarmsShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "sw.db"))
	defer s.Close()
	_ = s.Migrate()

	err := s.CreateSwarm(SwarmRow{
		ID: "sw-1", Project: "internal-platform-x", Feature: "feat-x",
		Phase: "applying", Parallelism: 8,
	})
	if !errIsNotImplementedPlan(err, 5) {
		t.Errorf("CreateSwarm: want Plan5, got %v", err)
	}
}

func TestTasksShape(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "tk.db"))
	defer s.Close()
	_ = s.Migrate()

	err := s.CreateTask(TaskRow{
		ID: "t-1", SwarmID: "sw-1", SpecJSON: "{}", Phase: "codegen",
	})
	if !errIsNotImplementedPlan(err, 5) {
		t.Errorf("CreateTask: want Plan5, got %v", err)
	}
}

func TestComprehensiveShapeWalk(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "walk.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tables := []string{
		"events", "llm_calls", "decisions", "memory_writes", "doc_versions",
		"postmortems", "task_state", "worktrees", "bypass_audit",
		"bypass_config_versions", "payg_spend", "notifications_queue",
		"projects", "sessions", "swarms", "tasks", "schema_version",
	}
	for _, table := range tables {
		var count int
		err := s.DB().QueryRow(
			"SELECT COUNT(*) FROM " + table,
		).Scan(&count)
		if err != nil {
			t.Errorf("table %q query failed: %v", table, err)
		}
	}

	stubChecks := []struct {
		name string
		call func() error
	}{
		{"InsertLLMCall", func() error { _, e := s.InsertLLMCall(LLMCallRow{}); return e }},
		{"InsertDecision", func() error { _, e := s.InsertDecision(DecisionRow{}); return e }},
		{"InsertMemoryWrite", func() error { _, e := s.InsertMemoryWrite(MemoryWriteRow{}); return e }},
		{"InsertDocVersion", func() error { _, e := s.InsertDocVersion(DocVersionRow{}); return e }},
		{"InsertPostmortem", func() error { _, e := s.InsertPostmortem(PostmortemRow{}); return e }},
		{"InsertTaskState", func() error { _, e := s.InsertTaskState(TaskStateRow{}); return e }},
		{"InsertWorktree", func() error { _, e := s.InsertWorktree(WorktreeRow{}); return e }},
		{"InsertBypassAudit", func() error { _, e := s.InsertBypassAudit(BypassAuditRow{}); return e }},
		{"RecordBypassConfigVersion", func() error { return s.RecordBypassConfigVersion("v1", "", "") }},
		{"InsertPAYGSpend", func() error { _, e := s.InsertPAYGSpend(PAYGSpendRow{}); return e }},
		{"EnqueueNotification", func() error { _, e := s.EnqueueNotification(NotificationRow{}); return e }},
		{"UpsertProject", func() error { return s.UpsertProject(ProjectRow{}) }},
		{"RegisterSession", func() error { return s.RegisterSession(SessionRow{}) }},
		{"CreateSwarm", func() error { return s.CreateSwarm(SwarmRow{}) }},
		{"CreateTask", func() error { return s.CreateTask(TaskRow{}) }},
	}
	for _, c := range stubChecks {
		err := c.call()
		if err == nil {
			t.Errorf("%s: stub returned nil; expected NotImplementedError", c.name)
			continue
		}
		var nie *zerrors.NotImplementedError
		if !stderrors.As(err, &nie) {
			t.Errorf("%s: error %v is not *NotImplementedError", c.name, err)
			continue
		}
		if nie.Plan < 2 || nie.Plan > 15 {
			t.Errorf("%s: unexpected Plan number %d", c.name, nie.Plan)
		}
	}
}

func TestExpectedIndexesExist(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "idx.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	expected := []string{
		"idx_events_ts",
		"idx_events_project",
		"idx_events_swarm",
		"idx_events_session",
		"idx_events_type",
		"idx_llm_calls_ts",
		"idx_llm_calls_project",
		"idx_llm_calls_provider",
		"idx_decisions_ts",
		"idx_decisions_project",
		"idx_memory_writes_project",
		"idx_memory_writes_ts",
		"idx_doc_versions_feature",
		"idx_postmortems_swarm",
		"idx_task_state_task",
		"idx_worktrees_status",
		"idx_bypass_audit_ts",
		"idx_bypass_audit_success",
		"idx_payg_spend_project_ts",
		"idx_notifications_dispatched",
		"idx_notifications_dedupe",
		"idx_sessions_project",
		"idx_swarms_project_phase",
		"idx_tasks_swarm",
	}
	for _, idx := range expected {
		var name string
		err := s.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q missing: %v", idx, err)
		}
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "fk.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	_, err = s.DB().Exec(
		`INSERT INTO sessions (id, project, runtime, started_at) VALUES ('s1', 'nonexistent', 'opencode', ?)`,
		nowUnix(),
	)
	if err == nil {
		t.Error("expected FK violation; insert succeeded")
	}
}

func TestMigrationV2BypassAnomalies(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "anomalies.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var v int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("schema_version = %d, want %d", v, schemaVersion)
	}

	rows, err := s.DB().Query(`PRAGMA table_info(bypass_anomalies)`)
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	want := map[string]bool{
		"id": false, "field_path": false, "parent_path": false,
		"count": false, "first_seen": false, "last_seen": false,
		"total_responses_in_window": false, "percentage": false,
		"acknowledged": false,
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for col, seen := range want {
		if !seen {
			t.Errorf("column %q missing from bypass_anomalies", col)
		}
	}

	if _, err := s.DB().Exec(
		`INSERT INTO bypass_anomalies (field_path, first_seen, last_seen) VALUES (?, ?, ?)`,
		"cache_metadata", 1700000000, 1700000000,
	); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err = s.DB().Exec(
		`INSERT INTO bypass_anomalies (field_path, first_seen, last_seen) VALUES (?, ?, ?)`,
		"cache_metadata", 1700000010, 1700000010,
	)
	if err == nil {
		t.Error("expected UNIQUE violation on duplicate field_path")
	}

	if err := s.Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var v2 int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v2); err != nil {
		t.Fatal(err)
	}
	if v2 != schemaVersion {
		t.Fatalf("after second Migrate, version = %d, want %d", v2, schemaVersion)
	}
}

func TestMigration037ConversationWAL(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "wal-mig.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var name string
	if err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='conversation_wal'`,
	).Scan(&name); err != nil {
		t.Fatalf("table missing: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO conversation_wal (conversation_id, request_hash, request_ts, status)
		 VALUES (?, ?, ?, 'pending')`,
		"c", []byte("h"), 1,
	); err != nil {
		t.Fatalf("pending insert: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO conversation_wal (conversation_id, request_hash, request_ts, status)
		 VALUES (?, ?, ?, 'bogus')`,
		"c", []byte("h"), 2,
	); err == nil {
		t.Error("expected CHECK violation for status='bogus'")
	}
}

func TestMigration038Idempotency(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "idem-mig.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var name string
	if err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='idempotency_keys'`,
	).Scan(&name); err != nil {
		t.Fatalf("table missing: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO idempotency_keys (key, request_hash, status, ts, expires_at)
		 VALUES ('k1', ?, 'pending', 1, 100)`, []byte("h"),
	); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO idempotency_keys (key, request_hash, status, ts, expires_at)
		 VALUES ('k1', ?, 'pending', 1, 100)`, []byte("h"),
	); err == nil {
		t.Error("expected duplicate-PK violation")
	}

	if _, err := s.DB().Exec(
		`INSERT INTO idempotency_keys (key, request_hash, status, ts, expires_at)
		 VALUES ('k2', ?, 'bogus', 1, 100)`, []byte("h"),
	); err == nil {
		t.Error("expected CHECK violation for status='bogus'")
	}
}

func TestSchemaVersionAt8(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "v8.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var v int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("query: %v", err)
	}
	if v < 8 {
		t.Errorf("schema_version max = %d, want >= 8", v)
	}
}
