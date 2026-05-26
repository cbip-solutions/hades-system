package inbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mkCollapseEvent(project string, sv Severity, ev string, at time.Time) Notification {
	return Notification{
		ProjectID:   project,
		Severity:    sv,
		EventType:   ev,
		ContentHash: ComputeContentHash(map[string]any{"k": project + ev}),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   at,
	}
}

func TestDefaultCollapseRule(t *testing.T) {
	r := DefaultCollapseRule("provider.error")
	if r.EventType != "provider.error" {
		t.Errorf("EventType = %q", r.EventType)
	}
	if r.Window != 60*time.Second {
		t.Errorf("Window = %v, want 60s", r.Window)
	}
	if r.MinProjects != 3 {
		t.Errorf("MinProjects = %d, want 3", r.MinProjects)
	}
}

func TestDetectCollapseBelowThreshold(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now),
		mkCollapseEvent(pidB, SeverityUrgent, "provider.error", now.Add(10*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")
	got, ok := DetectCollapse(events, rule, now.Add(20*time.Second))
	if ok {
		t.Errorf("collapse fired below threshold: got %+v", got)
	}
}

func TestDetectCollapseAtThreshold(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now),
		mkCollapseEvent(pidB, SeverityUrgent, "provider.error", now.Add(10*time.Second)),
		mkCollapseEvent(pidC, SeverityUrgent, "provider.error", now.Add(30*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")
	got, ok := DetectCollapse(events, rule, now.Add(40*time.Second))
	if !ok {
		t.Fatal("collapse should fire at MinProjects=3")
	}
	if len(got.ProjectIDs) != 3 {
		t.Errorf("ProjectIDs len = %d, want 3", len(got.ProjectIDs))
	}
	if got.EventType != "provider.error" {
		t.Errorf("EventType = %q", got.EventType)
	}
	if got.Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent", got.Severity)
	}
	if !strings.Contains(got.Message, "3 projects") {
		t.Errorf("Message %q does not mention 3 projects", got.Message)
	}
}

func TestDetectCollapsePicksMaxSeverity(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityInfoDigest, "provider.error", now),
		mkCollapseEvent(pidB, SeverityActionNeeded, "provider.error", now.Add(10*time.Second)),
		mkCollapseEvent(pidC, SeverityUrgent, "provider.error", now.Add(20*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")
	got, ok := DetectCollapse(events, rule, now.Add(30*time.Second))
	if !ok {
		t.Fatal("expected collapse")
	}
	if got.Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent (max)", got.Severity)
	}
}

func TestDetectCollapseHonorsWindow(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now),
		mkCollapseEvent(pidB, SeverityUrgent, "provider.error", now.Add(45*time.Second)),
		mkCollapseEvent(pidC, SeverityUrgent, "provider.error", now.Add(90*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")

	got, ok := DetectCollapse(events, rule, now.Add(90*time.Second))
	if ok {
		t.Errorf("should NOT collapse: only 2 in window. got %+v", got)
	}
}

func TestDetectCollapseFiltersByEventType(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now),
		mkCollapseEvent(pidB, SeverityUrgent, "different.event", now.Add(10*time.Second)),
		mkCollapseEvent(pidC, SeverityUrgent, "provider.error", now.Add(20*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")
	_, ok := DetectCollapse(events, rule, now.Add(30*time.Second))
	if ok {
		t.Error("collapse should NOT fire — only 2 of 3 events match EventType")
	}
}

func TestDetectCollapseDeduplicatesProjects(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now),
		mkCollapseEvent(pidA, SeverityUrgent, "provider.error", now.Add(10*time.Second)),
		mkCollapseEvent(pidB, SeverityUrgent, "provider.error", now.Add(20*time.Second)),
	}
	rule := DefaultCollapseRule("provider.error")
	_, ok := DetectCollapse(events, rule, now.Add(30*time.Second))
	if ok {
		t.Error("collapse should NOT fire — only 2 distinct projects")
	}
}

func TestDetectCollapseProjectIDsSorted(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidC, SeverityUrgent, "x.y", now),
		mkCollapseEvent(pidA, SeverityUrgent, "x.y", now.Add(time.Second)),
		mkCollapseEvent(pidB, SeverityUrgent, "x.y", now.Add(2*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	got, ok := DetectCollapse(events, rule, now.Add(10*time.Second))
	if !ok {
		t.Fatal("expected collapse")
	}
	for i := 1; i < len(got.ProjectIDs); i++ {
		if got.ProjectIDs[i-1] >= got.ProjectIDs[i] {
			t.Errorf("ProjectIDs not sorted ascending: %v", got.ProjectIDs)
		}
	}
}

func TestDetectCollapseSameProjectMaxSeverity(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityInfoDigest, "x.y", now),
		mkCollapseEvent(pidA, SeverityUrgent, "x.y", now.Add(5*time.Second)),
		mkCollapseEvent(pidB, SeverityInfoDigest, "x.y", now.Add(10*time.Second)),
		mkCollapseEvent(pidC, SeverityInfoDigest, "x.y", now.Add(15*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	got, ok := DetectCollapse(events, rule, now.Add(20*time.Second))
	if !ok {
		t.Fatal("expected collapse with 3 distinct projects")
	}
	if got.Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent (max across all events)", got.Severity)
	}
}

func TestDetectCollapseSameProjectKeepsMaxOnSecondLowerEvent(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "x.y", now),
		mkCollapseEvent(pidA, SeverityInfoDigest, "x.y", now.Add(5*time.Second)),
		mkCollapseEvent(pidB, SeverityInfoDigest, "x.y", now.Add(10*time.Second)),
		mkCollapseEvent(pidC, SeverityInfoDigest, "x.y", now.Add(15*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	got, ok := DetectCollapse(events, rule, now.Add(20*time.Second))
	if !ok {
		t.Fatal("expected collapse with 3 distinct projects")
	}

	if got.Severity != SeverityUrgent {
		t.Errorf("Severity = %q, want urgent (downgrade from urgent must be rejected)", got.Severity)
	}
}

func TestDetectCollapseRejectsFutureEvents(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityUrgent, "x.y", now),
		mkCollapseEvent(pidB, SeverityUrgent, "x.y", now.Add(5*time.Second)),

		mkCollapseEvent(pidC, SeverityUrgent, "x.y", now.Add(60*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	_, ok := DetectCollapse(events, rule, now.Add(10*time.Second))
	if ok {
		t.Error("collapse must NOT fire — pidC event is future-dated relative to eval time")
	}
}

func TestDetectCollapseInfoImmediateRanks(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, SeverityInfoDigest, "x.y", now),
		mkCollapseEvent(pidB, SeverityInfoImmediate, "x.y", now.Add(5*time.Second)),
		mkCollapseEvent(pidC, SeverityInfoDigest, "x.y", now.Add(10*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	got, ok := DetectCollapse(events, rule, now.Add(15*time.Second))
	if !ok {
		t.Fatal("expected collapse with 3 distinct projects")
	}
	if got.Severity != SeverityInfoImmediate {
		t.Errorf("Severity = %q, want info-immediate (rank 2 > rank 1)", got.Severity)
	}
}

func TestDetectCollapseUnknownSeverityRanksLowest(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)
	pidC := "c" + strings.Repeat("2", 63)

	events := []Notification{
		mkCollapseEvent(pidA, Severity("garbage"), "x.y", now),
		mkCollapseEvent(pidB, SeverityInfoDigest, "x.y", now.Add(5*time.Second)),
		mkCollapseEvent(pidC, SeverityInfoDigest, "x.y", now.Add(10*time.Second)),
	}
	rule := DefaultCollapseRule("x.y")
	got, ok := DetectCollapse(events, rule, now.Add(15*time.Second))
	if !ok {
		t.Fatal("expected collapse with 3 distinct projects")
	}
	if got.Severity != SeverityInfoDigest {
		t.Errorf("Severity = %q, want info-digest (known wins over unknown)", got.Severity)
	}
}
