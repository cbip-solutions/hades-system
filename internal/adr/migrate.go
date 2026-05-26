// SPDX-License-Identifier: MIT
package adr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type MigrateOptions struct {
	DryRun bool

	PlanFromRange string
}

type MigrationStatus string

const (
	MigrationStatusSuccess MigrationStatus = "success"

	MigrationStatusSkipped MigrationStatus = "skipped"

	MigrationStatusFailed MigrationStatus = "failed"
)

type MigrationReport struct {
	Files []MigrationFileResult
}

type MigrationFileResult struct {
	Path   string
	Status MigrationStatus
	Error  error
	Before string
	After  string
}

var (
	reTitleH1 = regexp.MustCompile(`^# ADR[- ](\d{4})\s*[:—–-]\s*(.+?)\s*$`)

	reStatus = regexp.MustCompile(`(?m)^\*\*Status:?\*\*:?\s*(.+)$`)

	reDate = regexp.MustCompile(`(?m)^\*\*Date:?\*\*:?\s*(.+)$`)

	reMaker = regexp.MustCompile(`(?m)^\*\*Decision-maker:?\*\*:?\s*(.+)$`)

	rePlan = regexp.MustCompile(`(?m)^\*\*Plan:?\*\*:?\s*(.+)$`)

	reRelated = regexp.MustCompile(`(?m)^\*\*Related:?\*\*:?\s*(.+)$`)

	rePlanTok = regexp.MustCompile(`(?i)plan[- ]?(\d+)`)

	reADRTok = regexp.MustCompile(`(?i)\bADR[- ](\d{4})\b`)
)

func MigrateDirectory(ctx context.Context, dir string, opts MigrateOptions) (*MigrationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("adr: readdir %s: %w", dir, err)
	}

	rep := &MigrationReport{}
	var paths []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res := migrateOne(p, opts)
		rep.Files = append(rep.Files, res)
	}
	return rep, nil
}

func migrateOne(path string, opts MigrateOptions) MigrationFileResult {
	raw, err := os.ReadFile(path)
	if err != nil {
		return MigrationFileResult{Path: path, Status: MigrationStatusFailed, Error: err}
	}

	a, err := Parse(bytes.NewReader(raw))
	if err == nil && a.HasFrontmatter() {
		return MigrationFileResult{Path: path, Status: MigrationStatusSkipped}
	}

	fm, body, err := extractFrontmatterFromLegacy(string(raw), opts.PlanFromRange, filepath.Base(path))
	if err != nil {
		return MigrationFileResult{Path: path, Status: MigrationStatusFailed, Error: err}
	}

	out, err := composeStructuredMADR(fm, body)
	if err != nil {
		return MigrationFileResult{Path: path, Status: MigrationStatusFailed, Error: err}
	}

	if opts.DryRun {
		return MigrationFileResult{
			Path: path, Status: MigrationStatusSuccess,
			Before: string(raw), After: out,
		}
	}

	tmp := path + ".migrate.tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return MigrationFileResult{Path: path, Status: MigrationStatusFailed, Error: err}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return MigrationFileResult{Path: path, Status: MigrationStatusFailed, Error: err}
	}
	return MigrationFileResult{Path: path, Status: MigrationStatusSuccess, Before: string(raw), After: out}
}

func extractFrontmatterFromLegacy(content, defaultPlan, fileName string) (Frontmatter, string, error) {
	var fm Frontmatter

	for _, line := range strings.Split(content, "\n") {
		if m := reTitleH1.FindStringSubmatch(line); m != nil {
			fm.ID = "ADR-" + m[1]
			fm.Title = strings.TrimSpace(m[2])
			break
		}
	}

	if fm.ID == "" {

		base := strings.TrimSuffix(fileName, ".md")
		if len(base) >= 4 {
			prefix := base[:4]
			isAllDigit := true
			for _, c := range prefix {
				if c < '0' || c > '9' {
					isAllDigit = false
					break
				}
			}
			if isAllDigit {
				fm.ID = "ADR-" + prefix
			}
		}
	}

	if m := reStatus.FindStringSubmatch(content); m != nil {
		fm.Status = normalizeStatus(strings.TrimSpace(m[1]))
	}
	if m := reDate.FindStringSubmatch(content); m != nil {
		fm.Date = normalizeDate(strings.TrimSpace(m[1]))
	}
	if m := reMaker.FindStringSubmatch(content); m != nil {
		fm.Deciders = parseDeciders(strings.TrimSpace(m[1]))
	}
	if m := rePlan.FindStringSubmatch(content); m != nil {
		fm.Plan = normalizePlan(strings.TrimSpace(m[1]))
	}
	if fm.Plan == "" {
		fm.Plan = defaultPlan
	}
	if m := reRelated.FindStringSubmatch(content); m != nil {
		fm.RelatesTo = parseRelated(strings.TrimSpace(m[1]))
	}

	if fm.Plan != "" {
		fm.Tags = []string{fm.Plan}
	} else {
		fm.Tags = []string{}
	}

	body := content
	for _, re := range []*regexp.Regexp{reStatus, reDate, reMaker, rePlan, reRelated} {
		body = re.ReplaceAllString(body, "")
	}

	body = collapseLeadingBlankLines(body)

	return fm, body, nil
}

func normalizeStatus(s string) Status {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch {
	case low == "accepted":
		return StatusAccepted
	case low == "proposed":
		return StatusProposed
	case low == "rejected":
		return StatusRejected
	case low == "superseded":
		return StatusSuperseded
	case low == "deprecated":
		return StatusDeprecated
	case strings.HasPrefix(low, "reserved"):
		return StatusReserved
	default:

		return Status(s)
	}
}

func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		return s
	}
	return s
}

func parseDeciders(s string) []string {

	for strings.Contains(s, "(") && strings.Contains(s, ")") {
		l := strings.Index(s, "(")
		r := strings.Index(s, ")")
		if r < l {
			break
		}
		s = strings.TrimSpace(s[:l] + s[r+1:])
	}

	for strings.Contains(s, "`") {
		l := strings.Index(s, "`")
		r := strings.Index(s[l+1:], "`")
		if r < 0 {
			break
		}
		r = l + 1 + r
		s = strings.TrimSpace(s[:l] + s[r+1:])
	}

	if i := strings.Index(strings.ToLower(s), " via "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizePlan(s string) string {
	if m := rePlanTok.FindStringSubmatch(s); m != nil {
		return "plan-" + m[1]
	}
	return ""
}

func parseRelated(s string) []string {
	matches := reADRTok.FindAllStringSubmatch(s, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range matches {
		id := "ADR-" + m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func collapseLeadingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	skipping := true
	for _, l := range lines {
		if skipping && strings.TrimSpace(l) == "" {
			continue
		}
		skipping = false
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func composeStructuredMADR(fm Frontmatter, body string) (string, error) {
	yml, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("adr: marshal yaml: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(yml)
	sb.WriteString("---\n\n")
	sb.WriteString(body)
	return sb.String(), nil
}
