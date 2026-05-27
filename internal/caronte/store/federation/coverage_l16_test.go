// go:build cgo
package federation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func TestCoverageFederationGetWorkspace_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	row, err := wsdb.FederationGetWorkspace(ctx, wsID)
	if err != nil {
		t.Fatalf("FederationGetWorkspace: %v", err)
	}
	if row.WorkspaceID != wsID {
		t.Errorf("WorkspaceID = %q, want %q", row.WorkspaceID, wsID)
	}
	if row.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", row.SchemaVersion)
	}
}

func TestCoverageFederationGetWorkspace_NotFoundPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()

	_, err := wsdb.FederationGetWorkspace(ctx, "nonexistent-ws-id")
	if err == nil {
		t.Errorf("FederationGetWorkspace(nonexistent) returned nil err; want ErrNotFound")
	}
}

func TestCoverageFederationListWorkspaceMembers_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	members, err := wsdb.FederationListWorkspaceMembers(ctx, wsID)
	if err != nil {
		t.Fatalf("FederationListWorkspaceMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("members len = %d, want 2", len(members))
	}
	for _, m := range members {
		if m.WorkspaceID != wsID {
			t.Errorf("member.WorkspaceID = %q, want %q", m.WorkspaceID, wsID)
		}
		if m.RegisteredAt == 0 {
			t.Errorf("member.RegisteredAt = 0; want non-zero")
		}
	}
}

func TestCoverageFederationGetWorkspacePolicy_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	policy, err := wsdb.FederationGetWorkspacePolicy(ctx, wsID)
	if err != nil {
		t.Fatalf("FederationGetWorkspacePolicy (empty): %v", err)
	}
	if policy != "" {
		t.Errorf("policy = %q on fresh workspace; want empty", policy)
	}

	if err := wsdb.SetWorkspacePolicy(ctx, wsID, `{"locked":true}`); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}
	policy, err = wsdb.FederationGetWorkspacePolicy(ctx, wsID)
	if err != nil {
		t.Fatalf("FederationGetWorkspacePolicy (set): %v", err)
	}
	if policy != `{"locked":true}` {
		t.Errorf("policy = %q; want {\"locked\":true}", policy)
	}
}

func TestCoverageFederationListContractLinks_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	ls := wsdb.LinkStore()
	for i := 0; i < 3; i++ {
		if err := ls.Append(ctx, LinkRow{
			CallID:       "call-cov-" + itoaCov(i),
			CallRepo:     "owner",
			EndpointID:   "ep-cov-" + itoaCov(i),
			EndpointRepo: "client",
			Confidence:   "exact_proto_import",
			WorkspaceID:  wsID,
			ResolvedAt:   time.Now().Unix(),
			LinkMethod:   "artifact",
		}); err != nil {
			t.Fatalf("LinkStore.Append [%d]: %v", i, err)
		}
	}

	links, err := wsdb.FederationListContractLinks(ctx, wsID, 10)
	if err != nil {
		t.Fatalf("FederationListContractLinks: %v", err)
	}
	if len(links) != 3 {
		t.Errorf("links len = %d, want 3", len(links))
	}
	for _, l := range links {
		if l.WorkspaceID != wsID {
			t.Errorf("link.WorkspaceID = %q, want %q", l.WorkspaceID, wsID)
		}
		if l.Confidence != "exact_proto_import" {
			t.Errorf("link.Confidence = %q, want exact_proto_import", l.Confidence)
		}
		if l.LinkMethod != "artifact" {
			t.Errorf("link.LinkMethod = %q, want artifact", l.LinkMethod)
		}
	}
}

func TestCoverageFederationListRecentBreakingChanges_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	for i := 0; i < 2; i++ {
		if err := wsdb.InsertBreakingChange(ctx, BreakingChange{
			ChangeID:     "ch-cov-" + itoaCov(i),
			WorkspaceID:  wsID,
			EndpointID:   "ep-cov-" + itoaCov(i),
			EndpointRepo: "owner",
			Kind:         "type_changed",
			Detail:       `{"op":"changed"}`,
			DetectedAt:   time.Now().Unix(),
			DetectorID:   "oasdiff",
		}); err != nil {
			t.Fatalf("InsertBreakingChange [%d]: %v", i, err)
		}
	}

	rows, err := wsdb.FederationListRecentBreakingChanges(ctx, wsID, 10)
	if err != nil {
		t.Fatalf("FederationListRecentBreakingChanges: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows len = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.WorkspaceID != wsID {
			t.Errorf("row.WorkspaceID = %q, want %q", r.WorkspaceID, wsID)
		}
		if r.DetectorID != "oasdiff" {
			t.Errorf("row.DetectorID = %q, want oasdiff", r.DetectorID)
		}
		if r.Kind != "type_changed" {
			t.Errorf("row.Kind = %q, want type_changed", r.Kind)
		}
	}
}

func TestCoverageFederationGetBreakingChangeWithConsumers(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	const changeID = "ch-cov-composite"
	if err := wsdb.InsertBreakingChange(ctx, BreakingChange{
		ChangeID:     changeID,
		WorkspaceID:  wsID,
		EndpointID:   "ep-composite",
		EndpointRepo: "owner",
		Kind:         "field_removed",
		Detail:       `{"op":"removed"}`,
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "buf",
	}); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := wsdb.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{
			ChangeID: changeID,
			CallID:   "call-cov-composite-" + itoaCov(i),
			CallRepo: "client",
		}); err != nil {
			t.Fatalf("InsertBreakingChangeConsumer [%d]: %v", i, err)
		}
	}

	bc, consumers, err := wsdb.FederationGetBreakingChangeWithConsumers(ctx, changeID)
	if err != nil {
		t.Fatalf("FederationGetBreakingChangeWithConsumers: %v", err)
	}
	if bc.ChangeID != changeID {
		t.Errorf("bc.ChangeID = %q, want %q", bc.ChangeID, changeID)
	}
	if len(consumers) != 2 {
		t.Errorf("consumers len = %d, want 2", len(consumers))
	}
	for _, c := range consumers {
		if c.ChangeID != changeID {
			t.Errorf("consumer.ChangeID = %q, want %q", c.ChangeID, changeID)
		}
		if c.CallRepo != "client" {
			t.Errorf("consumer.CallRepo = %q, want client", c.CallRepo)
		}
	}
}

func TestCoverageFederationListWorkspaces(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	for i := 0; i < 3; i++ {
		if err := wsdb.RegisterWorkspace(ctx, WorkspaceRow{
			WorkspaceID:   "ws-list-" + itoaCov(i),
			OwningProject: "owner-" + itoaCov(i),
			PolicyLocked:  i%2 == 0,
			CreatedAt:     time.Now().Unix() - int64(3-i),
			SchemaVersion: 1,
		}); err != nil {
			t.Fatalf("RegisterWorkspace [%d]: %v", i, err)
		}
	}

	wss, err := wsdb.FederationListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("FederationListWorkspaces: %v", err)
	}
	if len(wss) != 3 {
		t.Errorf("workspaces len = %d, want 3", len(wss))
	}
}

func TestCoverageInsertBreakingChangeTx_HappyPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	tx, err := wsdb.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	if err := wsdb.InsertBreakingChangeTx(ctx, tx, BreakingChange{
		ChangeID:     "ch-tx-cov",
		WorkspaceID:  wsID,
		EndpointID:   "ep-tx",
		EndpointRepo: "owner",
		Kind:         "type_changed",
		Detail:       `{"op":"changed"}`,
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "oasdiff",
	}); err != nil {
		t.Fatalf("InsertBreakingChangeTx: %v", err)
	}
	if err := wsdb.InsertBreakingChangeConsumerTx(ctx, tx, BreakingChangeConsumer{
		ChangeID: "ch-tx-cov",
		CallID:   "call-tx-cov",
		CallRepo: "client",
	}); err != nil {
		t.Fatalf("InsertBreakingChangeConsumerTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}

	bc, err := wsdb.GetBreakingChange(ctx, "ch-tx-cov")
	if err != nil {
		t.Fatalf("GetBreakingChange post-Tx: %v", err)
	}
	if bc.ChangeID != "ch-tx-cov" {
		t.Errorf("post-Tx bc.ChangeID = %q, want ch-tx-cov", bc.ChangeID)
	}
}

func TestCoverageInsertBreakingChangeTx_NilTx(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()

	err := wsdb.InsertBreakingChangeTx(ctx, nil, BreakingChange{ChangeID: "x"})
	if err == nil {
		t.Errorf("InsertBreakingChangeTx(nil tx) returned nil err; want guard error")
	}
}

func TestCoverageUnresolvedStore_InsertGetListCycle(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()
	wsID := seedL16Workspace(t, ctx, wsdb)

	us := wsdb.UnresolvedStore()
	for i := 0; i < 3; i++ {
		row := UnresolvedRow{
			WorkspaceID: wsID,
			CallID:      "call-unres-" + itoaCov(i),
			CallRepo:    "client",
			BaseURLRef:  "UNKNOWN_" + itoaCov(i),
			Reason:      "no manifest entry",
			RecordedAt:  time.Now().UnixNano(),
		}
		if err := us.Insert(ctx, row); err != nil {
			t.Fatalf("UnresolvedStore.Insert [%d]: %v", i, err)
		}
	}

	got, err := wsdb.GetUnresolved(ctx, wsID, "call-unres-1", "client")
	if err != nil {
		t.Fatalf("GetUnresolved: %v", err)
	}
	if got.BaseURLRef != "UNKNOWN_1" {
		t.Errorf("got.BaseURLRef = %q, want UNKNOWN_1", got.BaseURLRef)
	}

	_, err = wsdb.GetUnresolved(ctx, wsID, "call-does-not-exist", "client")
	if err == nil {
		t.Errorf("GetUnresolved(nonexistent) returned nil err; want ErrNotFound")
	}

	rows, err := wsdb.ListUnresolvedByWorkspace(ctx, wsID, 100)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("rows len = %d, want 3", len(rows))
	}

	rows, err = wsdb.ListUnresolvedByWorkspace(ctx, wsID, 0)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace(limit=0): %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("rows len = %d on default-cap, want 3", len(rows))
	}
}

func TestCoverageInsertBreakingChangeConsumerTx_NilTx(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()

	err := wsdb.InsertBreakingChangeConsumerTx(ctx, nil, BreakingChangeConsumer{ChangeID: "x"})
	if err == nil {
		t.Errorf("InsertBreakingChangeConsumerTx(nil tx) returned nil err; want guard error")
	}
}

func TestCoverageInsertBreakingChangeTx_FKViolationRollback(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()

	tx, err := wsdb.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	err = wsdb.InsertBreakingChangeTx(ctx, tx, BreakingChange{
		ChangeID:     "ch-tx-bad-ws",
		WorkspaceID:  "workspace-does-not-exist",
		EndpointID:   "ep-x",
		EndpointRepo: "x",
		Kind:         "type_changed",
		Detail:       `{}`,
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "oasdiff",
	})
	if err == nil {
		t.Errorf("InsertBreakingChangeTx(bad ws_id) returned nil err; want FK violation")
	}
}

func TestCoverageUnresolvedInsert_NilDB(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	w := &WorkspaceFederationDB{}
	us := &unresolvedStoreImpl{parent: w}
	err := us.Insert(context.Background(), UnresolvedRow{WorkspaceID: "x"})
	if err != ErrEmptyDB {
		t.Errorf("Insert(nil db) err = %v, want ErrEmptyDB", err)
	}
}

func TestCoverageUnresolvedListAndGet_NilDB(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	w := &WorkspaceFederationDB{}
	if _, err := w.ListUnresolvedByWorkspace(context.Background(), "x", 10); err != ErrEmptyDB {
		t.Errorf("ListUnresolvedByWorkspace(nil db) err = %v, want ErrEmptyDB", err)
	}
}

func TestCoverageGetBreakingChange_NotFoundPath(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb := openL16FedDB(t, ctx)
	defer wsdb.Close()

	_, err := wsdb.GetBreakingChange(ctx, "nonexistent-change-id")
	if err == nil {
		t.Errorf("GetBreakingChange(nonexistent) returned nil err; want ErrNotFound")
	}
}

func TestCoverageNilDB_ErrEmptyDBGuards(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	w := &WorkspaceFederationDB{}
	ctx := context.Background()

	if err := w.RegisterWorkspace(ctx, WorkspaceRow{WorkspaceID: "x"}); err != ErrEmptyDB {
		t.Errorf("RegisterWorkspace(nil db) err = %v, want ErrEmptyDB", err)
	}
	if err := w.AddMember(ctx, MemberRow{WorkspaceID: "x"}); err != ErrEmptyDB {
		t.Errorf("AddMember(nil db) err = %v, want ErrEmptyDB", err)
	}
	if err := w.InsertBreakingChange(ctx, BreakingChange{ChangeID: "x"}); err != ErrEmptyDB {
		t.Errorf("InsertBreakingChange(nil db) err = %v, want ErrEmptyDB", err)
	}
	if err := w.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{ChangeID: "x"}); err != ErrEmptyDB {
		t.Errorf("InsertBreakingChangeConsumer(nil db) err = %v, want ErrEmptyDB", err)
	}
	if _, err := w.GetWorkspace(ctx, "x"); err != ErrEmptyDB {
		t.Errorf("GetWorkspace(nil db) err = %v, want ErrEmptyDB", err)
	}
	if _, err := w.ListWorkspaces(ctx); err != ErrEmptyDB {
		t.Errorf("ListWorkspaces(nil db) err = %v, want ErrEmptyDB", err)
	}
	if _, err := w.GetBreakingChange(ctx, "x"); err != ErrEmptyDB {
		t.Errorf("GetBreakingChange(nil db) err = %v, want ErrEmptyDB", err)
	}
	if _, err := w.GetUnresolved(ctx, "ws", "c", "r"); err != ErrEmptyDB {
		t.Errorf("GetUnresolved(nil db) err = %v, want ErrEmptyDB", err)
	}
}

func openL16FedDB(t *testing.T, ctx context.Context) *WorkspaceFederationDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "workspace.db")
	wsdb, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	return wsdb
}

func seedL16Workspace(t *testing.T, ctx context.Context, wsdb *WorkspaceFederationDB) string {
	t.Helper()
	const wsID = "ws-cov-test"
	if err := wsdb.RegisterWorkspace(ctx, WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: "owner",
		PolicyLocked:  false,
		CreatedAt:     time.Now().Unix(),
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for _, p := range []string{"owner", "client"} {
		if err := wsdb.AddMember(ctx, MemberRow{
			WorkspaceID:  wsID,
			ProjectID:    p,
			RegisteredAt: time.Now().Unix(),
		}); err != nil {
			t.Fatalf("AddMember(%s): %v", p, err)
		}
	}
	return wsID
}

func itoaCov(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
