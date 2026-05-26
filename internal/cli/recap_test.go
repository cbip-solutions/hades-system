package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func writeBrief(t *testing.T, dir, base, body string) string {
	t.Helper()
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", p, err)
	}
	return p
}

func TestParseRecapSince_Stdlib(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"90m", 90 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"45s", 45 * time.Second},
	}
	for _, c := range cases {
		got, err := parseRecapSince(c.in)
		if err != nil {
			t.Errorf("parseRecapSince(%q) err=%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseRecapSince(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseRecapSince_DaysWeeks(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
	}
	for _, c := range cases {
		got, err := parseRecapSince(c.in)
		if err != nil {
			t.Errorf("parseRecapSince(%q) err=%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseRecapSince(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseRecapSince_Errors(t *testing.T) {
	bad := []string{
		"",
		"abc",
		"7days",
		"-1d",
		"+1d",
		"d",
		"w",
		"7",
		"7y",
		"7.5d",
		"7d extra",
		"0d",
		"0w",
		"0s",
		"0h",
	}
	for _, in := range bad {
		if _, err := parseRecapSince(in); err == nil {
			t.Errorf("parseRecapSince(%q) expected error, got nil", in)
		}
	}
}

func TestRunRecap_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := runRecap(dir, 24*time.Hour, fixedClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

func TestRunRecap_NonexistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var buf bytes.Buffer

	if err := runRecap(dir, 24*time.Hour, fixedClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

func TestRunRecap_SingleBrief(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2026-05-01.md", "# Brief 2026-05-01\nbody-1\n")

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "# Brief 2026-05-01") {
		t.Errorf("missing brief content: %q", got)
	}
	if strings.Contains(got, "---") {
		t.Errorf("single brief should not emit separator: %q", got)
	}
}

func TestRunRecap_MultiBrief_ChronoSort(t *testing.T) {
	dir := t.TempDir()

	writeBrief(t, dir, "zen-day-2026-05-03.md", "third\n")
	writeBrief(t, dir, "zen-day-2026-05-01.md", "first\n")
	writeBrief(t, dir, "zen-day-2026-05-02.md", "second\n")

	var buf bytes.Buffer

	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	if err := runRecap(dir, 7*24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}

	got := buf.String()
	idxFirst := strings.Index(got, "first")
	idxSecond := strings.Index(got, "second")
	idxThird := strings.Index(got, "third")
	if idxFirst < 0 || idxSecond < 0 || idxThird < 0 {
		t.Fatalf("missing entries in output: %q", got)
	}
	if !(idxFirst < idxSecond && idxSecond < idxThird) {
		t.Errorf("chronological order broken: first@%d second@%d third@%d\n%s",
			idxFirst, idxSecond, idxThird, got)
	}

	if n := strings.Count(got, "---"); n != 2 {
		t.Errorf("separator count = %d, want 2: %q", n, got)
	}

	if strings.HasSuffix(got, "---\n") {
		t.Errorf("trailing separator present: %q", got)
	}
}

func TestRunRecap_OutOfWindowExcluded(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2026-04-20.md", "ancient\n")
	writeBrief(t, dir, "zen-day-2026-05-01.md", "recent\n")

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	if err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "recent") {
		t.Errorf("missing 'recent': %q", got)
	}
	if strings.Contains(got, "ancient") {
		t.Errorf("'ancient' should be filtered: %q", got)
	}
}

func TestRunRecap_AllFilesOutOfWindow(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2025-01-01.md", "year-old\n")
	writeBrief(t, dir, "zen-day-2025-02-01.md", "another\n")

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	if err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

type errAfterNWriter struct {
	max     int
	written int
	err     error
}

func (w *errAfterNWriter) Write(p []byte) (int, error) {
	if w.written >= w.max {
		return 0, w.err
	}
	avail := w.max - w.written
	if avail >= len(p) {
		w.written += len(p)
		return len(p), nil
	}

	w.written += avail
	return avail, w.err
}

func TestRunRecap_WriteBodyError(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2026-05-01.md", "body-content\n")

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	w := &errAfterNWriter{max: 0, err: errors.New("disk full")}
	err := runRecap(dir, 24*time.Hour, fixedClock(now), w)
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("expected wrapped write error, got %v", err)
	}
}

func TestRunRecap_WriteSeparatorError(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2026-05-01.md", "first\n")
	writeBrief(t, dir, "zen-day-2026-05-02.md", "second\n")

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)

	w := &errAfterNWriter{max: 6, err: errors.New("pipe broken")}
	err := runRecap(dir, 7*24*time.Hour, fixedClock(now), w)
	if err == nil {
		t.Fatal("expected write separator error, got nil")
	}
	if !strings.Contains(err.Error(), "separator") {
		t.Errorf("expected wrapped separator error, got %v", err)
	}
}

func TestRunRecap_EODAndMorningBoth(t *testing.T) {
	dir := t.TempDir()
	writeBrief(t, dir, "zen-day-2026-05-01.md", "morning-content\n")
	writeBrief(t, dir, "zen-day-2026-05-01-eod.md", "eod-content\n")

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 23, 0, 0, 0, time.UTC)
	if err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "morning-content") {
		t.Errorf("missing morning: %q", got)
	}
	if !strings.Contains(got, "eod-content") {
		t.Errorf("missing eod: %q", got)
	}

	idxM := strings.Index(got, "morning-content")
	idxE := strings.Index(got, "eod-content")
	if !(idxM < idxE) {
		t.Errorf("expected morning before eod when same date; got morning@%d eod@%d", idxM, idxE)
	}

	if n := strings.Count(got, "---"); n != 1 {
		t.Errorf("separator count = %d, want 1: %q", n, got)
	}
}

func TestRunRecap_MalformedFilenamesSkipped(t *testing.T) {
	dir := t.TempDir()

	writeBrief(t, dir, "zen-day-bogus.md", "x\n")
	writeBrief(t, dir, "zen-day-.md", "x\n")
	writeBrief(t, dir, "zen-day-2026-13-99.md", "x\n")
	writeBrief(t, dir, "zen-day-not-a-date.md", "x\n")
	writeBrief(t, dir, "zen-day-2026-05-01-archive.md", "x\n")

	writeBrief(t, dir, "zen-day-2026-05-01.md", "real-body\n")

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf); err != nil {
		t.Fatalf("runRecap: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "real-body") {
		t.Errorf("missing real entry: %q", got)
	}
	if strings.Contains(got, "x\n") {

		t.Errorf("malformed body leaked: %q", got)
	}
}

func TestRunRecap_ReadFileError_Propagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode 0 unsupported on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode 0; skipping")
	}
	dir := t.TempDir()
	p := writeBrief(t, dir, "zen-day-2026-05-01.md", "body\n")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	err := runRecap(dir, 24*time.Hour, fixedClock(now), &buf)
	if err == nil {
		t.Fatal("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("expected wrapped read error, got %v", err)
	}
}

func TestRunRecap_GlobError(t *testing.T) {
	bad := "/tmp/recap-test-[unclosed"
	var buf bytes.Buffer
	err := runRecap(bad, 24*time.Hour, fixedClock(time.Now()), &buf)
	if err == nil {
		t.Fatal("expected glob error, got nil")
	}
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("expected ErrBadPattern, got %v", err)
	}
}

func TestRunRecap_BadSinceParse_ReturnsError(t *testing.T) {

	cmd := NewRecapCmd()
	cmd.SetArgs([]string{"--since", "garbage"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse --since") {
		t.Errorf("expected parse --since wrap, got %v", err)
	}
}

func TestNewRecapCmd_DefaultSince(t *testing.T) {

	tmpHome := t.TempDir()
	archiveDir := filepath.Join(tmpHome, ".config", "zen-swarm")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeBrief(t, archiveDir, "zen-day-"+time.Now().UTC().Format("2006-01-02")+".md", "today's brief\n")

	t.Setenv("HOME", tmpHome)

	cmd := NewRecapCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "today's brief") {
		t.Errorf("missing today's brief in default-since invocation: %q", stdout.String())
	}
}

func TestNewRecapCmd_ExplicitSince(t *testing.T) {
	tmpHome := t.TempDir()
	archiveDir := filepath.Join(tmpHome, ".config", "zen-swarm")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	now := time.Now().UTC()
	recent := now.Add(-2 * 24 * time.Hour).Format("2006-01-02")
	ancient := now.Add(-365 * 24 * time.Hour).Format("2006-01-02")
	writeBrief(t, archiveDir, "zen-day-"+recent+".md", "RECENT\n")
	writeBrief(t, archiveDir, "zen-day-"+ancient+".md", "ANCIENT\n")

	t.Setenv("HOME", tmpHome)

	cmd := NewRecapCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--since", "7d"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "RECENT") {
		t.Errorf("missing RECENT: %q", got)
	}
	if strings.Contains(got, "ANCIENT") {
		t.Errorf("ANCIENT should be filtered: %q", got)
	}
}

func TestNewRecapCmd_HomeDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir consults USERPROFILE on Windows; HOME-clearing is unix-only")
	}
	t.Setenv("HOME", "")
	cmd := NewRecapCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--since", "24h"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected home dir error, got nil")
	}
	if !strings.Contains(err.Error(), "home dir") {
		t.Errorf("expected home dir wrap, got %v", err)
	}
}

func TestNewRecapCmd_RegisteredOnRoot(t *testing.T) {
	root := NewRootCmd()
	for _, sub := range root.Commands() {
		if sub.Use == "recap" {
			return
		}
	}
	t.Fatalf("recap not registered on root; root.Commands() = %d entries", len(root.Commands()))
}
