package compliance_test

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestInvZenG3_FlakeQuarantineFileExists(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/flake-quarantine.txt")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("inv-zen-313: scripts/release-gates/flake-quarantine.txt not found: %v", err)
	}
}

func TestInvZenG3_FlakeQuarantineHeaderPresent(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/flake-quarantine.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("inv-zen-313: open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	headerFound := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# Last review:") {
			tsStr := strings.TrimSpace(strings.TrimPrefix(line, "# Last review:"))
			if _, err := time.Parse(time.RFC3339, tsStr); err != nil {
				t.Errorf("inv-zen-313: # Last review timestamp not valid RFC3339: %q (err: %v)", tsStr, err)
			}
			headerFound = true
			break
		}
	}
	if scanner.Err() != nil {
		t.Fatalf("inv-zen-313: scanner error: %v", scanner.Err())
	}
	if !headerFound {
		t.Error("inv-zen-313: flake-quarantine.txt missing `# Last review: <ISO8601>` header")
	}
}

func TestInvZenG3_FlakeQuarantineEntryFormat(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/flake-quarantine.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("inv-zen-313: open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		tokens := strings.Fields(line)
		if len(tokens) != 3 {
			t.Errorf("inv-zen-313: line %d: expected 3 tokens (test-name + ISO8601 + reason-tag); got %d: %q",
				lineNum, len(tokens), line)
			continue
		}

		testName := tokens[0]
		tsStr := tokens[1]
		reasonTag := tokens[2]

		if testName == "" {
			t.Errorf("inv-zen-313: line %d: test-name is empty", lineNum)
		}
		if _, err := time.Parse(time.RFC3339, tsStr); err != nil {
			t.Errorf("inv-zen-313: line %d: timestamp %q is not valid RFC3339: %v", lineNum, tsStr, err)
		}
		if reasonTag == "" {
			t.Errorf("inv-zen-313: line %d: reason-tag is empty", lineNum)
		}
	}
}

func TestInvZenG3_FlakeQuarantineEntriesAutoExpireAfter14d(t *testing.T) {
	t.Parallel()

	scriptPath := repoPath_g(t, "scripts/release-gates/validate-flake-quarantine.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("inv-zen-313: validate-flake-quarantine.sh not found: %v", err)
	}

	now := time.Now().UTC()
	tests := []struct {
		name         string
		quarantineTs string
		wantExpired  bool
	}{
		{"13d ago — fresh", now.Add(-13 * 24 * time.Hour).Format(time.RFC3339), false},
		{"14d ago boundary — expired", now.Add(-14 * 24 * time.Hour).Format(time.RFC3339), true},
		{"15d ago — expired", now.Add(-15 * 24 * time.Hour).Format(time.RFC3339), true},
		{"1d ago — fresh", now.Add(-1 * 24 * time.Hour).Format(time.RFC3339), false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpFile := writeTempQuarantineFile_g(t, now.Format(time.RFC3339), tc.quarantineTs)
			defer os.Remove(tmpFile)

			cmd := exec.Command(scriptPath, tmpFile)
			out, err := cmd.CombinedOutput()
			exitedNonZero := err != nil

			if tc.wantExpired && !exitedNonZero {
				t.Errorf("inv-zen-313: expected validate-flake-quarantine.sh to exit non-zero for expired entry (ts=%s); got exit 0; output:\n%s",
					tc.quarantineTs, out)
			}
			if !tc.wantExpired && exitedNonZero {
				t.Errorf("inv-zen-313: expected validate-flake-quarantine.sh to exit 0 for fresh entry (ts=%s); got non-zero; output:\n%s",
					tc.quarantineTs, out)
			}
		})
	}
}

func TestInvZenG3_FlakeQuarantineHeaderFreshness(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/flake-quarantine.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("inv-zen-313: open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var headerTs time.Time
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# Last review:") {
			tsStr := strings.TrimSpace(strings.TrimPrefix(line, "# Last review:"))
			parsed, err := time.Parse(time.RFC3339, tsStr)
			if err != nil {
				t.Fatalf("inv-zen-313: # Last review timestamp invalid: %v", err)
			}
			headerTs = parsed
			break
		}
	}

	if headerTs.IsZero() {
		t.Fatal("inv-zen-313: # Last review header missing")
	}

	now := time.Now().UTC()
	if headerTs.After(now.Add(1 * time.Hour)) {
		t.Errorf("inv-zen-313: # Last review timestamp is in the future: %v (now: %v)", headerTs, now)
	}
	if headerTs.Before(now.Add(-365 * 24 * time.Hour)) {
		t.Errorf("inv-zen-313: # Last review timestamp is >1y old: %v (now: %v)", headerTs, now)
	}
}

func writeTempQuarantineFile_g(t *testing.T, headerTs, entryTs string) string {
	t.Helper()

	tmp, err := os.CreateTemp("", "flake-quarantine-*.txt")
	if err != nil {
		t.Fatalf("inv-zen-313: create temp: %v", err)
	}
	defer tmp.Close()

	content := "# Last review: " + headerTs + "\n" +
		"# Test quarantine list for inv-zen-313 boundary test\n" +
		"TestExampleFlaky " + entryTs + " network-timeout\n"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("inv-zen-313: write temp: %v", err)
	}
	return tmp.Name()
}
