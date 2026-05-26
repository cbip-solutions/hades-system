// Coverage
//   - ClassifierVersion non-empty (Phase G G-6 contract)
//   - Classify success path
//   - Classify infra patterns (all 9 in infraPatterns)
//   - Classify flake quarantine match
//   - Classify real fallback (failure + no infra + no flake)
//   - Classify pending (treated as real for safety)
//   - Classify with invalid regex in quarantine (skip, fallback real)
//   - LoadFlakeQuarantine empty file
//   - LoadFlakeQuarantine valid 3-token entries
//   - LoadFlakeQuarantine 14d boundary rejection (inclusive)
//   - LoadFlakeQuarantine malformed: 1-token, 2-token, invalid ts
//   - LoadFlakeQuarantine multi-token reason recombines
//   - LoadFlakeQuarantine missing file
//   - LoadFlakeQuarantine invalid Last-review header timestamp
//
// Coverage target ≥90% (security/correctness-critical per CLAUDE.md).
package ci_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

func TestClassifierVersion_NonEmpty(t *testing.T) {
	t.Parallel()
	if ci.ClassifierVersion == "" {
		t.Fatal("ci.ClassifierVersion must be non-empty (Phase G G-6 inv-zen-311 contract)")
	}
}

func TestClassify_SuccessBucketed(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{Status: "success"}
	out := ci.Classify(c, nil)
	if out.Bucket != "success" {
		t.Errorf("Bucket: got %s; want success", out.Bucket)
	}
}

func TestClassify_PendingBucketedReal(t *testing.T) {
	t.Parallel()

	c := ci.CommitStatus{Status: "pending"}
	out := ci.Classify(c, nil)
	if out.Bucket != "real" {
		t.Errorf("pending Bucket: got %s; want real", out.Bucket)
	}
}

func TestClassify_InfraPatterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason string
	}{
		{"gha-billing", "GHA billing block"},
		{"billing-block", "Repository billing block: payment failed"},
		{"runner-exhausted", "runner pool exhausted; try again"},
		{"runner-pool", "self-hosted runner pool unavailable"},
		{"network-timeout", "network timeout to example.com"},
		{"oom-short", "OOM killed"},
		{"out-of-memory", "process killed: out of memory at line 42"},
		{"429-rate-limit", "HTTP 429 rate limit hit"},
		{"503-service-unavailable", "HTTP 503 service unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := ci.CommitStatus{Status: "failure", Reason: tc.reason}
			out := ci.Classify(c, nil)
			if out.Bucket != "infra" {
				t.Errorf("Bucket for %q: got %s; want infra", tc.reason, out.Bucket)
			}
		})
	}
}

func TestClassify_Flake(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{Status: "failure", Reason: "test TestKnownFlake intermittently fails"}
	out := ci.Classify(c, []string{"TestKnownFlake"})
	if out.Bucket != "flake" {
		t.Errorf("Bucket: got %s; want flake", out.Bucket)
	}
}

func TestClassify_FlakeSkipsEmptyEntry(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{Status: "failure", Reason: "TestKnownFlake fails"}
	out := ci.Classify(c, []string{"", "TestKnownFlake", ""})
	if out.Bucket != "flake" {
		t.Errorf("Bucket: got %s; want flake (empty entries ignored)", out.Bucket)
	}
}

func TestClassify_FlakeSkipsInvalidRegex(t *testing.T) {
	t.Parallel()

	c := ci.CommitStatus{Status: "failure", Reason: "legitimate test regression"}
	out := ci.Classify(c, []string{"[invalid", "[also-bad"})
	if out.Bucket != "real" {
		t.Errorf("Bucket: got %s; want real (invalid regexes skipped, falls through to real)", out.Bucket)
	}
}

func TestClassify_RealFallback(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{Status: "failure", Reason: "legitimate test regression"}
	out := ci.Classify(c, nil)
	if out.Bucket != "real" {
		t.Errorf("Bucket: got %s; want real", out.Bucket)
	}
}

func TestClassify_RealWithQuarantineNoMatch(t *testing.T) {
	t.Parallel()
	c := ci.CommitStatus{Status: "failure", Reason: "some test regression"}
	out := ci.Classify(c, []string{"TestUnrelatedFlake"})
	if out.Bucket != "real" {
		t.Errorf("Bucket: got %s; want real (quarantine entries don't match)", out.Bucket)
	}
}

func TestLoadFlakeQuarantine_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := ci.LoadFlakeQuarantine("/tmp/this-file-does-not-exist-zen-test")
	if err == nil {
		t.Fatal("expected error for missing file; got nil")
	}
}

func TestLoadFlakeQuarantine_EmptyFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\n# No entries (v1.0 baseline)\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	q, err := ci.LoadFlakeQuarantine(path)
	if err != nil {
		t.Fatalf("LoadFlakeQuarantine: %v", err)
	}
	if len(q.Entries) != 0 {
		t.Errorf("expected 0 entries; got %d", len(q.Entries))
	}
	if q.LastReview.IsZero() {
		t.Errorf("expected LastReview parsed; got zero time")
	}
}

func TestLoadFlakeQuarantine_ValidEntries(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestExampleFlaky " + now.Add(-3*24*time.Hour).Format(time.RFC3339) + " network-timeout\n" +
		"TestAnotherFlaky " + now.Add(-7*24*time.Hour).Format(time.RFC3339) + " gha-runner-flake\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	q, err := ci.LoadFlakeQuarantine(path)
	if err != nil {
		t.Fatalf("LoadFlakeQuarantine: %v", err)
	}
	if len(q.Entries) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(q.Entries))
	}
	if q.Entries[0].TestName != "TestExampleFlaky" {
		t.Errorf("entry[0] name: got %s", q.Entries[0].TestName)
	}
	if q.Entries[0].Reason != "network-timeout" {
		t.Errorf("entry[0] reason: got %s", q.Entries[0].Reason)
	}
	names := q.Names()
	if len(names) != 2 || names[0] != "TestExampleFlaky" || names[1] != "TestAnotherFlaky" {
		t.Errorf("Names(): got %v", names)
	}
}

func TestLoadFlakeQuarantine_14DayBoundaryRejected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()

	stale := now.Add(-14 * 24 * time.Hour).Format(time.RFC3339)
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestStale " + stale + " network-timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for 14d boundary entry; got nil (inv-zen-313 boundary is inclusive)")
	}
}

func TestLoadFlakeQuarantine_15DayRejected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	stale := now.Add(-15 * 24 * time.Hour).Format(time.RFC3339)
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestVeryStale " + stale + " network-timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for 15d entry; got nil")
	}
}

func TestLoadFlakeQuarantine_13DayAccepted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	fresh := now.Add(-13 * 24 * time.Hour).Format(time.RFC3339)
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestFresh " + fresh + " network-timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	q, err := ci.LoadFlakeQuarantine(path)
	if err != nil {
		t.Fatalf("expected no error for 13d entry; got: %v", err)
	}
	if len(q.Entries) != 1 {
		t.Errorf("expected 1 entry; got %d", len(q.Entries))
	}
}

func TestLoadFlakeQuarantine_Malformed1Token(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\nTestOnlyOneToken\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for 1-token row; got nil")
	}
}

func TestLoadFlakeQuarantine_Malformed2Token(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestTwoTokens 2026-05-15T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for 2-token row; got nil")
	}
}

func TestLoadFlakeQuarantine_InvalidTimestamp(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestBadTs not-a-timestamp network-timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for invalid timestamp; got nil")
	}
}

func TestLoadFlakeQuarantine_InvalidLastReviewHeader(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	content := "# Last review: not-a-date\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ci.LoadFlakeQuarantine(path)
	if err == nil {
		t.Fatal("expected error for malformed Last-review header; got nil")
	}
}

func TestLoadFlakeQuarantine_MultiTokenReasonRecombined(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	fresh := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
		"TestSpaceReason " + fresh + " intermittent network timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	q, err := ci.LoadFlakeQuarantine(path)
	if err != nil {
		t.Fatalf("LoadFlakeQuarantine: %v", err)
	}
	if len(q.Entries) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(q.Entries))
	}
	if q.Entries[0].Reason != "intermittent network timeout" {
		t.Errorf("Reason: got %q; want %q", q.Entries[0].Reason, "intermittent network timeout")
	}
}

func TestLoadFlakeQuarantine_BlankLinesIgnored(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "q.txt")
	now := time.Now().UTC()
	fresh := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	content := "\n# Last review: " + now.Format(time.RFC3339) + "\n\n" +
		"TestOne " + fresh + " network-timeout\n\n" +
		"TestTwo " + fresh + " network-timeout\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	q, err := ci.LoadFlakeQuarantine(path)
	if err != nil {
		t.Fatalf("LoadFlakeQuarantine: %v", err)
	}
	if len(q.Entries) != 2 {
		t.Errorf("expected 2 entries; got %d", len(q.Entries))
	}
}

func TestFlakeQuarantine_NamesNil(t *testing.T) {
	t.Parallel()
	var q *ci.FlakeQuarantine
	if q.Names() != nil {
		t.Error("nil receiver should return nil names")
	}
}
