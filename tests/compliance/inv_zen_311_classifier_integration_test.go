package compliance_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

func TestInvZenG6_ClassifierVersionConstantExists(t *testing.T) {
	t.Parallel()

	if ci.ClassifierVersion == "" {
		t.Fatal("inv-zen-311: internal/ci/classifier.ClassifierVersion is empty")
	}
}

func TestInvZenG6_LoadFlakeQuarantineRejectsExpiredEntries(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	tests := []struct {
		name           string
		entryAge       time.Duration
		wantValidCount int
		wantError      bool
	}{
		{"1d fresh — accepted", 1 * 24 * time.Hour, 1, false},
		{"13d fresh — accepted", 13 * 24 * time.Hour, 1, false},
		{"14d boundary — rejected", 14 * 24 * time.Hour, 0, true},
		{"15d stale — rejected", 15 * 24 * time.Hour, 0, true},
		{"30d very stale — rejected", 30 * 24 * time.Hour, 0, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "flake-quarantine.txt")

			entryTs := now.Add(-tc.entryAge).Format(time.RFC3339)
			content := "# Last review: " + now.Format(time.RFC3339) + "\n" +
				"TestExampleFlaky " + entryTs + " network-timeout\n"
			if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
				t.Fatalf("inv-zen-311: write tmp file: %v", err)
			}

			quarantine, err := ci.LoadFlakeQuarantine(tmpFile)

			if tc.wantError {
				if err == nil && quarantine != nil && len(quarantine.Entries) > 0 {
					t.Errorf("inv-zen-311: expected rejection for entry age %v; got nil err + %d entries",
						tc.entryAge, len(quarantine.Entries))
				}
			} else {
				if err != nil {
					t.Errorf("inv-zen-311: expected no error for entry age %v; got err: %v", tc.entryAge, err)
				}
				if quarantine == nil {
					t.Fatalf("inv-zen-311: LoadFlakeQuarantine returned nil for fresh entry; expected pointer")
				}
				if len(quarantine.Entries) != tc.wantValidCount {
					t.Errorf("inv-zen-311: expected %d valid entries; got %d", tc.wantValidCount, len(quarantine.Entries))
				}
			}
		})
	}
}

func TestInvZenG6_LoadFlakeQuarantineHandlesEmptyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flake-quarantine.txt")

	now := time.Now().UTC()
	content := "# Last review: " + now.Format(time.RFC3339) + "\n# No entries (v1.0 baseline)\n"
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		t.Fatalf("inv-zen-311: write tmp file: %v", err)
	}

	quarantine, err := ci.LoadFlakeQuarantine(tmpFile)
	if err != nil {
		t.Fatalf("inv-zen-311: LoadFlakeQuarantine(empty): err = %v; want nil", err)
	}
	if quarantine == nil {
		t.Fatalf("inv-zen-311: LoadFlakeQuarantine returned nil for empty file; expected pointer with zero entries")
	}
	if len(quarantine.Entries) != 0 {
		t.Errorf("inv-zen-311: empty file should yield 0 entries; got %d", len(quarantine.Entries))
	}
}

func TestInvZenG6_RollingWindowGateSemantics(t *testing.T) {
	t.Parallel()

	w := ci.DefaultRollingWindow()

	tests := []struct {
		name     string
		commits  []ci.CommitStatus
		wantPass bool
	}{
		{
			name:     "50 commits all success — pass",
			commits:  buildCommits(50, 0, 0, 0),
			wantPass: true,
		},
		{
			name:     "45 success + 2 real + 3 infra — pass (47 classified ≥ MinSample, 45/47 = 0.957 ≥ 0.90, real 2 ≤ 2)",
			commits:  buildCommits(45, 2, 3, 0),
			wantPass: true,
		},
		{
			name:     "44 success + 3 real — fail (real 3 > 2 cap)",
			commits:  buildCommits(44, 3, 0, 0),
			wantPass: false,
		},
		{
			name:     "30 success + 0 real + 20 infra — pass (30 classified at min sample boundary, ratio 1.0)",
			commits:  buildCommits(30, 0, 20, 0),
			wantPass: true,
		},
		{
			name:     "20 success + 1 real + 29 infra — fail (only 21 classified < min sample 30)",
			commits:  buildCommits(20, 1, 29, 0),
			wantPass: false,
		},
		{
			name:     "40 success + 5 real — fail (real 5 > 2 cap; ratio 0.888 < 0.90 also fails)",
			commits:  buildCommits(40, 5, 0, 0),
			wantPass: false,
		},
		{
			name:     "40 success + 0 real + 5 flake + 5 infra — pass (40 classified, ratio 1.0)",
			commits:  buildCommits(40, 0, 5, 5),
			wantPass: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pass, reason := w.Evaluate(tc.commits)
			if pass != tc.wantPass {
				t.Errorf("inv-zen-311: Evaluate: got pass=%v; want pass=%v; reason=%q", pass, tc.wantPass, reason)
			}
		})
	}
}

func buildCommits(success, realFail, infra, flake int) []ci.CommitStatus {
	commits := make([]ci.CommitStatus, 0, success+realFail+infra+flake)
	for i := 0; i < success; i++ {
		commits = append(commits, ci.CommitStatus{Status: "success", Bucket: "success"})
	}
	for i := 0; i < realFail; i++ {
		commits = append(commits, ci.CommitStatus{Status: "failure", Bucket: "real"})
	}
	for i := 0; i < infra; i++ {
		commits = append(commits, ci.CommitStatus{Status: "failure", Bucket: "infra"})
	}
	for i := 0; i < flake; i++ {
		commits = append(commits, ci.CommitStatus{Status: "failure", Bucket: "flake"})
	}
	return commits
}
