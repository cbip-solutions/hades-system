// SPDX-License-Identifier: MIT
// Package augment — community_summarize.go ships GraphRAG-style structural
// cluster summarization (no LLM call). Per SOTA Report 2 (Microsoft GraphRAG
// abril 2024), community summarization yields ~97% token reduction vs raw
// triples.
//
// Algorithm
//   1. Cluster fused results by file-path common-prefix (>= 2 dirs deep).
//   2. Per cluster: derive ClusterID, Topic, Files, Symbols, NoteIDs, TokenCount.
//   3. Cap output at MaxCommunitiesDefault (5).
//   4. Sort clusters by aggregate Score descending.
//
// Determinism same input -> same output (stabilizes Anthropic prompt cache).

package augment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

const MaxCommunitiesDefault = 5

const MinPathDepthForClustering = 2

func Summarize(_ context.Context, fused []RRFFusedResult, _ string) ([]CommunitySummary, error) {
	if len(fused) == 0 {
		return nil, nil
	}

	type cluster struct {
		clusterID    string
		commonPrefix string
		files        map[string]struct{}
		symbols      map[string]struct{}
		noteIDs      []string
		aggScore     float64
	}

	clusters := map[string]*cluster{}

	for _, f := range fused {
		path, sym := extractPathAndSymbol(f.Title)
		key := pathClusterKey(path)
		c, ok := clusters[key]
		if !ok {
			c = &cluster{
				clusterID:    key,
				commonPrefix: key,
				files:        map[string]struct{}{},
				symbols:      map[string]struct{}{},
			}
			clusters[key] = c
		}
		if path != "" {
			c.files[path] = struct{}{}
		}
		if sym != "" {
			c.symbols[sym] = struct{}{}
		}
		c.noteIDs = append(c.noteIDs, f.NoteID)
		c.aggScore += f.Score
	}

	out := make([]CommunitySummary, 0, len(clusters))
	for _, c := range clusters {
		files := setToSortedSlice(c.files)
		symbols := setToSortedSlice(c.symbols)
		topic := inferTopic(symbols)
		tokenCount := estimateTokens(c.commonPrefix, files, symbols)
		out = append(out, CommunitySummary{
			ClusterID:  c.clusterID,
			Topic:      topic,
			Files:      files,
			Symbols:    symbols,
			NoteIDs:    append([]string(nil), c.noteIDs...),
			TokenCount: tokenCount,
		})
	}

	scoreByCluster := make(map[string]float64, len(clusters))
	for _, c := range clusters {
		scoreByCluster[c.clusterID] = c.aggScore
	}

	sort.SliceStable(out, func(i, j int) bool {
		si := scoreByCluster[out[i].ClusterID]
		sj := scoreByCluster[out[j].ClusterID]
		if si != sj {
			return si > sj
		}
		return out[i].ClusterID < out[j].ClusterID
	})

	if len(out) > MaxCommunitiesDefault {
		out = out[:MaxCommunitiesDefault]
	}

	return out, nil
}

func extractPathAndSymbol(title string) (path, symbol string) {
	idx := strings.LastIndex(title, " | ")
	if idx < 0 {
		return "", strings.TrimSpace(title)
	}
	symbol = strings.TrimSpace(title[:idx])
	path = strings.TrimSpace(title[idx+3:])
	if colon := strings.LastIndex(path, ":"); colon > 0 {
		suffix := path[colon+1:]
		if isAllDigits(suffix) {
			path = path[:colon]
		}
	}
	return path, symbol
}

func pathClusterKey(path string) string {
	if path == "" {
		return "uncategorized"
	}
	parts := strings.Split(path, "/")
	if parts[0] == "" {
		parts = parts[1:]
	}
	depth := MinPathDepthForClustering
	if len(parts) < depth {
		depth = len(parts) - 1
		if depth < 1 {
			depth = 1
		}
	}
	return strings.Join(parts[:depth], "/")
}

func inferTopic(symbols []string) string {
	if len(symbols) == 0 {
		return "code"
	}
	counts := map[string]int{}
	for _, s := range symbols {
		core := s
		if dot := strings.Index(s, "."); dot >= 0 {
			core = s[dot+1:]
		}
		if core == "" {
			continue
		}
		first := core[0]
		switch {
		case isAllUpper(core):
			counts["const"]++
		case first >= 'A' && first <= 'Z':
			counts["type"]++
		case first >= 'a' && first <= 'z':
			counts["function"]++
		default:
			counts["code"]++
		}
	}
	best := "code"
	bestCount := 0
	for k, c := range counts {
		if c > bestCount || (c == bestCount && k < best) {
			best = k
			bestCount = c
		}
	}
	return best
}

func estimateTokens(commonPrefix string, files, symbols []string) int {
	chars := len(commonPrefix)
	for _, f := range files {
		chars += len(f) + 1
	}
	for _, s := range symbols {
		chars += len(s) + 1
	}
	tokens := chars / 4
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

func setToSortedSlice(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isAllUpper(s string) bool {
	hasLetter := false
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			return false
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		}
	}
	return hasLetter
}

func hashClusterID(commonPrefix string) string {
	h := sha256.Sum256([]byte(commonPrefix))
	return hex.EncodeToString(h[:8])
}
