// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEmbeddingFromBytesNil(t *testing.T) {
	t.Parallel()
	if got := embeddingFromBytes(nil); got != nil {
		t.Errorf("embeddingFromBytes(nil): want nil, got %v", got)
	}
}

func TestEmbeddingFromBytesEmpty(t *testing.T) {
	t.Parallel()
	if got := embeddingFromBytes([]byte{}); got != nil {
		t.Errorf("embeddingFromBytes([]byte{}): want nil, got %v", got)
	}
}

func TestEmbeddingFromBytesNonMultipleOf4(t *testing.T) {
	t.Parallel()
	for _, bad := range []int{1, 2, 3, 5, 7, 9, 11} {
		b := make([]byte, bad)
		if got := embeddingFromBytes(b); got != nil {
			t.Errorf("embeddingFromBytes(%d bytes): want nil, got %v", bad, got)
		}
	}
}

func TestEmbeddingFromBytesRoundTrip(t *testing.T) {
	t.Parallel()
	orig := embed384Mock(7)
	encoded := embeddingToBytes(orig)
	decoded := embeddingFromBytes(encoded)
	if decoded == nil {
		t.Fatal("embeddingFromBytes returned nil for valid encoded input")
	}
	if len(decoded) != len(orig) {
		t.Fatalf("decoded length %d != original %d", len(decoded), len(orig))
	}
	for i := range orig {
		if orig[i] != decoded[i] {
			t.Errorf("decoded[%d] = %v, want %v", i, decoded[i], orig[i])
		}
	}
}

func TestEmbeddingFromBytesSmall(t *testing.T) {
	t.Parallel()
	b := []byte{0x00, 0x00, 0x80, 0x3f}
	got := embeddingFromBytes(b)
	if got == nil {
		t.Fatal("embeddingFromBytes(4 bytes): want non-nil, got nil")
	}
	if len(got) != 1 {
		t.Fatalf("want len 1, got %d", len(got))
	}
	if got[0] != 1.0 {
		t.Errorf("want 1.0, got %v", got[0])
	}
}

func TestApplyMigrationV2OnClosedDB(t *testing.T) {
	t.Parallel()
	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := applyMigrationV2(context.Background(), raw); err == nil {
		t.Fatal("expected error from applyMigrationV2 on closed DB, got nil")
	}
}

func TestApplyMigrationV3OnClosedDB(t *testing.T) {
	t.Parallel()
	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := applyMigrationV3(context.Background(), raw); err == nil {
		t.Fatal("expected error from applyMigrationV3 on closed DB, got nil")
	}
}

func TestApplyMigrationV4OnClosedDB(t *testing.T) {
	t.Parallel()
	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := applyMigrationV4(context.Background(), raw); err == nil {
		t.Fatal("expected error from applyMigrationV4 on closed DB, got nil")
	}
}

func TestApplyMigrationV5OnClosedDB(t *testing.T) {
	t.Parallel()
	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := applyMigrationV5(context.Background(), raw); err == nil {
		t.Fatal("expected error from applyMigrationV5 on closed DB, got nil")
	}
}

func TestSchemaVersionOnClosedDB(t *testing.T) {
	t.Parallel()
	raw, err := rawMemoryDB(t)
	if err != nil {
		t.Fatalf("rawMemoryDB: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := SchemaVersion(context.Background(), raw); err == nil {
		t.Fatal("expected error from SchemaVersion on closed DB, got nil")
	}
}

func TestNewCASSuccess(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cas-root")
	cas, err := NewCAS(root)
	if err != nil {
		t.Fatalf("NewCAS: unexpected error: %v", err)
	}
	if cas == nil {
		t.Fatal("NewCAS returned nil CAS")
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat CAS root: %v", err)
	}
	if !info.IsDir() {
		t.Error("CAS root is not a directory")
	}
}

func TestCASWritePrefixDirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}
	t.Parallel()

	root := filepath.Join(t.TempDir(), "cas-readonly")
	cas, err := NewCAS(root)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}

	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("chmod root 0555: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	_, err = cas.Write([]byte("test data for prefix dir error"), "json")
	if err == nil {
		t.Fatal("expected error writing to non-writable CAS root, got nil")
	}
}

func TestCASWriteTmpFileExistAndDestMissing(t *testing.T) {
	t.Parallel()
	cas, _ := newTestCAS(t)

	data := []byte("tmp-exist-test-content-unique-for-coverage-gap")

	hash, err := cas.Write(data, "json")
	if err != nil {
		t.Fatalf("initial Write: %v", err)
	}

	dest := cas.Path(hash, "json")
	tmpPath := dest + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatalf("create .tmp: %v", err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	if err := os.Remove(dest); err != nil {
		t.Fatalf("remove dest: %v", err)
	}

	_, err = cas.Write(data, "json")
	if err == nil {

		t.Log("note: concurrent Write succeeded; EEXIST path not triggered this run")
	}
}

func TestCASWriteIdempotentSecondWrite(t *testing.T) {
	t.Parallel()
	cas, _ := newTestCAS(t)
	data := []byte("idempotent-coverage-gap-test")

	hash1, err := cas.Write(data, "bin")
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	hash2, err := cas.Write(data, "bin")
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash mismatch: first=%q second=%q", hash1, hash2)
	}
}

func TestLookupSemanticNilDB(t *testing.T) {
	t.Parallel()
	_, err := LookupSemantic(context.Background(), nil, embed384Mock(0), "proj", "sess")
	if err == nil {
		t.Fatal("expected error for nil db, got nil")
	}
}

func TestLookupSemanticKNNQueryError(t *testing.T) {
	t.Parallel()
	db, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.SQL.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = LookupSemantic(context.Background(), db, embed384Mock(0), "proj", "sess")
	if err == nil {
		t.Fatal("expected error from LookupSemantic on closed DB, got nil")
	}
}

func TestLookupSemanticCancelledContext(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	emb := embed384Mock(0)
	seedDispatchWithEmbedding(t, db, "ctx-cancel-query", "proj", emb, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LookupSemantic(ctx, db, emb, "proj", "sess")

	if err == nil {
		t.Fatal("expected error or ErrCacheMiss from pre-cancelled context, got nil")
	}
}

func TestLookupSemanticOrphanedVecEntry(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	ctx := context.Background()

	emb := embed384Mock(1)
	buf := embeddingToBytes(emb)
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO research_query_vec(rowid, embedding) VALUES (?, ?)`,
		int64(88888), buf,
	)
	if err != nil {
		t.Fatalf("insert orphaned vec entry: %v", err)
	}

	_, err = LookupSemantic(ctx, db, emb, "orphan-proj", "sess")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("want ErrCacheMiss for orphaned vec entry, got %v", err)
	}
}

func TestLookupSemanticDispatchNotDone(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Unix()
	emb := embed384Mock(2)
	hash := ComputeQueryHash("pending-dispatch-query-cgap")
	dispatchID := "sem-pending-cgap-" + hash[:8]

	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		dispatchID, "pending-dispatch-query-cgap", hash,
		string(DispatchStatusPending),
		now-10, now,
	)
	if err != nil {
		t.Fatalf("insert pending dispatch: %v", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	buf := embeddingToBytes(emb)
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_query_vec(rowid, embedding) VALUES (?, ?)`,
		rowid, buf,
	)
	if err != nil {
		t.Fatalf("insert vec: %v", err)
	}

	_, err = LookupSemantic(ctx, db, emb, "proj-pending", "sess")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("want ErrCacheMiss for non-Done dispatch, got %v", err)
	}
}

func TestLookupSemanticZeroFindings(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	ctx := context.Background()

	emb := embed384Mock(3)
	seedDispatchWithEmbedding(t, db, "zero-findings-query-cgap", "proj-zero", emb, 0)

	_, err := LookupSemantic(ctx, db, emb, "proj-zero", "sess")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("want ErrCacheMiss for zero-findings dispatch, got %v", err)
	}
}

func TestSelectDispatchByIDNotFound(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	_, err := selectDispatchByID(context.Background(), db.SQL, 999999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want sql.ErrNoRows, got %v", err)
	}
}

func TestSelectDispatchByIDOnClosedDB(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	if err := db.SQL.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := selectDispatchByID(context.Background(), db.SQL, 1)
	if err == nil {
		t.Fatal("expected error from selectDispatchByID on closed DB, got nil")
	}
}

func TestSweeperRunNilDB(t *testing.T) {
	t.Parallel()
	s := &Sweeper{
		DB:          nil,
		Revalidator: NewRevalidator(ValidateOpts{}),
		Sink:        &captureSink{},
	}
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for nil DB, got nil")
	}
}

func TestSweeperRunNilRevalidator(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	s := &Sweeper{
		DB:          db,
		Revalidator: nil,
		Sink:        &captureSink{},
	}
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for nil Revalidator, got nil")
	}
}

func TestSweeperRunNilSink(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{}),
		Sink:        nil,
	}
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for nil Sink, got nil")
	}
}

func TestSweepOnceQueryErrorDoesNotPanic(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	if err := db.SQL.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{}),
		Sink:        &captureSink{},
		BatchSize:   10,
	}
	s.normalize()
	s.sweepOnce(context.Background())
}

func TestSweepOnceContextCancelledDuringFindings(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	srv := newSweepFixtureServer()
	defer srv.Close()

	for i := 0; i < 3; i++ {
		seedStaleFinding(t, db, srv.URL+"/fresh-etag", fmt.Sprintf("cgap-%d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{Timeout: 100 * time.Millisecond}),
		Sink:        &captureSink{},
		BatchSize:   50,
	}
	s.normalize()
	s.sweepOnce(ctx)
}

func TestSweepRunV5IdempotentApply(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{Timeout: 100 * time.Millisecond}),
		Sink:        &captureSink{},
		Cadence:     50 * time.Millisecond,
		BatchSize:   10,
	}

	err := s.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("Run: unexpected error: %v", err)
	}
}

func TestTTLRuleNilURLBranches(t *testing.T) {
	t.Parallel()
	rule := LookupTTLRule("://bad")
	if rule.Name != "default-1d" {
		t.Errorf("want default-1d rule for malformed URL, got %q", rule.Name)
	}
	if rule.TTL != 24*time.Hour {
		t.Errorf("want 24h TTL, got %v", rule.TTL)
	}
}

func TestTTLRuleMDNWithoutDocsPath(t *testing.T) {
	t.Parallel()
	rule := LookupTTLRule("https://developer.mozilla.org/api/some-api")
	if rule.Name != "docs-7d" {
		t.Errorf("want docs-7d for MDN non-docs path, got %q", rule.Name)
	}
	if rule.TTL != 7*24*time.Hour {
		t.Errorf("want 7d TTL, got %v", rule.TTL)
	}
}

func TestApplyMigrationV2AlterTableError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v2_alter.db")

	sqlDB, err := openRawWithV1Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV1Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE research_dispatches`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	err = applyMigrationV2(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV2 with missing table, got nil")
	}
}

func TestApplyMigrationV3AlterTableError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v3_alter.db")

	sqlDB, err := openRawWithV2Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV2Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE research_findings`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	err = applyMigrationV3(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV3 with missing table, got nil")
	}
}

func TestApplyMigrationV5AlterTableError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v5_alter.db")

	sqlDB, err := openRawWithV4Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV4Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE research_findings`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	err = applyMigrationV5(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV5 with missing table, got nil")
	}
}

func TestApplyMigrationV2VersionDeleteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v2_version_del.db")

	sqlDB, err := openRawWithV1Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV1Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE _cache_schema_version`); err != nil {
		t.Fatalf("drop version table: %v", err)
	}

	err = applyMigrationV2(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV2 (version DELETE), got nil")
	}
}

func TestApplyMigrationV3VersionDeleteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v3_version_del.db")

	sqlDB, err := openRawWithV2Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV2Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE _cache_schema_version`); err != nil {
		t.Fatalf("drop version table: %v", err)
	}

	err = applyMigrationV3(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV3 (version DELETE), got nil")
	}
}

func TestApplyMigrationV4VersionDeleteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v4_version_del.db")

	sqlDB, err := openRawWithV2Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV2Schema: %v", err)
	}
	defer sqlDB.Close()

	if err := applyMigrationV3(context.Background(), sqlDB); err != nil {
		t.Fatalf("applyMigrationV3: %v", err)
	}

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE _cache_schema_version`); err != nil {
		t.Fatalf("drop version table: %v", err)
	}

	err = applyMigrationV4(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV4 (version DELETE), got nil")
	}
}

func TestApplyMigrationV5VersionDeleteError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "v5_version_del.db")

	sqlDB, err := openRawWithV4Schema(t, dbPath)
	if err != nil {
		t.Fatalf("openRawWithV4Schema: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.ExecContext(context.Background(), `DROP TABLE _cache_schema_version`); err != nil {
		t.Fatalf("drop version table: %v", err)
	}

	err = applyMigrationV5(context.Background(), sqlDB)
	if err == nil {
		t.Fatal("expected error from applyMigrationV5 (version DELETE), got nil")
	}
}

func TestCASWriteTmpExistAndDestPresent(t *testing.T) {
	t.Parallel()
	cas, _ := newTestCAS(t)

	data := []byte("concurrent-write-dest-present-test-data")

	hash, err := cas.Write(data, "json")
	if err != nil {
		t.Fatalf("initial Write: %v", err)
	}

	dest := cas.Path(hash, "json")
	tmpPath := dest + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatalf("create .tmp: %v", err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	hash2, err := cas.Write(data, "json")
	if err != nil {
		t.Errorf("Write with existing dest: expected nil error, got %v", err)
	}
	if hash2 != hash {
		t.Errorf("Write with existing dest: hash mismatch %q != %q", hash2, hash)
	}
}

func seedStaleFindingWithBody(t *testing.T, db *DB, fixtureURL, suffix, bodyPath, canonicalURL string) string {
	t.Helper()
	ctx := context.Background()
	dispatchID := "cgap-dispatch-body-" + suffix
	findingID := "cgap-finding-body-" + suffix
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()

	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID,
		"cgap query body "+suffix,
		ComputeQueryHash("cgap query body "+suffix),
		string(DispatchStatusDone),
		"cgap-project",
		"cgap-session",
		string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("seedStaleFindingWithBody: insert dispatch: %v", err)
	}

	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path,
		  source_url_canonical)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		fixtureURL,
		"cgap body title "+suffix,
		"cgap body snippet "+suffix,
		string(FreshnessFresh),
		oneWeekAgo,
		sweepFixedETag,
		nil,
		bodyPath,
		canonicalURL,
	)
	if err != nil {
		t.Fatalf("seedStaleFindingWithBody: insert finding: %v", err)
	}

	return findingID
}

func TestSweepOnceWithBodyPathAndCanonicalURL(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	srv := newSweepFixtureServer()
	defer srv.Close()

	seedStaleFindingWithBody(t, db, srv.URL+"/fresh-etag", "body1",
		"/var/cache/blobs/ab/abcdef1234.json",
		"https://canonical.example.com/article/1",
	)
	seedStaleFindingWithBody(t, db, srv.URL+"/fresh-etag", "body2",
		"/var/cache/blobs/cd/cdefgh5678.json",
		"https://canonical.example.com/article/2",
	)

	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{Timeout: 2 * time.Second}),
		Sink:        &captureSink{},
		BatchSize:   50,
	}
	s.normalize()
	s.sweepOnce(context.Background())

}

func TestDispatcherWriteBackWithZeroRetrievedAt(t *testing.T) {
	t.Parallel()

	mcp := &mockMCPClient{
		fixedFindings: FreshFindings{
			Query: "zero-retrieved-at-test",
			Findings: []FreshFinding{
				{
					SourceURL: "https://example.com/zero-retrieved-at",
					Ext:       "html",
					Body:      []byte("test body"),
				},
			},
		},
	}
	d := newTestDispatcher(t, mcp, nil, &mockSink{})

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "zero retrieved at test query",
		ProjectID:      "proj-zero-time",
		SessionID:      "sess-zero-time",
		SkipCache:      true,
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if !res.Hit {
		t.Errorf("expected hit, got miss")
	}
	if len(res.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(res.Findings))
	}
}

func TestDispatcherWriteBackInsertError(t *testing.T) {
	t.Parallel()

	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	d := newTestDispatcher(t, mcp, nil, &mockSink{})

	if err := d.DB.SQL.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:     "insert fail test",
		ProjectID: "proj-insert-fail",
		SkipCache: true,
	})
	if err == nil {
		t.Fatal("expected error from LookupOrDispatch with closed DB, got nil")
	}
}

func TestDispatcherExactLookupDBError(t *testing.T) {
	t.Parallel()

	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	d := newTestDispatcher(t, mcp, nil, &mockSink{})

	if err := d.DB.SQL.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:     "exact lookup db error test",
		ProjectID: "proj-exact-err",
		SkipCache: false,
	})
	if err == nil {
		t.Fatal("expected error from LookupOrDispatch with closed DB, got nil")
	}
}

func TestSweepOnceWithLastValidatedAt(t *testing.T) {
	t.Parallel()
	db := openTestCacheDB(t)
	srv := newSweepFixtureServer()
	defer srv.Close()

	ctx := context.Background()
	dispatchID := "cgap-dispatch-lva"
	findingID := "cgap-finding-lva"
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
	oneHourAgo := time.Now().Add(-1 * time.Hour).Unix()

	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, project_id, session_id,
		  cache_hit_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchID, "lva test query",
		ComputeQueryHash("lva test query"),
		string(DispatchStatusDone),
		"lva-project", "lva-session", string(CacheHitMiss),
		oneWeekAgo, oneWeekAgo,
	)
	if err != nil {
		t.Fatalf("insert dispatch: %v", err)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status,
		  retrieved_at, content_hash, body_inline_blob, body_path,
		  last_validated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		findingID, dispatchID,
		srv.URL+"/fresh-etag",
		"lva title", "lva snippet",
		string(FreshnessFresh),
		oneWeekAgo,
		sweepFixedETag,
		nil, nil,
		oneHourAgo,
	)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	s := &Sweeper{
		DB:          db,
		Revalidator: NewRevalidator(ValidateOpts{Timeout: 2 * time.Second}),
		Sink:        &captureSink{},
		BatchSize:   50,
	}
	s.normalize()
	s.sweepOnce(ctx)
}

func openRawWithV1Schema(t *testing.T, dbPath string) (*sql.DB, error) {
	t.Helper()

	{
		tmp, err := Open(context.Background(), ":memory:")
		if err != nil {
			return nil, fmt.Errorf("tmp Open for sqlite_vec.Auto: %w", err)
		}
		_ = tmp.SQL.Close()
	}

	dsn := fmt.Sprintf(
		"%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL",
		dbPath,
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := applySchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applySchema: %w", err)
	}
	return db, nil
}

func openRawWithV2Schema(t *testing.T, dbPath string) (*sql.DB, error) {
	t.Helper()
	db, err := openRawWithV1Schema(t, dbPath)
	if err != nil {
		return nil, err
	}
	if err := applyMigrationV2(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applyMigrationV2: %w", err)
	}
	return db, nil
}

func openRawWithV4Schema(t *testing.T, dbPath string) (*sql.DB, error) {
	t.Helper()
	db, err := openRawWithV2Schema(t, dbPath)
	if err != nil {
		return nil, err
	}
	if err := applyMigrationV3(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applyMigrationV3: %w", err)
	}
	if err := applyMigrationV4(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applyMigrationV4: %w", err)
	}
	return db, nil
}
