//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func setupUnpromoteFixture(t *testing.T) *promoteFixture {
	t.Helper()
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Promote(context.Background(),
		fix.noteID, fix.projectID, fix.operatorID,
		"pre-promote for unpromote test")
	if err != nil {
		t.Fatalf("setupUnpromoteFixture: Promote: %v", err)
	}
	return fix
}

const (
	unpromoteOperatorID = "testuser"
	unpromoteReason     = "removing stale knowledge pin"
)

func resetUnpromoteHooks() {
	unpromoteHooks.beginTx = nil
	unpromoteHooks.execFTS = nil
	unpromoteHooks.execVec = nil
	unpromoteHooks.execWikilinks = nil
	unpromoteHooks.execPinIndex = nil
	unpromoteHooks.commit = nil
}

func injectedExecErr(msg string) func(context.Context, string, ...any) (sql.Result, error) {
	return func(_ context.Context, _ string, _ ...any) (sql.Result, error) {
		return nil, errors.New(msg)
	}
}

func TestUnpromoteHappyPath(t *testing.T) {
	fix := setupUnpromoteFixture(t)

	var beforeCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID,
	).Scan(&beforeCnt); err != nil {
		t.Fatalf("pre-check COUNT pin_index: %v", err)
	}
	if beforeCnt != 1 {
		t.Fatalf("expected 1 pin_index row before Unpromote; got %d", beforeCnt)
	}

	result, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err != nil {
		t.Fatalf("Unpromote: %v", err)
	}

	if result.AuditChainAnchor == "" {
		t.Error("result.AuditChainAnchor empty; expected non-empty")
	}
	if !containsStr(result.AuditChainAnchor, ":") {
		t.Errorf("result.AuditChainAnchor = %q; expected to contain \":\"", result.AuditChainAnchor)
	}

	if result.NoteID != fix.noteID {
		t.Errorf("result.NoteID = %q; want %q", result.NoteID, fix.noteID)
	}

	if result.Idempotent {
		t.Error("result.Idempotent = true on first Unpromote of existing pin; expected false")
	}

	if result.UnpromotedAt.IsZero() {
		t.Error("result.UnpromotedAt is zero; expected non-zero timestamp")
	}

	var afterCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID,
	).Scan(&afterCnt); err != nil {
		t.Fatalf("post-check COUNT pin_index: %v", err)
	}
	if afterCnt != 0 {
		t.Errorf("pin_index count after Unpromote = %d; want 0", afterCnt)
	}

	var ftsCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_fts WHERE knowledge_pin_fts MATCH ?`, "wikilink",
	).Scan(&ftsCnt); err != nil {
		t.Fatalf("post-check COUNT pin_fts: %v", err)
	}
	if ftsCnt != 0 {
		t.Errorf("pin_fts count after Unpromote = %d; want 0", ftsCnt)
	}

	var vecCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_vec WHERE rowid IN (SELECT rowid FROM knowledge_pin_index WHERE note_id = ?)`,
		fix.noteID,
	).Scan(&vecCnt); err != nil {
		t.Fatalf("post-check COUNT pin_vec: %v", err)
	}
	if vecCnt != 0 {
		t.Errorf("pin_vec count after Unpromote = %d; want 0", vecCnt)
	}
}

func TestUnpromoteRejectsEmptyReason(t *testing.T) {
	fix := setupUnpromoteFixture(t)

	_, err := fix.agg.Unpromote(context.Background(), fix.noteID, fix.operatorID, "")
	if err == nil {
		t.Fatal("Unpromote with empty reason succeeded; expected ErrPromoteReasonRequired")
	}
	if !errors.Is(err, ErrPromoteReasonRequired) {
		t.Errorf("error = %v; want errors.Is(err, ErrPromoteReasonRequired)", err)
	}

	if !containsStr(err.Error(), "inv-zen-146") {
		t.Errorf("error.Error() = %q; must contain \"inv-zen-146\"", err.Error())
	}
}

func TestUnpromoteIdempotentNonexistentPin(t *testing.T) {

	fix := setupPromoteFixture(t)

	const ghostNoteID = "never-promoted-note"
	result, err := fix.agg.Unpromote(context.Background(),
		ghostNoteID, fix.operatorID, unpromoteReason)
	if err != nil {
		t.Fatalf("Unpromote nonexistent pin: unexpected error: %v", err)
	}
	if !result.Idempotent {
		t.Error("result.Idempotent = false for nonexistent pin; expected true")
	}
	if result.NoteID != ghostNoteID {
		t.Errorf("result.NoteID = %q; want %q", result.NoteID, ghostNoteID)
	}
	if result.UnpromotedAt.IsZero() {
		t.Error("result.UnpromotedAt is zero for idempotent path; expected non-zero")
	}
}

func TestUnpromoteSourceVaultUntouched(t *testing.T) {

	fix := setupUnpromoteFixture(t)

	var beforeVaultCnt int
	if err := fix.projectDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID,
	).Scan(&beforeVaultCnt); err != nil {
		t.Fatalf("pre-check per-project vault COUNT: %v", err)
	}
	if beforeVaultCnt != 1 {
		t.Fatalf("expected 1 row in per-project vault before Unpromote; got %d", beforeVaultCnt)
	}

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err != nil {
		t.Fatalf("Unpromote: %v", err)
	}

	var aggCnt int
	if err := fix.pinDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID,
	).Scan(&aggCnt); err != nil {
		t.Fatalf("post-check aggregator pin_index COUNT: %v", err)
	}
	if aggCnt != 0 {
		t.Errorf("aggregator pin_index count after Unpromote = %d; want 0", aggCnt)
	}

	// Per-project vault knowledge_pin_index MUST still have 1 row.
	var afterVaultCnt int
	if err := fix.projectDB.QueryRow(
		`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID,
	).Scan(&afterVaultCnt); err != nil {
		t.Fatalf("post-check per-project vault COUNT: %v", err)
	}
	if afterVaultCnt != 1 {
		t.Errorf("per-project vault row count after Unpromote = %d; want 1 (source vault must be untouched)", afterVaultCnt)
	}
}

func TestUnpromoteEmptyNoteIDOrOperator(t *testing.T) {
	fix := setupPromoteFixture(t)

	_, err := fix.agg.Unpromote(context.Background(), "", fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with empty noteID returned nil; expected error")
	}
	if errors.Is(err, ErrPromoteReasonRequired) {
		t.Error("error should NOT be ErrPromoteReasonRequired for empty noteID")
	}

	_, err = fix.agg.Unpromote(context.Background(), fix.noteID, "", unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with empty operatorID returned nil; expected error")
	}
}

func TestUnpromoteSelectError(t *testing.T) {
	fix := setupUnpromoteFixture(t)

	fix.pinDB.Close()

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with closed pinDB returned nil; expected SELECT error")
	}
}

func TestUnpromoteChainAnchorError(t *testing.T) {
	fix := setupUnpromoteFixture(t)

	agg, err := New(Options{
		DB:       fix.pinDB,
		Embedder: newMockEmbedder(384),
		Store:    fix.store,
		Chain:    errChain{},
		Clock:    promoteClock{fixedAt: fix.fixedTime},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with failing chain returned nil; expected ComputeAnchor error")
	}

	var cnt int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("pin_index count after ComputeAnchor error = %d; want 1 (rollback expected)", cnt)
	}
}

func TestUnpromoteBeginTxError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.beginTx = func(_ context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
		return nil, errors.New("injected: BeginTx failure")
	}

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with injected BeginTx error returned nil; expected error")
	}
	if !containsStr(err.Error(), "begin tx") {
		t.Errorf("error = %q; expected to mention \"begin tx\"", err.Error())
	}

	var cnt int
	fix.pinDB.QueryRow(`SELECT COUNT(*) FROM knowledge_pin_index WHERE note_id = ?`, fix.noteID).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("pin_index count after BeginTx error = %d; want 1", cnt)
	}
}

func TestUnpromoteFTSDeleteError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.execFTS = injectedExecErr("injected: FTS DELETE error")

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with FTS DELETE error returned nil; expected error")
	}
	if !containsStr(err.Error(), "delete fts") {
		t.Errorf("error = %q; expected to mention \"delete fts\"", err.Error())
	}
}

func TestUnpromoteVecDeleteError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.execVec = injectedExecErr("injected: vec DELETE error")

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with vec DELETE error returned nil; expected error")
	}
	if !containsStr(err.Error(), "delete vec") {
		t.Errorf("error = %q; expected to mention \"delete vec\"", err.Error())
	}
}

func TestUnpromoteWikilinksDeleteError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.execWikilinks = injectedExecErr("injected: wikilinks DELETE error")

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with wikilinks DELETE error returned nil; expected error")
	}
	if !containsStr(err.Error(), "delete wikilinks") {
		t.Errorf("error = %q; expected to mention \"delete wikilinks\"", err.Error())
	}
}

func TestUnpromotePinIndexDeleteError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.execPinIndex = injectedExecErr("injected: pin_index DELETE error")

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with pin_index DELETE error returned nil; expected error")
	}
	if !containsStr(err.Error(), "delete pin_index") {
		t.Errorf("error = %q; expected to mention \"delete pin_index\"", err.Error())
	}
}

func TestUnpromoteCommitError(t *testing.T) {
	fix := setupUnpromoteFixture(t)
	t.Cleanup(resetUnpromoteHooks)

	unpromoteHooks.commit = func() error {
		return errors.New("injected: Commit failure")
	}

	_, err := fix.agg.Unpromote(context.Background(),
		fix.noteID, fix.operatorID, unpromoteReason)
	if err == nil {
		t.Fatal("Unpromote with Commit error returned nil; expected error")
	}
	if !containsStr(err.Error(), "commit") {
		t.Errorf("error = %q; expected to mention \"commit\"", err.Error())
	}
}

var _ = UnpromoteResult{UnpromotedAt: time.Time{}}
