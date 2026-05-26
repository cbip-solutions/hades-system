package zenday_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

func TestGenerator_GenerateMorningBrief_DispatchesToFreeFunction(t *testing.T) {
	tmp := t.TempDir()
	morningDeps, _ := newMorningDeps(t, tmp, canonicalNow)

	gen := zenday.NewGenerator(zenday.GeneratorDeps{Morning: morningDeps})

	doc, err := gen.GenerateMorningBrief(context.Background(), false)
	if err != nil {
		t.Fatalf("Generator.GenerateMorningBrief err = %v", err)
	}
	if doc.Type != zenday.BriefTypeMorning {
		t.Errorf("doc.Type = %v, want BriefTypeMorning", doc.Type)
	}
	wantPath := filepath.Join(tmp, "zen-day-2026-05-01.md")

	if _, err := zenday.GenerateMorningBrief(context.Background(), morningDeps, true); err != nil {
		t.Fatalf("free GenerateMorningBrief err = %v", err)
	}
	_ = wantPath
}

func TestGenerator_GenerateEODDigest_DispatchesToFreeFunction(t *testing.T) {
	tmp := t.TempDir()
	eodDeps, _ := newEODDeps(t, tmp, canonicalNow)

	gen := zenday.NewGenerator(zenday.GeneratorDeps{EOD: eodDeps})

	doc, err := gen.GenerateEODDigest(context.Background(), false)
	if err != nil {
		t.Fatalf("Generator.GenerateEODDigest err = %v", err)
	}
	if doc.Type != zenday.BriefTypeEOD {
		t.Errorf("doc.Type = %v, want BriefTypeEOD", doc.Type)
	}
}

func TestGenerator_CheckPending_DispatchesToFreeFunction(t *testing.T) {
	checkDeps := newCheckPendingDeps(canonicalNow)
	checkDeps.Inbox = &fakeInboxStore{
		rows: []zenday.InboxCacheRow{
			{NotificationID: 1, Severity: inbox.SeverityUrgent, CreatedAt: canonicalNow},
			{NotificationID: 2, Severity: inbox.SeverityActionNeeded, CreatedAt: canonicalNow},
		},
	}

	gen := zenday.NewGenerator(zenday.GeneratorDeps{CheckPending: checkDeps})

	doc, err := gen.CheckPending(context.Background())
	if err != nil {
		t.Fatalf("Generator.CheckPending err = %v", err)
	}
	if doc.Type != zenday.BriefTypeCheckPending {
		t.Errorf("doc.Type = %v, want BriefTypeCheckPending", doc.Type)
	}
	if doc.PendingUrgent != 1 {
		t.Errorf("doc.PendingUrgent = %d, want 1", doc.PendingUrgent)
	}
	if doc.PendingActionNeeded != 1 {
		t.Errorf("doc.PendingActionNeeded = %d, want 1", doc.PendingActionNeeded)
	}
	if !doc.NextScheduledAt.Equal(canonicalNext) {
		t.Errorf("doc.NextScheduledAt = %v, want %v", doc.NextScheduledAt, canonicalNext)
	}
}

func TestGenerator_MorningForceForwarded(t *testing.T) {
	tmp := t.TempDir()
	morningDeps, _ := newMorningDeps(t, tmp, canonicalNow)
	gen := zenday.NewGenerator(zenday.GeneratorDeps{Morning: morningDeps})

	if _, err := gen.GenerateMorningBrief(context.Background(), false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := gen.GenerateMorningBrief(context.Background(), false)
	if !errors.Is(err, zenday.ErrAlreadyGenerated) {
		t.Errorf("second call err = %v, want ErrAlreadyGenerated", err)
	}
	if _, err := gen.GenerateMorningBrief(context.Background(), true); err != nil {
		t.Errorf("force re-run failed: %v", err)
	}
}

func TestGenerator_EODForceForwarded(t *testing.T) {
	tmp := t.TempDir()
	eodDeps, _ := newEODDeps(t, tmp, canonicalNow)
	gen := zenday.NewGenerator(zenday.GeneratorDeps{EOD: eodDeps})

	if _, err := gen.GenerateEODDigest(context.Background(), false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := gen.GenerateEODDigest(context.Background(), false)
	if !errors.Is(err, zenday.ErrAlreadyGenerated) {
		t.Errorf("second call err = %v, want ErrAlreadyGenerated", err)
	}
	if _, err := gen.GenerateEODDigest(context.Background(), true); err != nil {
		t.Errorf("force re-run failed: %v", err)
	}
}

func TestGenerator_CheckPendingErrorPropagates(t *testing.T) {
	checkDeps := newCheckPendingDeps(canonicalNow)
	checkDeps.Inbox = &fakeInboxStore{err: errors.New("inbox down")}

	gen := zenday.NewGenerator(zenday.GeneratorDeps{CheckPending: checkDeps})

	_, err := gen.CheckPending(context.Background())
	if err == nil {
		t.Fatal("expected inbox error; got nil")
	}
}

func TestNewGenerator_NilSafe(t *testing.T) {
	gen := zenday.NewGenerator(zenday.GeneratorDeps{})
	if gen == nil {
		t.Fatal("NewGenerator returned nil; want non-nil *Generator even with empty deps")
	}
}

func TestGenerator_AllThreeMethodsCallableInSequence(t *testing.T) {
	tmp := t.TempDir()
	morningDeps, _ := newMorningDeps(t, tmp, canonicalNow)
	eodDeps, _ := newEODDeps(t, tmp, canonicalNow)
	checkDeps := newCheckPendingDeps(canonicalNow)

	gen := zenday.NewGenerator(zenday.GeneratorDeps{
		Morning:      morningDeps,
		EOD:          eodDeps,
		CheckPending: checkDeps,
	})

	morning, err := gen.GenerateMorningBrief(context.Background(), false)
	if err != nil {
		t.Fatalf("morning err = %v", err)
	}
	if morning.Type != zenday.BriefTypeMorning {
		t.Errorf("morning.Type = %v", morning.Type)
	}

	eod, err := gen.GenerateEODDigest(context.Background(), false)
	if err != nil {
		t.Fatalf("eod err = %v", err)
	}
	if eod.Type != zenday.BriefTypeEOD {
		t.Errorf("eod.Type = %v", eod.Type)
	}

	check, err := gen.CheckPending(context.Background())
	if err != nil {
		t.Fatalf("check err = %v", err)
	}
	if check.Type != zenday.BriefTypeCheckPending {
		t.Errorf("check.Type = %v", check.Type)
	}
}
