package augment_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/augment"
)

type recordingChainStore struct {
	tip           string
	updateCols    []string
	updateRecords []updateColRecord
	leafIDs       []string
	leaves        []string
	leafInputs    []augment.TesseraLeafInput
	tipErr        error
	updateErr     error
	leafErr       error
	appendErr     error
	tipReads      atomic.Int32
	leavesAppend  atomic.Int32
}

func (r *recordingChainStore) GetChainTip(_ context.Context) (string, error) {
	r.tipReads.Add(1)
	if r.tipErr != nil {
		return "", r.tipErr
	}
	return r.tip, nil
}

type updateColRecord struct {
	EventID     string
	PrevHash    string
	EventType   string
	Payload     []byte
	EmittedAt   int64
	RecordHash  string
	PartitionID string
}

func (r *recordingChainStore) UpdateChainColumns(_ context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updateCols = append(r.updateCols, eventID+"|"+prevHash+"|"+recordHash+"|"+partitionID)
	r.updateRecords = append(r.updateRecords, updateColRecord{
		EventID:     eventID,
		PrevHash:    prevHash,
		EventType:   eventType,
		Payload:     append([]byte(nil), payload...),
		EmittedAt:   emittedAt,
		RecordHash:  recordHash,
		PartitionID: partitionID,
	})
	return nil
}
func (r *recordingChainStore) UpdateTesseraLeafID(_ context.Context, eventID, leafID string) error {
	if r.leafErr != nil {
		return r.leafErr
	}
	r.leafIDs = append(r.leafIDs, eventID+"|"+leafID)
	return nil
}
func (r *recordingChainStore) AppendTesseraLeaf(_ context.Context, in augment.TesseraLeafInput) (string, error) {
	r.leavesAppend.Add(1)
	if r.appendErr != nil {
		return "", r.appendErr
	}
	leafID := fmt.Sprintf("leaf-%d", r.leavesAppend.Load())
	r.leaves = append(r.leaves, in.ProjectID+"|"+in.Partition+"|"+leafID)
	r.leafInputs = append(r.leafInputs, in)
	return leafID, nil
}

func TestAuditAnchor_EmitHappyPath(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip}
	clk := fixedClock{t: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
	a := augment.NewAuditAnchor(store, clk)

	auditEventID, err := a.Emit(context.Background(), augment.EventAugmentationStarted,
		[]byte(`{"session_id":"s1","project":"internal-platform-x"}`), "internal-platform-x")
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	parts := strings.Split(auditEventID, ":")
	if len(parts) != 3 {
		t.Fatalf("anchor format: want 3 parts, got %d in %q", len(parts), auditEventID)
	}
	if parts[0] != "2026_05" {
		t.Errorf("partition: want 2026_05, got %q", parts[0])
	}
	if !strings.HasPrefix(parts[1], "evt-") {
		t.Errorf("eventID: want evt- prefix, got %q", parts[1])
	}
	if len(parts[2]) < 16 {
		t.Errorf("recordHash: want >=16 chars, got %d", len(parts[2]))
	}
	if store.tipReads.Load() != 1 {
		t.Errorf("tip reads: want 1, got %d", store.tipReads.Load())
	}
	if len(store.updateCols) != 1 {
		t.Errorf("updateChainColumns: want 1, got %d", len(store.updateCols))
	}
	if store.leavesAppend.Load() != 1 {
		t.Errorf("AppendTesseraLeaf: want 1, got %d", store.leavesAppend.Load())
	}
}

func TestAuditAnchor_AllSevenEventTypes(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip}
	clk := fixedClock{t: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
	a := augment.NewAuditAnchor(store, clk)

	events := []augment.EventType{
		augment.EventAugmentationStarted,
		augment.EventAugmentationCompleted,
		augment.EventAugmentationTruncated,
		augment.EventAugmentationSkipped,
		augment.EventKGQueryDispatched,
		augment.EventCrossProjectQueryFiltered,
		augment.EventAugmentationOverridden,
	}
	for _, ev := range events {
		_, err := a.Emit(context.Background(), ev, []byte(`{}`), "p")
		if err != nil {
			t.Errorf("Emit %s: %v", ev, err)
		}
	}
	if len(store.updateCols) != 7 {
		t.Errorf("expected 7 chain updates, got %d", len(store.updateCols))
	}
}

func TestAuditAnchor_PartitionFormat(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip}
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"jan", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2026_01"},
		{"dec", time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC), "2025_12"},
		{"feb", time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC), "2027_02"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := augment.NewAuditAnchor(store, fixedClock{t: c.t})
			anchor, err := a.Emit(context.Background(), augment.EventAugmentationStarted, []byte(`{}`), "p")
			if err != nil {
				t.Fatalf("Emit: %v", err)
			}
			parts := strings.Split(anchor, ":")
			if parts[0] != c.want {
				t.Errorf("partition: want %q, got %q", c.want, parts[0])
			}
		})
	}
}

func TestAuditAnchor_TipReadErrorPropagates(t *testing.T) {
	store := &recordingChainStore{tipErr: errors.New("tip down")}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err == nil || !contains(err.Error(), "tip down") {
		t.Fatalf("expected tip error to propagate, got %v", err)
	}
}

func TestAuditAnchor_UpdateColumnsErrorPropagates(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip, updateErr: errors.New("write fail")}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err == nil || !contains(err.Error(), "write fail") {
		t.Fatalf("expected update error, got %v", err)
	}
}

func TestAuditAnchor_TesseraAppendErrorPropagates(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip, appendErr: errors.New("tessera fail")}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err == nil || !contains(err.Error(), "tessera fail") {
		t.Fatalf("expected tessera error, got %v", err)
	}
}

func TestAuditAnchor_UpdateLeafIDErrorPropagates(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip, leafErr: errors.New("leaf id fail")}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err == nil || !contains(err.Error(), "leaf id fail") {
		t.Fatalf("expected leaf-id error, got %v", err)
	}
}

func TestAuditAnchor_EventIDUnique(t *testing.T) {
	store := &recordingChainStore{tip: validHexTip}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	id1, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Error("expected unique eventIDs across emits")
	}
}

func TestAuditAnchor_GenesisTipHandled(t *testing.T) {
	store := &recordingChainStore{}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err != nil {
		t.Fatalf("genesis emit: %v", err)
	}
}

type rawEmptyTipStore struct {
	recordingChainStore
}

func (r *rawEmptyTipStore) GetChainTip(_ context.Context) (string, error) {
	r.tipReads.Add(1)
	return "", nil
}

func TestAuditAnchor_RawEmptyTipFallback(t *testing.T) {
	store := &rawEmptyTipStore{}
	a := augment.NewAuditAnchor(store, fixedClock{t: time.Now()})
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err != nil {
		t.Fatalf("raw-empty tip: %v", err)
	}
}

func TestNewAuditAnchor_NilClockDefaults(t *testing.T) {
	store := &recordingChainStore{}
	a := augment.NewAuditAnchor(store, nil)
	if a == nil {
		t.Fatal("nil anchor")
	}
	_, err := a.Emit(context.Background(), augment.EventAugmentationStarted, nil, "p")
	if err != nil {
		t.Fatalf("Emit with default Clock: %v", err)
	}
}

const validHexTip = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestAuditAnchor_RecordHashMatchesChainCompute(t *testing.T) {

	clk := fixedClock{t: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)}
	tsSeconds := clk.Now().Unix()

	store := &recordingChainStore{tip: validHexTip}
	a := augment.NewAuditAnchor(store, clk)

	payload := []byte(`{"session_id":"s","project":"internal-platform-x"}`)
	eventType := augment.EventAugmentationStarted

	anchor, err := a.Emit(context.Background(), eventType, payload, "internal-platform-x")
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	parts := strings.Split(anchor, ":")
	if len(parts) != 3 {
		t.Fatalf("anchor format: want 3 parts, got %d in %q", len(parts), anchor)
	}
	gotHash := parts[2]

	wantHash, err := chain.Compute(validHexTip, eventType.String(), payload, tsSeconds)
	if err != nil {
		t.Fatalf("chain.Compute: %v", err)
	}

	if gotHash != wantHash {
		t.Fatalf(
			"recordHash mismatch with chain.Compute:\n  got:  %s\n  want: %s\n  inputs: prev=%q type=%q ts=%d",
			gotHash, wantHash, validHexTip, eventType.String(), tsSeconds,
		)
	}
}

func TestAuditAnchor_GenesisRecordHashMatchesChainCompute(t *testing.T) {
	clk := fixedClock{t: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)}
	tsSeconds := clk.Now().Unix()
	store := &rawEmptyTipStore{}
	a := augment.NewAuditAnchor(store, clk)

	payload := []byte(`{"genesis":true}`)
	eventType := augment.EventAugmentationStarted

	anchor, err := a.Emit(context.Background(), eventType, payload, "p")
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	parts := strings.Split(anchor, ":")
	if len(parts) != 3 {
		t.Fatalf("anchor format: want 3 parts, got %d", len(parts))
	}
	gotHash := parts[2]

	// Genesis input: prevHash == "". chain.Compute MUST accept that and
	// produce a deterministic hash; we reproduce that here.
	wantHash, err := chain.Compute("", eventType.String(), payload, tsSeconds)
	if err != nil {
		t.Fatalf("chain.Compute genesis: %v", err)
	}
	if gotHash != wantHash {
		t.Fatalf(
			"genesis recordHash mismatch:\n  got:  %s\n  want: %s",
			gotHash, wantHash,
		)
	}
}

func TestAuditAnchor_RecordHashAllSevenEventTypes(t *testing.T) {
	clk := fixedClock{t: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)}
	tsSeconds := clk.Now().Unix()
	events := []augment.EventType{
		augment.EventAugmentationStarted,
		augment.EventAugmentationCompleted,
		augment.EventAugmentationTruncated,
		augment.EventAugmentationSkipped,
		augment.EventKGQueryDispatched,
		augment.EventCrossProjectQueryFiltered,
		augment.EventAugmentationOverridden,
	}
	for _, ev := range events {
		t.Run(ev.String(), func(t *testing.T) {

			store := &recordingChainStore{tip: validHexTip}
			a := augment.NewAuditAnchor(store, clk)
			payload := []byte(fmt.Sprintf(`{"t":%q}`, ev.String()))
			anchor, err := a.Emit(context.Background(), ev, payload, "p")
			if err != nil {
				t.Fatalf("Emit: %v", err)
			}
			parts := strings.Split(anchor, ":")
			if len(parts) != 3 {
				t.Fatalf("anchor format: want 3 parts")
			}
			gotHash := parts[2]
			wantHash, err := chain.Compute(validHexTip, ev.String(), payload, tsSeconds)
			if err != nil {
				t.Fatalf("chain.Compute: %v", err)
			}
			if gotHash != wantHash {
				t.Errorf("recordHash mismatch for %s:\n  got:  %s\n  want: %s", ev, gotHash, wantHash)
			}
		})
	}
}
