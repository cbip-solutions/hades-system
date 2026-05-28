// SPDX-License-Identifier: MIT
// ClassifierVersion constant + LoadFlakeQuarantine quarantine loader.
//
// Classification rules (spec §7.3; .hades/session.md "permanent-red trap"
// context):
//
// - success → bucket "success"
// - failure + infra_pattern regex match → bucket "infra"
// - failure + flake quarantine match → bucket "flake"
// - otherwise failure → bucket "real"
//
// ClassifierVersion is bumped on any classification-rule change. Cache
// entries embed this version; classifier rejects stale-version entries
// (forces re-classification when rules evolve). Referenced by
// G-6 compliance tests (invariant versioning semantics).
//
// Coverage target ≥90% per project instructions security/correctness-critical list.
package ci

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const ClassifierVersion = "1.1"

const FlakeQuarantineMaxAge = 14 * 24 * time.Hour

type CommitStatus struct {
	SHA    string    `json:"SHA"`
	Status string    `json:"Status"`
	Bucket string    `json:"Bucket"`
	Reason string    `json:"Reason"`
	URL    string    `json:"URL"`
	Date   time.Time `json:"Date"`
}

// FlakeQuarantine is the parsed form of scripts/release checks/flake-
// quarantine.txt. Consumed by Classify (via the simple
// []string list of test-name regexes) AND by the validator
// script + compliance tests.
//
// File format (spec §G.3.1):
//
// # Last review: 2026-05-15T00:00:00Z
// TestExampleFlaky 2026-05-08T00:00:00Z network-timeout
// TestAnotherFlaky 2026-05-10T00:00:00Z gha-runner-flake
//
// Each entry MUST have exactly 3 whitespace-separated tokens:
// test-name, quarantined-since (RFC3339), reason-tag.
// Entries older than FlakeQuarantineMaxAge (14d) are rejected.
type FlakeQuarantine struct {
	LastReview time.Time
	Entries    []FlakeQuarantineEntry
}

type FlakeQuarantineEntry struct {
	TestName   string
	Quarantine time.Time
	Reason     string
}

func (q *FlakeQuarantine) Names() []string {
	if q == nil {
		return nil
	}
	out := make([]string, 0, len(q.Entries))
	for _, e := range q.Entries {
		out = append(out, e.TestName)
	}
	return out
}

var infraPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)gha\s+billing`),
	regexp.MustCompile(`(?i)billing\s+block`),
	regexp.MustCompile(`(?i)runner.{0,30}exhausted`),
	regexp.MustCompile(`(?i)runner.{0,30}pool`),
	regexp.MustCompile(`(?i)network.{0,30}timeout`),
	regexp.MustCompile(`(?i)\boom\b`),
	regexp.MustCompile(`(?i)out\s+of\s+memory`),
	regexp.MustCompile(`(?i)429.{0,30}rate.?limit`),
	regexp.MustCompile(`(?i)503.{0,30}service.?unavailable`),
	regexp.MustCompile(`(?i)recent\s+account\s+payments\s+have\s+failed`),
	regexp.MustCompile(`(?i)spending\s+limit\s+needs\s+to\s+be\s+increased`),
}

func Classify(commit CommitStatus, flakeQuarantine []string) CommitStatus {
	if commit.Status == "success" {
		commit.Bucket = "success"
		return commit
	}
	if commit.Status != "failure" {

		commit.Bucket = "real"
		return commit
	}

	for _, pat := range infraPatterns {
		if pat.MatchString(commit.Reason) {
			commit.Bucket = "infra"
			return commit
		}
	}
	for _, quarantineEntry := range flakeQuarantine {
		if quarantineEntry == "" {
			continue
		}
		pat, err := regexp.Compile(quarantineEntry)
		if err != nil {

			continue
		}
		if pat.MatchString(commit.Reason) {
			commit.Bucket = "flake"
			return commit
		}
	}
	commit.Bucket = "real"
	return commit
}

func LoadFlakeQuarantine(path string) (*FlakeQuarantine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ci: open quarantine %s: %w", path, err)
	}
	defer f.Close()

	now := time.Now().UTC()
	q := &FlakeQuarantine{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "#") {

			if strings.HasPrefix(strings.ToLower(raw), "# last review:") {
				tsRaw := strings.TrimSpace(raw[len("# last review:"):])
				ts, err := time.Parse(time.RFC3339, tsRaw)
				if err != nil {
					return nil, fmt.Errorf("ci: quarantine %s line %d: invalid Last review timestamp %q: %w", path, lineNo, tsRaw, err)
				}
				q.LastReview = ts
			}
			continue
		}

		toks := strings.Fields(raw)
		if len(toks) < 3 {
			return nil, fmt.Errorf("ci: quarantine %s line %d: expected 3 tokens (test, ts, reason); got %d in %q", path, lineNo, len(toks), raw)
		}
		ts, err := time.Parse(time.RFC3339, toks[1])
		if err != nil {
			return nil, fmt.Errorf("ci: quarantine %s line %d: invalid timestamp %q: %w", path, lineNo, toks[1], err)
		}
		age := now.Sub(ts)
		if age >= FlakeQuarantineMaxAge {
			return nil, fmt.Errorf("ci: quarantine %s line %d: entry %q is %v old (≥ %v cap; auto-expire policy invariant)", path, lineNo, toks[0], age.Round(time.Hour), FlakeQuarantineMaxAge)
		}

		reason := strings.Join(toks[2:], " ")
		q.Entries = append(q.Entries, FlakeQuarantineEntry{
			TestName:   toks[0],
			Quarantine: ts,
			Reason:     reason,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ci: read quarantine %s: %w", path, err)
	}
	return q, nil
}
