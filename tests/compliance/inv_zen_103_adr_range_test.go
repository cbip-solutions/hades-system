// tests/compliance/inv_zen_103_adr_range_test.go
//
// Compliance test for inv-zen-103 (Plan 5 Phase K-7):
//
//	amendment.NextAvailableID enforces the Plan 5 reserved ADR range
//	[0020, 0029]. When all 10 slots are consumed (across docs/decisions/,
//	docs/decisions/proposed/, docs/decisions/rejected/) the function
//	returns ErrADRRangeExhausted; out-of-range files (base ADRs 0001+,
//	future plan ranges 0030+) MUST NOT block Plan 5 allocation.
package compliance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestInvZen103ADRRangeExhaustion(t *testing.T) {
	dir := t.TempDir()
	for i := amendment.Plan5MinADR; i <= amendment.Plan5MaxADR; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d-x.md", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := amendment.NextAvailableID(dir)
	if !errors.Is(err, amendment.ErrADRRangeExhausted) {
		t.Fatalf("inv-zen-103: want ErrADRRangeExhausted, got %v", err)
	}
}

func TestInvZen103ADRRangeIgnoresOutOfRange(t *testing.T) {
	dir := t.TempDir()
	// Future plan ranges + base ADRs MUST NOT block Plan 5 allocation.
	if err := os.WriteFile(filepath.Join(dir, "0001-base.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0008-base.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0030-plan6.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != amendment.Plan5MinADR {
		t.Errorf("inv-zen-103: want %d (out-of-range files ignored), got %d", amendment.Plan5MinADR, id)
	}
}

func TestInvZen103ADRRangeAcrossThreeDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proposed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rejected"), 0o755); err != nil {
		t.Fatal(err)
	}

	for i, sub := range []string{".", "proposed", "rejected"} {
		idx := amendment.Plan5MinADR + i*3
		for j := 0; j < 3; j++ {
			if err := os.WriteFile(
				filepath.Join(dir, sub, fmt.Sprintf("%04d-x.md", idx+j)),
				[]byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatalf("inv-zen-103: %v", err)
	}
	if id != 29 {
		t.Errorf("inv-zen-103: want 29 (only free slot), got %d", id)
	}
}

type captureEmitter103 struct {
	events []eventlog.Event
}

func (c *captureEmitter103) Append(_ context.Context, ev eventlog.Event) error {
	c.events = append(c.events, ev)
	return nil
}

func TestInvZen103ADRRangeExhaustedEventEmitted(t *testing.T) {
	dir := t.TempDir()
	for i := amendment.Plan5MinADR; i <= amendment.Plan5MaxADR; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d-x.md", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	em := &captureEmitter103{}
	r := &amendment.RangeAllocatorReal{Emitter: em}
	_, err := r.NextAvailableID(context.Background(), dir)
	if !errors.Is(err, amendment.ErrADRRangeExhausted) {
		t.Fatalf("want ErrADRRangeExhausted, got %v", err)
	}
	found := false
	for _, ev := range em.events {
		if ev.Type == eventlog.EvtADRRangeExhausted {
			found = true
			if ev.Payload["min"] != amendment.Plan5MinADR || ev.Payload["max"] != amendment.Plan5MaxADR {
				t.Errorf("event payload missing min/max: %+v", ev.Payload)
			}
		}
	}
	if !found {
		t.Errorf("inv-zen-103: ADRRangeExhausted audit event not emitted: %+v", em.events)
	}
}
