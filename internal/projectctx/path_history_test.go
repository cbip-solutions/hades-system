package projectctx

import (
	"testing"
	"time"
)

func mkID(prefix string) ProjectID {

	hex := "abcdef0123456789"
	out := prefix
	for len(out) < 64 {
		out += string(hex[len(out)%len(hex)])
	}
	return ProjectID(out[:64])
}

func TestDetectMvNoHistoryReturnsNil(t *testing.T) {
	got := DetectMv(Alias("test"), "/path/new", mkID("a"), nil)
	if got != nil {
		t.Errorf("expected nil for empty history, got %+v", got)
	}
}

func TestDetectMvCurrentMatchesHistoryReturnsNil(t *testing.T) {
	currentID := mkID("a")
	history := []PathHistoryEntry{
		{
			ProjectID:   currentID,
			Path:        "/path/known",
			FirstSeenAt: time.Now().Add(-1 * time.Hour),
			LastSeenAt:  time.Now().Add(-1 * time.Hour),
		},
	}
	got := DetectMv(Alias("test"), "/path/known", currentID, history)
	if got != nil {
		t.Errorf("expected nil when current matches history, got %+v", got)
	}
}

func TestDetectMvDifferentIDReturnsMvDetection(t *testing.T) {
	oldID := mkID("a")
	newID := mkID("b")
	history := []PathHistoryEntry{
		{
			ProjectID:   oldID,
			Path:        "/old/path",
			FirstSeenAt: time.Now().Add(-2 * time.Hour),
			LastSeenAt:  time.Now().Add(-1 * time.Hour),
		},
	}
	got := DetectMv(Alias("test"), "/new/path", newID, history)
	if got == nil {
		t.Fatal("expected MvDetection for different id, got nil")
	}
	if got.Alias != Alias("test") {
		t.Errorf("Alias = %q, want test", got.Alias)
	}
	if got.OldPath != "/old/path" {
		t.Errorf("OldPath = %q, want /old/path", got.OldPath)
	}
	if got.NewPath != "/new/path" {
		t.Errorf("NewPath = %q, want /new/path", got.NewPath)
	}
	if got.OldID != oldID {
		t.Errorf("OldID = %s, want %s", got.OldID, oldID)
	}
	if got.NewID != newID {
		t.Errorf("NewID = %s, want %s", got.NewID, newID)
	}
}

func TestDetectMvUsesMostRecentEntry(t *testing.T) {
	id1 := mkID("1")
	id2 := mkID("2")
	id3 := mkID("3")
	now := time.Now()
	history := []PathHistoryEntry{
		{ProjectID: id1, Path: "/old/1", FirstSeenAt: now.Add(-3 * time.Hour), LastSeenAt: now.Add(-3 * time.Hour)},
		{ProjectID: id2, Path: "/old/2", FirstSeenAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-1 * time.Hour)},
		{ProjectID: id3, Path: "/old/3", FirstSeenAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour)},
	}
	newID := mkID("4")
	got := DetectMv(Alias("test"), "/new", newID, history)
	if got == nil {
		t.Fatal("expected MvDetection")
	}
	if got.OldPath != "/old/2" {
		t.Errorf("OldPath = %q, want /old/2 (most recent)", got.OldPath)
	}
	if got.OldID != id2 {
		t.Errorf("OldID = %s, want %s", got.OldID, id2)
	}
}

func TestDetectMvCyclicMoveBackReturnsNil(t *testing.T) {

	idA := mkID("a")
	idB := mkID("b")
	now := time.Now()
	history := []PathHistoryEntry{
		{ProjectID: idA, Path: "/a", FirstSeenAt: now.Add(-3 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour)},
		{ProjectID: idB, Path: "/b", FirstSeenAt: now.Add(-1 * time.Hour), LastSeenAt: now.Add(-1 * time.Hour)},
	}
	got := DetectMv(Alias("test"), "/a", idA, history)
	if got != nil {
		t.Errorf("expected nil on cyclic move-back, got %+v", got)
	}
}

func TestPathHistoryEntryRoundTripFields(t *testing.T) {
	now := time.Now()
	entry := PathHistoryEntry{
		ProjectID:   mkID("z"),
		Path:        "/test",
		FirstSeenAt: now,
		LastSeenAt:  now.Add(1 * time.Hour),
	}
	if entry.ProjectID.String() == "" {
		t.Error("ProjectID empty after assignment")
	}
	if entry.FirstSeenAt.After(entry.LastSeenAt) {
		t.Error("FirstSeenAt > LastSeenAt")
	}
}

func TestMvDetectionString(t *testing.T) {
	d := &MvDetection{
		Alias:   Alias("test"),
		OldPath: "/old",
		NewPath: "/new",
		OldID:   mkID("a"),
		NewID:   mkID("b"),
	}
	got := d.String()
	if got == "" {
		t.Error("String() returned empty")
	}
	for _, want := range []string{"test", "/old", "/new"} {
		if !stringContains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}

func TestMvDetectionStringStableFormat(t *testing.T) {
	d := &MvDetection{
		Alias:   Alias("internal-platform-x"),
		OldPath: "/old/canonical",
		NewPath: "/new/canonical",
		OldID:   mkID("a"),
		NewID:   mkID("b"),
	}
	got := d.String()
	wantPrefix := "mv-detected: alias=internal-platform-x old=/old/canonical:"
	if !stringContains(got, wantPrefix) {
		t.Errorf("String() = %q, missing prefix %q", got, wantPrefix)
	}
	wantNew := " new=/new/canonical:" + d.NewID.Short()
	if !stringContains(got, wantNew) {
		t.Errorf("String() = %q, missing %q", got, wantNew)
	}
	wantOld := " old=/old/canonical:" + d.OldID.Short()
	if !stringContains(got, wantOld) {
		t.Errorf("String() = %q, missing %q", got, wantOld)
	}
}

func stringContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
