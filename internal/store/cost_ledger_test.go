package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openMigratedCostStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cost.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleRow(idemKey string, ts time.Time, project, tier string, usd float64) CostLedgerRow {
	return CostLedgerRow{
		IdempotencyKey:      idemKey,
		TS:                  ts,
		Project:             project,
		Profile:             "orchestrator",
		Tier:                tier,
		Model:               "claude-opus-4-6",
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     0,
		CacheCreationTokens: 0,
		CostUSD:             usd,
		ConversationID:      "conv-1",
		SessionID:           "sess-1",
		RequestHash:         []byte{0x01, 0x02, 0x03},
	}
}

func TestInsertCostLedgerSuccess(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("idem-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.025)
	id, err := InsertCostLedger(s.DB(), row)
	if err != nil || id <= 0 {
		t.Fatalf("InsertCostLedger: id=%d err=%v", id, err)
	}
}

func TestInsertCostLedgerDuplicateIdempotencyReturnsErr(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("idem-dup", time.Now(), "internal-platform-x", "tier2-paygo", 0.05)
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	row.CostUSD = 0.10
	_, err := InsertCostLedger(s.DB(), row)
	if !errors.Is(err, ErrDuplicateIdempotency) {
		t.Fatalf("want ErrDuplicateIdempotency, got %v", err)
	}

	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") &&
		!strings.Contains(msg, "constraint failed") {
		t.Errorf("wrapped error missing SQL constraint detail: %q", msg)
	}
}

func TestNoDoubleChargeSymbolPresent(t *testing.T) {
	if err := noDoubleCharge(); !errors.Is(err, ErrDuplicateIdempotency) {
		t.Fatalf("noDoubleCharge must return ErrDuplicateIdempotency, got %v", err)
	}
}

func TestInsertCostLedgerEmptyIdempotencyKey(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("", time.Now(), "internal-platform-x", "tier2-paygo", 0.01)
	id, err := InsertCostLedger(s.DB(), row)
	if err == nil {
		t.Fatalf("want error for empty idempotency_key, got id=%d", id)
	}
	if !strings.Contains(err.Error(), "idempotency_key") {
		t.Errorf("error must mention idempotency_key, got %q", err)
	}
}

func TestInsertCostLedgerEmptyRequiredField(t *testing.T) {
	s := openMigratedCostStore(t)
	cases := []struct {
		name string
		mut  func(*CostLedgerRow)
	}{
		{"empty project", func(r *CostLedgerRow) { r.Project = "" }},
		{"empty profile", func(r *CostLedgerRow) { r.Profile = "" }},
		{"empty tier", func(r *CostLedgerRow) { r.Tier = "" }},
		{"empty model", func(r *CostLedgerRow) { r.Model = "" }},
	}
	for i, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			row := sampleRow("idem-required-"+tc.name, time.Now(), "internal-platform-x", "tier2-paygo", 0.01)
			tc.mut(&row)
			id, err := InsertCostLedger(s.DB(), row)
			if err == nil {
				t.Fatalf("case %d (%s): want error, got id=%d", i, tc.name, id)
			}
		})
	}
}

func TestInsertCostLedgerConcurrent(t *testing.T) {
	s := openMigratedCostStore(t)
	const N = 8
	const idemKey = "idem-concurrent"

	var wg sync.WaitGroup
	wg.Add(N)
	results := make([]error, N)
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			row := sampleRow(idemKey, time.Now(), "internal-platform-x", "tier2-paygo", float64(i)*0.01)
			_, err := InsertCostLedger(s.DB(), row)
			results[i] = err
		}()
	}
	close(start)
	wg.Wait()

	successes := 0
	dups := 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrDuplicateIdempotency):
			dups++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("want exactly 1 success, got %d", successes)
	}
	if dups != N-1 {
		t.Errorf("want %d duplicate errors, got %d", N-1, dups)
	}
}

func TestInsertCostLedgerTSRoundTripMillisecondPrecision(t *testing.T) {
	s := openMigratedCostStore(t)
	original := time.Now()
	row := sampleRow("idem-ts", original, "internal-platform-x", "tier2-paygo", 0.01)
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := QueryAllRecentCosts(s.DB(), original.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	delta := rows[0].TS.Sub(original)
	if delta > time.Millisecond || delta < -time.Millisecond {
		t.Errorf("ts round-trip drift %v exceeds 1ms (in=%v out=%v)",
			delta, original, rows[0].TS)
	}
}

func TestQueryCostInWindowAggregates(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	rows := []CostLedgerRow{
		sampleRow("w-1", now.Add(-10*time.Minute), "internal-platform-x", "tier2-paygo", 0.10),
		sampleRow("w-2", now.Add(-5*time.Minute), "internal-platform-x", "tier2-paygo", 0.20),
		sampleRow("w-3", now, "internal-platform-x", "tier2-paygo", 0.30),
		sampleRow("w-old", now.Add(-2*time.Hour), "internal-platform-x", "tier2-paygo", 9.99),
		sampleRow("w-othertier", now, "internal-platform-x", "tier1-bypass", 9.99),
	}
	for _, r := range rows {
		if _, err := InsertCostLedger(s.DB(), r); err != nil {
			t.Fatalf("seed insert %s: %v", r.IdempotencyKey, err)
		}
	}
	totalUSD, count, err := QueryCostInWindow(s.DB(), "internal-platform-x", "orchestrator", "tier2-paygo", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryCostInWindow: %v", err)
	}
	const want = 0.10 + 0.20 + 0.30
	if !floatEqual(totalUSD, want, 1e-9) {
		t.Errorf("total=%v want %v", totalUSD, want)
	}
	if count != 3 {
		t.Errorf("count=%d want 3", count)
	}
}

func TestQueryCostInWindowEmpty(t *testing.T) {
	s := openMigratedCostStore(t)
	totalUSD, count, err := QueryCostInWindow(s.DB(), "no-such", "orchestrator", "tier2-paygo", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryCostInWindow: %v", err)
	}
	if totalUSD != 0 || count != 0 {
		t.Errorf("want (0, 0), got (%v, %d)", totalUSD, count)
	}
}

func TestQueryCostBySessionAggregates(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	mk := func(idem string, sess string, usd float64) CostLedgerRow {
		r := sampleRow(idem, now, "internal-platform-x", "tier2-paygo", usd)
		r.SessionID = sess
		return r
	}
	rows := []CostLedgerRow{
		mk("s-1", "sess-A", 0.05),
		mk("s-2", "sess-A", 0.10),
		mk("s-3", "sess-B", 9.99),
	}
	for _, r := range rows {
		if _, err := InsertCostLedger(s.DB(), r); err != nil {
			t.Fatalf("seed insert %s: %v", r.IdempotencyKey, err)
		}
	}
	totalUSD, count, err := QueryCostBySession(s.DB(), "sess-A")
	if err != nil {
		t.Fatalf("QueryCostBySession: %v", err)
	}
	const want = 0.05 + 0.10
	if !floatEqual(totalUSD, want, 1e-9) {
		t.Errorf("total=%v want %v", totalUSD, want)
	}
	if count != 2 {
		t.Errorf("count=%d want 2", count)
	}
}

func TestQueryCostBySessionEmpty(t *testing.T) {
	s := openMigratedCostStore(t)
	totalUSD, count, err := QueryCostBySession(s.DB(), "no-such-session")
	if err != nil {
		t.Fatalf("QueryCostBySession: %v", err)
	}
	if totalUSD != 0 || count != 0 {
		t.Errorf("want (0, 0), got (%v, %d)", totalUSD, count)
	}
}

func TestQueryAllRecentCostsEmpty(t *testing.T) {
	s := openMigratedCostStore(t)
	rows, err := QueryAllRecentCosts(s.DB(), time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want 0 rows, got %d", len(rows))
	}
}

func TestQueryAllRecentCostsOrder(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	for i, off := range []time.Duration{-30 * time.Minute, -10 * time.Minute, -20 * time.Minute} {
		r := sampleRow(fmt.Sprintf("ord-%d", i), now.Add(off), "internal-platform-x", "tier2-paygo", 0.01)
		if _, err := InsertCostLedger(s.DB(), r); err != nil {
			t.Fatalf("seed insert: %v", err)
		}
	}
	rows, err := QueryAllRecentCosts(s.DB(), now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].TS.Before(rows[i-1].TS) {
			t.Errorf("rows not ascending by ts at index %d (%v < %v)",
				i, rows[i].TS, rows[i-1].TS)
		}
	}
}

// TestIsUniqueViolationStringFallbacks — the defense-in-depth fallbacks
// match a string carrying "UNIQUE constraint failed" or
// "constraint failed: cost_ledger.idempotency_key" even when the typed
// sqlite3.CONSTRAINT_UNIQUE is absent. Documents the contract: future
// driver upgrades that drop the typed code MUST still surface
// ErrDuplicateIdempotency via the message text.
func TestIsUniqueViolationStringFallbacks(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unique-constraint-failed", errors.New("UNIQUE constraint failed: cost_ledger.idempotency_key"), true},
		{"constraint-failed-cost-ledger", errors.New("sqlite3: constraint failed: cost_ledger.idempotency_key"), true},
		{"unrelated-error", errors.New("disk full"), false},
		{"empty-error", errors.New(""), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := isUniqueViolation(tc.err)
			if got != tc.want {
				t.Errorf("isUniqueViolation(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestInsertCostLedgerNonUniqueSQLError(t *testing.T) {
	s := openMigratedCostStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	row := sampleRow("idem-closed", time.Now(), "internal-platform-x", "tier2-paygo", 0.01)
	_, err := InsertCostLedger(s.DB(), row)
	if err == nil {
		t.Fatalf("want error after Close, got nil")
	}
	if errors.Is(err, ErrDuplicateIdempotency) {
		t.Errorf("non-UNIQUE error must NOT match ErrDuplicateIdempotency: %v", err)
	}
	if !strings.Contains(err.Error(), "insert cost_ledger") {
		t.Errorf("error must wrap with insert cost_ledger: %v", err)
	}
}

func TestQueryCostInWindowSQLError(t *testing.T) {
	s := openMigratedCostStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, err := QueryCostInWindow(s.DB(), "p", "pr", "t", time.Now())
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "query cost_in_window") {
		t.Errorf("want wrapped error, got %v", err)
	}
}

func TestQueryCostBySessionSQLError(t *testing.T) {
	s := openMigratedCostStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, err := QueryCostBySession(s.DB(), "sess")
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "query cost_by_session") {
		t.Errorf("want wrapped error, got %v", err)
	}
}

func TestQueryAllRecentCostsSQLError(t *testing.T) {
	s := openMigratedCostStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := QueryAllRecentCosts(s.DB(), time.Now())
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "query all_recent_costs") {
		t.Errorf("want wrapped error, got %v", err)
	}
}

func floatEqual(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

func TestInsertCostLedgerNilRequestHashStoredAsNull(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("idem-nilhash", time.Now(), "internal-platform-x", "tier2-paygo", 0.01)
	row.RequestHash = nil
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rows, err := QueryAllRecentCosts(s.DB(), time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if len(rows[0].RequestHash) != 0 {
		t.Errorf("want nil/empty RequestHash, got %v", rows[0].RequestHash)
	}
}

func TestQueryCostInWindowBoundaryInclusion(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	since := now.Add(-1 * time.Hour)

	inWindow := []CostLedgerRow{
		sampleRow("bnd-w-1", now.Add(-30*time.Minute), "internal-platform-x", "tier2-paygo", 0.10),
		sampleRow("bnd-w-2", now.Add(-10*time.Minute), "internal-platform-x", "tier2-paygo", 0.20),
	}
	for _, r := range inWindow {
		if _, err := InsertCostLedger(s.DB(), r); err != nil {
			t.Fatalf("seed insert %s: %v", r.IdempotencyKey, err)
		}
	}

	totalBefore, countBefore, err := QueryCostInWindow(s.DB(), "internal-platform-x", "orchestrator", "tier2-paygo", since)
	if err != nil {
		t.Fatalf("pre-boundary query: %v", err)
	}

	boundaryRow := sampleRow("idem-boundary", since, "internal-platform-x", "tier2-paygo", 0.0001)
	if _, err := InsertCostLedger(s.DB(), boundaryRow); err != nil {
		t.Fatalf("insert boundary row: %v", err)
	}

	totalAfter, countAfter, err := QueryCostInWindow(s.DB(), "internal-platform-x", "orchestrator", "tier2-paygo", since)
	if err != nil {
		t.Fatalf("post-boundary query: %v", err)
	}

	if countAfter != countBefore+1 {
		t.Errorf("boundary row not counted: countBefore=%d countAfter=%d (want countBefore+1)", countBefore, countAfter)
	}
	wantTotal := totalBefore + 0.0001
	if !floatEqual(totalAfter, wantTotal, 1e-9) {
		t.Errorf("boundary row cost not summed: totalBefore=%v totalAfter=%v want %v", totalBefore, totalAfter, wantTotal)
	}
}

func TestQueryCostBySessionEmptyMatchesZero(t *testing.T) {
	s := openMigratedCostStore(t)

	row := sampleRow("idem-empty-sess", time.Now(), "internal-platform-x", "tier2-paygo", 0.05)
	row.SessionID = ""
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}

	total, count, err := QueryCostBySession(s.DB(), "")
	if err != nil {
		t.Fatalf("QueryCostBySession: %v", err)
	}
	if total != 0 || count != 0 {
		t.Errorf("QueryCostBySession(\"\") = (%v, %d), want (0, 0)", total, count)
	}
}

func TestInsertCostLedgerNilRequestHashIsSQLNull(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("idem-nilhash-sql", time.Now(), "internal-platform-x", "tier2-paygo", 0.001)
	row.RequestHash = nil
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var isNull bool
	if err := s.DB().QueryRow(
		"SELECT request_hash IS NULL FROM cost_ledger WHERE idempotency_key=?",
		"idem-nilhash-sql",
	).Scan(&isNull); err != nil {
		t.Fatalf("probe IS NULL: %v", err)
	}
	if !isNull {
		t.Error("nil RequestHash should be stored as SQL NULL, not zero-length BLOB")
	}
}

func TestQueryCostInWindowMultiProjectMultiTier(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	type spec struct {
		idem    string
		offset  time.Duration
		project string
		profile string
		tier    string
		usd     float64
	}
	specs := []spec{
		{"a-1", -1 * time.Hour, "internal-platform-x", "orchestrator", "tier2-paygo", 0.10},
		{"a-2", -2 * time.Hour, "internal-platform-x", "orchestrator", "tier2-paygo", 0.20},
		{"a-3", -3 * time.Hour, "internal-platform-x", "orchestrator", "tier3-gemini", 0.05},
		{"n-1", -1 * time.Hour, "nexus", "orchestrator", "tier2-paygo", 0.40},
		{"n-2", -25 * time.Hour, "nexus", "orchestrator", "tier2-paygo", 0.99},
		{"x-1", -1 * time.Hour, "internal-platform-x", "swarm-coder", "tier2-paygo", 0.08},
	}
	for _, sp := range specs {
		row := sampleRow(sp.idem, now.Add(sp.offset), sp.project, sp.tier, sp.usd)
		row.Profile = sp.profile
		if _, err := InsertCostLedger(s.DB(), row); err != nil {
			t.Fatalf("seed %s: %v", sp.idem, err)
		}
	}

	total, count, err := QueryCostInWindow(s.DB(), "internal-platform-x", "orchestrator", "tier2-paygo", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("QueryCostInWindow: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if total < 0.299 || total > 0.301 {
		t.Errorf("total = %f, want 0.30", total)
	}

	total, count, _ = QueryCostInWindow(s.DB(), "nexus", "orchestrator", "tier2-paygo", now.Add(-24*time.Hour))
	if count != 1 || total < 0.399 || total > 0.401 {
		t.Errorf("nexus 24h: count=%d total=%f", count, total)
	}

	total, count, _ = QueryCostInWindow(s.DB(), "nexus", "orchestrator", "tier2-paygo", now.Add(-30*24*time.Hour))
	if count != 2 || total < 1.389 || total > 1.391 {
		t.Errorf("nexus 30d: count=%d total=%f", count, total)
	}

	total, count, _ = QueryCostInWindow(s.DB(), "internal-platform-x", "swarm-coder", "tier2-paygo", now.Add(-24*time.Hour))
	if count != 1 || total < 0.079 || total > 0.081 {
		t.Errorf("swarm-coder isolation: count=%d total=%f", count, total)
	}
}

// TestQueryCostBySessionLifetime — plan F-2 test #2.
// 4 rows on session-X (sum 0.15) + 1 row on session-Y (99.99).
// session-Y MUST NOT contribute to session-X totals.
func TestQueryCostBySessionLifetime(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	for i, usd := range []float64{0.01, 0.02, 0.04, 0.08} {
		row := sampleRow(fmt.Sprintf("s-%d", i), now.Add(-time.Duration(i)*time.Hour), "internal-platform-x", "tier2-paygo", usd)
		row.SessionID = "session-X"
		if _, err := InsertCostLedger(s.DB(), row); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	row := sampleRow("other-1", now, "internal-platform-x", "tier2-paygo", 99.99)
	row.SessionID = "session-Y"
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	total, count, err := QueryCostBySession(s.DB(), "session-X")
	if err != nil {
		t.Fatalf("QueryCostBySession: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if total < 0.149 || total > 0.151 {
		t.Errorf("total = %f, want 0.15", total)
	}
}

func TestQueryAllRecentCostsOrderedAndFiltered(t *testing.T) {
	s := openMigratedCostStore(t)
	now := time.Now()
	for i, off := range []time.Duration{-40 * 24 * time.Hour, -25 * 24 * time.Hour, -1 * time.Hour} {
		row := sampleRow(fmt.Sprintf("r-%d", i), now.Add(off), "internal-platform-x", "tier2-paygo", 0.10)
		if _, err := InsertCostLedger(s.DB(), row); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	rows, err := QueryAllRecentCosts(s.DB(), now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2 (filter excluded -40d)", len(rows))
	}
	if rows[0].TS.After(rows[1].TS) {
		t.Errorf("rows not ascending: %v then %v", rows[0].TS, rows[1].TS)
	}
}

func TestQueryAllRecentCostsFutureTimestamp(t *testing.T) {
	s := openMigratedCostStore(t)
	row := sampleRow("future-ts-1", time.Now(), "internal-platform-x", "tier2-paygo", 0.01)
	if _, err := InsertCostLedger(s.DB(), row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err := QueryAllRecentCosts(s.DB(), time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts with future since: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows with future since, got %d", len(rows))
	}
}

func TestInsertCostLedger_PersistsProvider(t *testing.T) {
	db := openMigratedCostStore(t).DB()
	id, err := InsertCostLedger(db, CostLedgerRow{
		IdempotencyKey: "idem-prov-1",
		TS:             time.UnixMilli(1700000000000),
		Project:        "internal-platform-x",
		Profile:        "worker-code",
		Provider:       "deepseek-direct",
		Tier:           "openai-compat",
		Model:          "deepseek-chat",
		InputTokens:    10,
		OutputTokens:   20,
		CostUSD:        0.01,
	})
	if err != nil {
		t.Fatalf("InsertCostLedger: %v", err)
	}
	if id <= 0 {
		t.Fatalf("InsertCostLedger returned id %d", id)
	}
	rows, err := QueryAllRecentCosts(db, time.UnixMilli(0))
	if err != nil {
		t.Fatalf("QueryAllRecentCosts: %v", err)
	}
	var got *CostLedgerRow
	for i := range rows {
		if rows[i].IdempotencyKey == "idem-prov-1" {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatal("row idem-prov-1 not returned by QueryAllRecentCosts")
	}
	if got.Provider != "deepseek-direct" {
		t.Errorf("round-tripped Provider = %q, want deepseek-direct", got.Provider)
	}
}
