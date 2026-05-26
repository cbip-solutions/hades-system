package amendment_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func writeRangeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNextAvailableIDEmptyReturnsLowest(t *testing.T) {
	dir := t.TempDir()
	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != amendment.Plan5MinADR {
		t.Errorf("want %d, got %d", amendment.Plan5MinADR, id)
	}
}

func TestNextAvailableIDSkipsTakenAcrossThreeDirs(t *testing.T) {
	dir := t.TempDir()
	writeRangeFiles(t, dir, "0020-already.md")
	writeRangeFiles(t, filepath.Join(dir, "proposed"), "0021-pend.md")
	writeRangeFiles(t, filepath.Join(dir, "rejected"), "0022-rej.md")
	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != 23 {
		t.Errorf("want 23, got %d", id)
	}
}

func TestNextAvailableIDExhaustedReturnsErr(t *testing.T) {
	dir := t.TempDir()
	for i := amendment.Plan5MinADR; i <= amendment.Plan5MaxADR; i++ {
		writeRangeFiles(t, dir, fmt.Sprintf("%04d-x.md", i))
	}
	_, err := amendment.NextAvailableID(dir)
	if !errors.Is(err, amendment.ErrADRRangeExhausted) {
		t.Errorf("want ErrADRRangeExhausted, got %v", err)
	}
}

func TestNextAvailableIDIgnoresOutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeRangeFiles(t, dir, "0001-existing.md", "0008-existing.md", "0030-future.md", "0099-future.md")
	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != 20 {
		t.Errorf("want 20, got %d", id)
	}
}

func TestNextAvailableIDIgnoresNonMDOrSubdirs(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "0020-subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeRangeFiles(t, dir, "0021-x.txt")

	writeRangeFiles(t, dir, "020-x.md")

	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != 20 {
		t.Errorf("want 20 (subdirs and non-md ignored), got %d", id)
	}
}

func TestNextAvailableIDMissingSubdirsTolerated(t *testing.T) {
	dir := t.TempDir()
	id, err := amendment.NextAvailableID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != 20 {
		t.Errorf("want 20, got %d", id)
	}
}

func TestNextAvailableIDReadDirError(t *testing.T) {
	dir := t.TempDir()

	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := amendment.NextAvailableID(dir)
	if err == nil {
		t.Skip("filesystem allowed read on chmod 000 (root or permissive)")
	}
	if !errors.Is(err, os.ErrPermission) && err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Logf("got error (acceptable): %v", err)
	}
}

type rangeFakeEmitter struct {
	mu     sync.Mutex
	events []eventlog.Event
}

func (r *rangeFakeEmitter) Append(_ context.Context, ev eventlog.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func TestRangeAllocatorRealEmitsOnExhaustion(t *testing.T) {
	dir := t.TempDir()
	for i := amendment.Plan5MinADR; i <= amendment.Plan5MaxADR; i++ {
		writeRangeFiles(t, dir, fmt.Sprintf("%04d-x.md", i))
	}
	em := &rangeFakeEmitter{}
	r := &amendment.RangeAllocatorReal{Emitter: em}
	_, err := r.NextAvailableID(context.Background(), dir)
	if !errors.Is(err, amendment.ErrADRRangeExhausted) {
		t.Errorf("want ErrADRRangeExhausted, got %v", err)
	}
	if len(em.events) != 1 || em.events[0].Type != eventlog.EvtADRRangeExhausted {
		t.Fatalf("want 1 ADRRangeExhausted event, got %+v", em.events)
	}
	if em.events[0].Payload["min"] != amendment.Plan5MinADR {
		t.Errorf("expected min in payload, got %+v", em.events[0].Payload)
	}
}

func TestRangeAllocatorRealNoEmitOnSuccess(t *testing.T) {
	dir := t.TempDir()
	em := &rangeFakeEmitter{}
	r := &amendment.RangeAllocatorReal{Emitter: em}
	id, err := r.NextAvailableID(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != 20 {
		t.Errorf("want 20, got %d", id)
	}
	if len(em.events) != 0 {
		t.Errorf("must not emit on success, got %+v", em.events)
	}
}

func TestRangeAllocatorRealNilEmitter(t *testing.T) {
	dir := t.TempDir()
	for i := amendment.Plan5MinADR; i <= amendment.Plan5MaxADR; i++ {
		writeRangeFiles(t, dir, fmt.Sprintf("%04d-x.md", i))
	}
	r := &amendment.RangeAllocatorReal{}
	_, err := r.NextAvailableID(context.Background(), dir)
	if !errors.Is(err, amendment.ErrADRRangeExhausted) {
		t.Errorf("want ErrADRRangeExhausted, got %v", err)
	}
}

func TestRangeAllocatorRealNonExhaustionErrorPassedThrough(t *testing.T) {

	dir := t.TempDir()
	bad := filepath.Join(dir, "broken-dir")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })
	em := &rangeFakeEmitter{}
	r := &amendment.RangeAllocatorReal{Emitter: em}
	_, err := r.NextAvailableID(context.Background(), bad)
	if err == nil {
		t.Skip("filesystem allowed read on chmod 000")
	}

	for _, ev := range em.events {
		if ev.Type == eventlog.EvtADRRangeExhausted {
			t.Errorf("must not emit exhaustion on non-exhaustion error: %+v", ev)
		}
	}
}
