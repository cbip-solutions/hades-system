// SPDX-License-Identifier: MIT
// Package loretrailer implements the Lore-enforcement go-vet analyzer for
// zen-swarm Plan 19 Caronte (spec §10; inv-zen-238). WHEN ENABLED
// (-loretrailer.enabled=true; default false, adoption-gated per spec §21), it
// scans branch-local commit BODIES and flags any commit that touches a
// high-risk file (the -loretrailer.high-risk-files glob set, supplied by the
// daemon from caronte coreness/blast-radius) without a Lore-Constraint:
// git-trailer.
//
// Mechanism mirrors the as-built conventional_commit analyzer
// (internal/doctrine/lint/analyzers/conventional_commit/analyzer.go): shell
// out to git log, run-once guard, -base-ref/-depth mode split, skip-when-no-
// git. The difference: this analyzer reads commit bodies (%B) to find Lore
// trailers, where conventional_commit reads subjects (%s) to validate shape.
//
// DEFAULT OFF: with -loretrailer.enabled=false (the zero value) the analyzer
// is a no-op — existing zen-doctrine-lint behavior is unchanged. This is the
// frozen-surface guarantee for the binary (Plan 8/9/14 analyzers untouched).
package loretrailer

import (
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
)

type Diagnostic struct {
	CommitHash string
	Subject    string
	Message    string
}

type Options struct {
	Enabled       bool
	HighRiskFiles []string
	BaseRef       string
	Depth         int
}

var (
	enabledFlag       bool
	highRiskFilesFlag string
	baseRefFlag       string
	depthFlag         = 50
	skipWhenNoGitFlag bool
	flagSetOnce       = newFlagSet()
)

func newFlagSet() flag.FlagSet {
	fs := flag.NewFlagSet("loretrailer", flag.ContinueOnError)
	fs.BoolVar(&enabledFlag, "enabled", false,
		"enable Lore-Constraint enforcement on high-risk commits (default false — adoption-gated, spec §21)")
	fs.StringVar(&highRiskFilesFlag, "high-risk-files", "",
		"comma-separated path globs whose touch requires a Lore-Constraint trailer "+
			"(supplied by the daemon from caronte coreness/blast-radius; empty = no-op)")
	fs.StringVar(&baseRefFlag, "base-ref", "",
		"limit scan to <base-ref>..HEAD (branch-local commits only); empty = last -depth commits")
	fs.IntVar(&depthFlag, "depth", 50, "how many recent commits to scan when -base-ref is empty")
	fs.BoolVar(&skipWhenNoGitFlag, "skip-when-no-git", false,
		"silently return nil if the git binary is unavailable (CI minimal images)")
	return *fs
}

var Analyzer = &analysis.Analyzer{
	Name: "loretrailer",
	Doc: "When -loretrailer.enabled=true (default false), flags branch-local commits that touch a " +
		"high-risk file (-loretrailer.high-risk-files glob set) without a Lore-Constraint: git-trailer " +
		"(spec §10; inv-zen-238). Default OFF = no-op (adoption-gated, spec §21). Scan range: " +
		"-loretrailer.base-ref=<ref> (branch-local) or -loretrailer.depth=N (last N commits).",
	Flags: flagSetOnce,
	Run:   run,
}

var runOnce atomic.Bool

func ResetOnceForTest() { runOnce.Store(false) }

func run(pass *analysis.Pass) (any, error) {
	if !runOnce.CompareAndSwap(false, true) {
		return nil, nil
	}
	opts := Options{
		Enabled:       enabledFlag,
		HighRiskFiles: splitCSV(highRiskFilesFlag),
		BaseRef:       baseRefFlag,
		Depth:         depthFlag,
	}
	diags, err := RunWithGitDir(".", opts)
	if err != nil {
		if skipWhenNoGitFlag && strings.Contains(err.Error(), "exec: \"git\":") {
			return nil, nil
		}
		return nil, err
	}
	if len(pass.Files) == 0 {
		if len(diags) > 0 {
			return nil, fmt.Errorf("loretrailer: %d high-risk commits missing Lore-Constraint (no source file to anchor)", len(diags))
		}
		return nil, nil
	}
	anchor := pass.Files[0].Pos()
	for _, d := range diags {
		pass.Reportf(anchor, "%s (commit %s): %q", d.Message, d.CommitHash, d.Subject)
	}
	return nil, nil
}

func RunWithGitDir(gitDir string, opts Options) ([]Diagnostic, error) {
	if !opts.Enabled || len(opts.HighRiskFiles) == 0 {
		return nil, nil
	}
	depth := opts.Depth
	if depth < 1 {
		depth = 1
	}

	pretty := "--pretty=format:%H" + fieldSep + "%s" + fieldSep + "%B" + recSep
	var args []string
	if opts.BaseRef != "" {
		args = []string{"log", opts.BaseRef + "..HEAD", pretty, "--name-only"}
	} else {
		args = []string{"log", pretty, "--name-only", fmt.Sprintf("-n%d", depth)}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("loretrailer: git log failed in %s: %v\n%s", gitDir, err, out)
	}

	commits := parseLoreCommits(string(out))
	var diags []Diagnostic
	for _, c := range commits {
		if !touchesHighRisk(c.files, opts.HighRiskFiles) {
			continue
		}
		if hasLoreConstraint(c.body) {
			continue
		}
		diags = append(diags, Diagnostic{
			CommitHash: c.hash, Subject: c.subject,
			Message: "lore-missing-constraint: commit touches a high-risk node but carries no " +
				"Lore-Constraint: trailer (inv-zen-238; spec §10). Add a Lore-Constraint trailer " +
				"recording the architectural invariant the change must preserve, or lower the node's risk.",
		})
	}
	return diags, nil
}

type loreEntry struct {
	hash    string
	subject string
	body    string
	files   []string
}

func parseLoreCommits(out string) []loreEntry {
	chunks := strings.Split(out, recSep)
	type pending struct {
		hash    string
		subject string
		body    string
	}
	var p *pending
	var result []loreEntry
	for _, chunk := range chunks {

		lines := strings.Split(chunk, "\n")
		hdrIdx := -1
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], fieldSep) {
				hdrIdx = i
				break
			}
		}

		var fileLines []string
		limit := hdrIdx
		if limit < 0 {
			limit = len(lines)
		}
		for _, l := range lines[:limit] {
			if s := strings.TrimSpace(l); s != "" {
				fileLines = append(fileLines, s)
			}
		}

		if p != nil {
			result = append(result, loreEntry{
				hash:    p.hash,
				subject: p.subject,
				body:    p.body,
				files:   fileLines,
			})
			p = nil
		} else if len(fileLines) > 0 && len(result) > 0 {

			last := &result[len(result)-1]
			last.files = append(last.files, fileLines...)
		}

		if hdrIdx >= 0 {
			hdr := strings.Join(lines[hdrIdx:], "\n")
			fields := strings.SplitN(hdr, fieldSep, 3)
			if len(fields) >= 3 {
				hash := strings.TrimSpace(fields[0])
				subject := strings.TrimSpace(fields[1])
				body := fields[2]
				if hash != "" {
					p = &pending{hash: hash, subject: subject, body: body}
				}
			}
		}
	}

	if p != nil {
		result = append(result, loreEntry{
			hash:    p.hash,
			subject: p.subject,
			body:    p.body,
			files:   nil,
		})
	}
	return result
}

const (
	fieldSep = "\x1f"
	recSep   = "\x1e"
)

func touchesHighRisk(files, globs []string) bool {
	for _, f := range files {
		for _, g := range globs {
			if ok, _ := filepath.Match(g, f); ok {
				return true
			}

			if matchPrefixGlob(g, f) {
				return true
			}
		}
	}
	return false
}

func matchPrefixGlob(glob, file string) bool {
	switch {
	case strings.HasSuffix(glob, "/**"):
		return strings.HasPrefix(file, strings.TrimSuffix(glob, "**"))
	case strings.HasSuffix(glob, "/*"):
		dir := strings.TrimSuffix(glob, "*")
		rest := strings.TrimPrefix(file, dir)
		return strings.HasPrefix(file, dir) && !strings.Contains(rest, "/")
	default:
		return false
	}
}

func hasLoreConstraint(body string) bool {
	for _, line := range trailingFooter(body) {
		if strings.HasPrefix(line, "Lore-Constraint:") &&
			strings.TrimSpace(strings.TrimPrefix(line, "Lore-Constraint:")) != "" {
			return true
		}
	}
	return false
}

func trailingFooter(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			break
		}
		if isFooterLine(l) {
			start = i
			continue
		}
		break
	}
	footer := lines[start:]
	var folded []string
	for _, l := range footer {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(folded) > 0 {
			folded[len(folded)-1] += " " + strings.TrimSpace(l)
			continue
		}
		folded = append(folded, l)
	}
	return folded
}

func isFooterLine(line string) bool {
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return true
	}
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return false
	}
	for _, r := range line[:colon] {
		isAZ := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !isAZ && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

func splitBodyAndFiles(blob string) (string, []string) {
	lines := strings.Split(blob, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	fileStart := end
	for fileStart > 0 && strings.TrimSpace(lines[fileStart-1]) != "" {
		fileStart--
	}
	var files []string
	for _, l := range lines[fileStart:end] {
		if s := strings.TrimSpace(l); s != "" {
			files = append(files, s)
		}
	}

	bodyEnd := fileStart
	for bodyEnd > 0 && strings.TrimSpace(lines[bodyEnd-1]) == "" {
		bodyEnd--
	}
	return strings.Join(lines[:bodyEnd], "\n"), files
}

func splitCSV(csv string) []string {
	if csv == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
