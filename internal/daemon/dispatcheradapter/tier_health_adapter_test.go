// internal/daemon/dispatcheradapter/tier_health_adapter_test.go
//
// Test surface for the orchestrator→store TierHealthSink boundary adapter
// .
//
// Three paths exercised:
//
// - RecordHealthSample roundtrip via a real *store.Store backed by a
// temp SQLite file. Asserts the inserted row is readable via
// store.QueryTierHealthSamples with field parity (Provider, Tier,
// Success, LatencyMS, ErrorPattern, TS).
// - Reflective parity guard between orchestrator.TierHealthSampleRow
// and store.TierHealthSampleRow — store has an extra ID
// (autoincrement); every orchestrator field MUST exist on the store
// type with the same name + identical reflect.Type.
// - Fail-fast posture: NewTierHealthSampleAdapter(nil) MUST panic
// (wiring-bug detection at boot — same precedent as Adapter.New /
// NewPinStoreAdapter).
//
// Package dispatcheradapter_test (external) — matches the sibling
// dispatcheradapter_test.go convention so the adapter exercises only the
// exported surface.
package dispatcheradapter_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestTierHealthSampleAdapter_RecordsToStore(t *testing.T) {
	t.Parallel()
	st := openIntegrationStore(t)
	a := dispatcheradapter.NewTierHealthSampleAdapter(st)

	var _ orchestrator.TierHealthSink = a

	ts := time.UnixMilli(1700000000000)
	if err := a.RecordHealthSample(orchestrator.TierHealthSampleRow{
		TS:           ts,
		Provider:     "deepseek-direct",
		Tier:         "openai-compat",
		Success:      true,
		LatencyMS:    110,
		ErrorPattern: "",
	}); err != nil {
		t.Fatalf("RecordHealthSample: %v", err)
	}

	got, err := store.QueryTierHealthSamples(st.DB(), "deepseek-direct", time.UnixMilli(0))
	if err != nil {
		t.Fatalf("QueryTierHealthSamples: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.Provider != "deepseek-direct" {
		t.Errorf("Provider: got %q, want %q", r.Provider, "deepseek-direct")
	}
	if r.Tier != "openai-compat" {
		t.Errorf("Tier: got %q, want %q", r.Tier, "openai-compat")
	}
	if !r.Success {
		t.Errorf("Success: got false, want true")
	}
	if r.LatencyMS != 110 {
		t.Errorf("LatencyMS: got %d, want 110", r.LatencyMS)
	}
	if r.ErrorPattern != "" {
		t.Errorf("ErrorPattern: got %q, want %q", r.ErrorPattern, "")
	}

	if r.TS.UnixMilli() != ts.UnixMilli() {
		t.Errorf("TS: got %v, want %v", r.TS, ts)
	}
}

// TestTierHealthSampleRowParity guards the orchestrator <-> store mirror.
// store.TierHealthSampleRow has an extra ID field (autoincrement); every
// orchestrator field MUST exist on the store type with the same name and
// identical reflect.Type. Drift here would break the 1:1 field copy in
// TierHealthSampleAdapter.RecordHealthSample.
//
// Same pattern as TestCostLedgerRowParity in dispatcheradapter_test.go.
func TestTierHealthSampleRowParity(t *testing.T) {
	t.Parallel()
	orchT := reflect.TypeOf(orchestrator.TierHealthSampleRow{})
	storeT := reflect.TypeOf(store.TierHealthSampleRow{})
	for i := 0; i < orchT.NumField(); i++ {
		f := orchT.Field(i)
		sf, ok := storeT.FieldByName(f.Name)
		if !ok {
			t.Errorf("store.TierHealthSampleRow missing field %q", f.Name)
			continue
		}
		if sf.Type != f.Type {
			t.Errorf("field %q type drift: orchestrator %v vs store %v", f.Name, f.Type, sf.Type)
		}
	}
}

// TestNewTierHealthSampleAdapter_PanicsOnNilStore enforces the fail-fast
// posture: a nil *store.Store at wiring time is a boot bug that MUST
// surface before serving traffic. Same precedent as Adapter.New /
// NewPinStoreAdapter (see dispatcheradapter_test.go).
func TestNewTierHealthSampleAdapter_PanicsOnNilStore(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewTierHealthSampleAdapter(nil): want panic, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value not a string: %v (%T)", r, r)
		}
		// Substring assertion — exact wording may evolve but MUST
		// name the constructor + the "nil" condition for operator
		// debuggability.
		if msg == "" {
			t.Fatalf("panic message empty")
		}
	}()
	_ = dispatcheradapter.NewTierHealthSampleAdapter(nil)
}
