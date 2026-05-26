// SPDX-License-Identifier: MIT
// Package intent — Lore git-trailer parsing (CGO-agnostic core).
//
// parseTrailerLines + trailerKindFor are pure functions (no I/O): given a
// commit message body, return the Lore-* trailers found in the trailing
// trailer block, mapped to the locked store.TrailerKind consts. The
// store-backed indexer that consumes these lives in lore.go (//go:build cgo).
//
// Trailer-block semantics mirror git-interpret-trailers: trailers are
// recognized only in the contiguous run of "Key: value" lines at the END of
// the message (the footer), so a Lore key quoted in prose is not a trailer.
// See spec §10 (get_why — Lore source) + Lore protocol (arXiv 2603.15566).
package intent

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type ParsedTrailer struct {
	Kind store.TrailerKind
	Body string
}

type LoreIndexResult struct {
	CommitsScanned int
	Trailers       int
	WrittenRows    int
}

// loreTrailerKeys maps the four canonical Lore-* git-trailer keys (spec §10)
// to the locked store.TrailerKind consts (Phase A C-4). Case-sensitive on the
// canonical Title-Case-with-hyphens form: a worker MUST write the exact key
// the doctrine specifies, so a typo'd "lore-constraint" conveys no intent and
// is correctly ignored (no silent mis-attribution).
var loreTrailerKeys = map[string]store.TrailerKind{
	"Lore-Constraint":      store.TrailerConstraint,
	"Lore-Rejected":        store.TrailerRejected,
	"Lore-Agent-Directive": store.TrailerAgentDirective,
	"Lore-Verification":    store.TrailerVerification,
}

func trailerKindFor(key string) (store.TrailerKind, bool) {
	k, ok := loreTrailerKeys[key]
	return k, ok
}

func parseTrailerLines(body string) []ParsedTrailer {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}

	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		if isContinuation(line) || trailerKeyOf(line) != "" {
			start = i
			continue
		}
		break
	}
	footer := lines[start:]

	var folded []string
	for _, line := range footer {
		if isContinuation(line) && len(folded) > 0 {
			folded[len(folded)-1] += " " + strings.TrimSpace(line)
			continue
		}
		folded = append(folded, line)
	}

	out := []ParsedTrailer{}
	for _, line := range folded {
		key := trailerKeyOf(line)
		if key == "" {
			continue
		}
		kind, ok := trailerKindFor(key)
		if !ok {
			continue
		}
		colon := strings.IndexByte(line, ':')
		value := strings.TrimSpace(line[colon+1:])
		if value == "" {
			continue
		}
		out = append(out, ParsedTrailer{Kind: kind, Body: value})
	}
	return out
}

func trailerKeyOf(line string) string {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return ""
	}
	key := line[:colon]
	if key != strings.TrimSpace(key) {
		return ""
	}
	for _, r := range key {
		isAZ := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		isDigit := r >= '0' && r <= '9'
		if !isAZ && !isDigit && r != '-' {
			return ""
		}
	}
	return key
}

func isContinuation(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

const (
	loreFieldSep = "\x1f"
	loreRecSep   = "\x1e"
)

type loreCommit struct {
	sha      string
	email    string
	unixTime int64
	body     string
	files    []string
}

func parseLoreLog(out string) []loreCommit {
	chunks := strings.Split(out, loreRecSep)
	type pendingCommit struct {
		sha      string
		email    string
		unixTime int64
		body     string
	}
	var pending *pendingCommit
	commits := make([]loreCommit, 0, len(chunks))

	for _, chunk := range chunks {
		files, hdr := splitChunkFilesAndHeader(chunk)

		if pending != nil {
			commits = append(commits, loreCommit{
				sha:      pending.sha,
				email:    pending.email,
				unixTime: pending.unixTime,
				body:     pending.body,
				files:    files,
			})
			pending = nil
		}

		if hdr != "" {
			fields := strings.SplitN(hdr, loreFieldSep, 4)
			if len(fields) >= 4 {
				sha := strings.TrimSpace(fields[0])
				email := strings.TrimSpace(fields[1])
				unix, perr := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
				if sha != "" && perr == nil {
					pending = &pendingCommit{
						sha:      sha,
						email:    email,
						unixTime: unix,
						body:     fields[3],
					}
				}
			}
		}
	}

	if pending != nil {
		commits = append(commits, loreCommit{
			sha:      pending.sha,
			email:    pending.email,
			unixTime: pending.unixTime,
			body:     pending.body,
			files:    nil,
		})
	}
	return commits
}

func splitChunkFilesAndHeader(chunk string) (files []string, hdr string) {
	lines := strings.Split(chunk, "\n")
	hdrStart := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], loreFieldSep) {
			hdrStart = i
			break
		}
	}
	if hdrStart < 0 {

		for _, l := range lines {
			if s := strings.TrimSpace(l); s != "" {
				files = append(files, s)
			}
		}
		return files, ""
	}

	for _, l := range lines[:hdrStart] {
		if s := strings.TrimSpace(l); s != "" {
			files = append(files, s)
		}
	}
	hdr = strings.Join(lines[hdrStart:], "\n")
	return files, hdr
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
	body := strings.Join(lines[:bodyEnd], "\n")
	return body, files
}

func primaryTouchedNode(files []string, fileNodes map[string][]string) (string, string) {
	if len(files) == 0 {
		return "", ""
	}
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		nodes := fileNodes[f]
		if len(nodes) == 0 {
			continue
		}
		ns := append([]string(nil), nodes...)
		sort.Strings(ns)
		return f, ns[0]
	}
	return sorted[0], ""
}
