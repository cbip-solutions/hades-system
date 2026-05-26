package tessera

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWitnessDoctorReportsMissingKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	doc := WitnessDoctor{Witness: w, RotationCadence: 90 * 24 * time.Hour}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Severity != SeverityFail {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityFail)
	}
	if !strings.Contains(got.Message, "no witness key") {
		t.Errorf("Message = %q, want to mention 'no witness key'", got.Message)
	}
}

func TestWitnessDoctorReportsHealthyKey(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	doc := WitnessDoctor{
		Witness:         w,
		RotationCadence: 90 * 24 * time.Hour,
	}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Severity != SeverityOK {
		t.Errorf("Severity = %v, want %v (just-generated key)", got.Severity, SeverityOK)
	}
}

func TestCheckpointDoctorReportsEmptyLog(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	doc := CheckpointDoctor{
		Checkpoint:       cp,
		FreshnessCadence: 60 * time.Second,
	}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if got.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityWarn)
	}
}

func TestSeverityStringPinsCanonicalTags(t *testing.T) {
	cases := []struct {
		s    Severity
		want string
	}{
		{SeverityOK, "OK"},
		{SeverityWarn, "WARN"},
		{SeverityFail, "FAIL"},
		{Severity(99), "UNKNOWN"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

// TestWitnessDoctorReportsNilWitness verifies the nil-witness fast
// path. cmd/zen-swarm-ctld is the only producer of WitnessDoctor in
// production and always sets Witness — but Phase J's doctor surface is
// constructed by struct-literal in the renderer, so a future call site
// that forgets to wire Witness MUST fail loud here rather than NPE.
func TestWitnessDoctorReportsNilWitness(t *testing.T) {
	doc := WitnessDoctor{Witness: nil, RotationCadence: 90 * 24 * time.Hour}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Severity != SeverityFail {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityFail)
	}
	if !strings.Contains(got.Message, "not configured") {
		t.Errorf("Message = %q, want to mention 'not configured'", got.Message)
	}
}

func TestWitnessDoctorReportsCadenceExceeded(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	doc := WitnessDoctor{
		Witness:         w,
		RotationCadence: 1 * time.Second,

		LastRotatedAt: time.Now().Add(-2 * time.Second),
	}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityWarn)
	}
	if !strings.Contains(got.Message, "exceeds cadence") {
		t.Errorf("Message = %q, want to mention 'exceeds cadence'", got.Message)
	}
	if got.Details["last_rotated_age"] == "" {
		t.Error("Details missing last_rotated_age")
	}
	if got.Details["pubkey_fingerprint"] == "" {
		t.Error("Details missing pubkey_fingerprint")
	}
}

// TestCheckpointDoctorReportsNilCheckpoint verifies the nil-Checkpoint
// fast path. Phase J's renderer constructs CheckpointDoctor from the
// Manager's checkpoint accessor; a nil-checkpoint Manager (impossible
// today but enforced via this test) MUST fail loud.
func TestCheckpointDoctorReportsNilCheckpoint(t *testing.T) {
	doc := CheckpointDoctor{Checkpoint: nil, FreshnessCadence: 60 * time.Second}
	got, err := doc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Severity != SeverityFail {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityFail)
	}
	if !strings.Contains(got.Message, "not configured") {
		t.Errorf("Message = %q, want to mention 'not configured'", got.Message)
	}
}

func TestCheckpointDoctorReportsCadenceExceeded(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	cs := NewCoSigner(w, cp)

	stalePast := time.Now().UTC().Add(-10 * time.Minute)
	sth := STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab), Timestamp: stalePast}
	if err := cs.OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("OnSTH: %v", err)
	}
	doc := CheckpointDoctor{
		Checkpoint:       cp,
		FreshnessCadence: 30 * time.Second,
	}
	deadline := time.Now().Add(5 * time.Second)
	var got DoctorResult
	for time.Now().Before(deadline) {
		var err error
		got, err = doc.Check(context.Background())
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if got.Severity == SeverityWarn && strings.Contains(got.Message, "exceeds cadence") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want %v", got.Severity, SeverityWarn)
	}
	if !strings.Contains(got.Message, "exceeds cadence") {
		t.Errorf("Message = %q, want to mention 'exceeds cadence'", got.Message)
	}
	for _, k := range []string{"size", "latest_age", "latest_project_id"} {
		if got.Details[k] == "" {
			t.Errorf("Details missing %s", k)
		}
	}
	if got.Details["latest_project_id"] != "p1" {
		t.Errorf("latest_project_id = %q, want p1", got.Details["latest_project_id"])
	}
}

func TestCheckpointDoctorReportsClosedLog(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	if err := cp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	doc := CheckpointDoctor{Checkpoint: cp, FreshnessCadence: 60 * time.Second}
	_, err := doc.Check(context.Background())
	if err == nil {
		t.Fatal("Check on closed checkpoint: want error, got nil")
	}
	if !errors.Is(err, ErrCheckpointLogClosed) {
		t.Errorf("Check on closed checkpoint: want ErrCheckpointLogClosed, got %v", err)
	}
}

func TestCheckpointLatestRoundTrip(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)

	if _, _, err := cp.Latest(context.Background()); !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("Latest on empty log: want ErrCheckpointNotFound, got %v", err)
	}

	cs := NewCoSigner(w, cp)
	sth := STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab), Timestamp: time.Now().UTC()}
	if err := cs.OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("OnSTH: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var (
		gotSigned SignedSTH
		gotSize   uint64
		gotErr    error
	)
	for time.Now().Before(deadline) {
		gotSigned, gotSize, gotErr = cp.Latest(context.Background())
		if gotErr == nil && gotSize > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if gotErr != nil {
		t.Fatalf("Latest after publish: %v", gotErr)
	}
	if gotSize == 0 {
		t.Error("Latest returned size=0 after successful publish")
	}
	if gotSigned.STH.ProjectID != "p1" {
		t.Errorf("Latest STH ProjectID = %q, want p1", gotSigned.STH.ProjectID)
	}
	if len(gotSigned.Signature) == 0 {
		t.Error("Latest returned empty signature")
	}
}

func TestCheckpointLatestReturnsClosedErr(t *testing.T) {
	cp, _ := newTempCheckpoint(t)
	if err := cp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := cp.Latest(context.Background()); !errors.Is(err, ErrCheckpointLogClosed) {
		t.Errorf("Latest on closed log: want ErrCheckpointLogClosed, got %v", err)
	}
}

func TestCheckpointDoctorReportsHealthyAfterAppend(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cp, _ := newTempCheckpoint(t)
	cs := NewCoSigner(w, cp)
	sth := STH{ProjectID: "p1", Size: 1, RootHash: bytes32(0xab), Timestamp: time.Now().UTC()}
	if err := cs.OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("OnSTH: %v", err)
	}
	doc := CheckpointDoctor{
		Checkpoint:       cp,
		FreshnessCadence: 60 * time.Second,
	}

	deadline := time.Now().Add(5 * time.Second)
	var got DoctorResult
	for time.Now().Before(deadline) {
		var err error
		got, err = doc.Check(context.Background())
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if got.Severity == SeverityOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got.Severity != SeverityOK {
		t.Errorf("Severity = %v, want %v (recent checkpoint); message=%q details=%v",
			got.Severity, SeverityOK, got.Message, got.Details)
	}
}
