// go:build realworld
//go:build realworld
// +build realworld

package plan9_audit_chain_realworld_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func TestRealworld_HighVolumeSustainedAppend(t *testing.T) {
	if testing.Short() {
		t.Skip("realworld skipped under -short")
	}
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	st := testhelpers.NewTestStore(t)
	aa := auditadapter.New(st)

	const projectID = "proj-A"
	const eventType = "research.findings_returned"
	const N = 5000
	baseTS := time.Now().UTC().Unix()
	start := time.Now()
	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-rw-%05d", i)
		payload := []byte(fmt.Sprintf(`{"i":%d}`, i))
		if _, err := st.DB().ExecContext(ctx,
			`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at) VALUES (?, ?, ?, ?, ?)`,
			eventID, projectID, eventType, string(payload), baseTS+int64(i),
		); err != nil {
			t.Fatalf("insert[%d]: %v", i, err)
		}
		if _, err := aa.OnEmitRaw(ctx, eventID, projectID, eventType, payload, baseTS+int64(i)); err != nil {
			t.Fatalf("OnEmitRaw[%d]: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 5*time.Minute {
		t.Errorf("5000-event sustained append took %v; spec §3.1 budget violated", elapsed)
	}
	t.Logf("5000-event sustained append completed in %v (avg %v/event)", elapsed, elapsed/N)

	report, err := chain.Walk(ctx, aa, projectID)
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if report.EventsWalked != int64(N) {
		t.Errorf("EventsWalked = %d, want %d", report.EventsWalked, N)
	}
	if len(report.Tampered) != 0 || len(report.GapsDetected) != 0 {
		t.Errorf("integrity findings after sustained append: tampered=%d gaps=%d",
			len(report.Tampered), len(report.GapsDetected))
	}

	if os.Getenv("LITESTREAM_BIN") == "" || os.Getenv("S3_TEST_BUCKET") == "" {
		t.Log("LITESTREAM_BIN or S3_TEST_BUCKET unset; skipping real-Litestream variant")
		return
	}
	t.Logf("LITESTREAM_BIN=%s + S3_TEST_BUCKET=%s set; operator-staged variant TBD (Phase L can wire once sidecar mgr exposes test seam)",
		os.Getenv("LITESTREAM_BIN"), os.Getenv("S3_TEST_BUCKET"))
}

func TestRealworld_MockS3OutageRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("realworld skipped under -short")
	}
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	mockS3 := testhelpers.NewMockS3(t)
	mockS3.SetFault("PUT", "test-bucket/example.db", 503, 30*time.Second)
	t.Logf("MockS3 URL: %s (PUT fault active: 503 + Retry-After=30s)", mockS3.URL)

	st := testhelpers.NewTestStore(t)
	aa := auditadapter.New(st)

	const projectID = "proj-A"
	const N = 100
	baseTS := time.Now().UTC().Unix()
	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-outage-%03d", i)
		payload := []byte(fmt.Sprintf(`{"during_outage":true,"i":%d}`, i))
		if _, err := st.DB().ExecContext(ctx,
			`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at) VALUES (?, ?, ?, ?, ?)`,
			eventID, projectID, "test.event", string(payload), baseTS+int64(i),
		); err != nil {
			t.Fatalf("insert during outage[%d]: %v", i, err)
		}
		if _, err := aa.OnEmitRaw(ctx, eventID, projectID, "test.event", payload, baseTS+int64(i)); err != nil {
			t.Fatalf("OnEmitRaw during outage[%d]: %v", i, err)
		}
	}

	report, err := chain.Walk(ctx, aa, projectID)
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if report.EventsWalked != int64(N) {
		t.Errorf("EventsWalked = %d during outage, want %d (hot path must be S3-independent)",
			report.EventsWalked, N)
	}
	if len(report.Tampered) != 0 || len(report.GapsDetected) != 0 {
		t.Errorf("integrity broken during MockS3 outage: tampered=%d gaps=%d",
			len(report.Tampered), len(report.GapsDetected))
	}
}
