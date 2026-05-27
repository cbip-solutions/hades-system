//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"context"
	"fmt"

	caronteevo "github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var allNodeKinds = store.AllNodeKinds()

type LoreIndexer struct {
	store  *store.Store
	runner caronteevo.GitRunner
}

func NewLoreIndexer(s *store.Store, runner caronteevo.GitRunner) *LoreIndexer {
	return &LoreIndexer{store: s, runner: runner}
}

func (li *LoreIndexer) IndexLore(ctx context.Context, projectID, repoDir string) (LoreIndexResult, error) {

	fileNodes, err := li.buildFileNodeIndex(ctx)
	if err != nil {
		return LoreIndexResult{}, fmt.Errorf("caronte/intent: build file-node index: %w", err)
	}

	// Read the full history through the GitRunner. The format is
	// package-constructed (no user input); --name-only yields the touched
	// files. We do NOT pass --since (Lore intent never expires).
	out, err := li.runner.Log(ctx, repoDir,
		"--no-merges",
		"--pretty=format:%H"+loreFieldSep+"%ae"+loreFieldSep+"%ct"+loreFieldSep+"%B"+loreRecSep,
		"--name-only",
	)
	if err != nil {
		return LoreIndexResult{}, fmt.Errorf("caronte/intent: git log: %w", err)
	}

	commits := parseLoreLog(out)
	res := LoreIndexResult{CommitsScanned: len(commits)}
	for _, c := range commits {
		trailers := parseTrailerLines(c.body)
		res.Trailers += len(trailers)
		if len(trailers) == 0 {
			continue
		}
		primaryFile, primaryNode := primaryTouchedNode(c.files, fileNodes)
		for _, tr := range trailers {
			if err := li.store.UpsertLoreTrailer(ctx, store.LoreTrailer{
				CommitSHA:   c.sha,
				FilePath:    primaryFile,
				NodeID:      primaryNode,
				TrailerKind: string(tr.Kind),
				Body:        tr.Body,
				AuthoredAt:  c.unixTime,
			}); err != nil {
				return res, fmt.Errorf("caronte/intent: upsert lore (commit %s): %w", c.sha, err)
			}
			res.WrittenRows++
		}
	}
	return res, nil
}

func (li *LoreIndexer) buildFileNodeIndex(ctx context.Context) (map[string][]string, error) {
	idx := make(map[string][]string)
	for _, kind := range allNodeKinds {
		nodes, err := li.store.ListNodesByKind(ctx, kind)
		if err != nil {
			return nil, fmt.Errorf("list %s nodes: %w", kind, err)
		}
		for _, n := range nodes {
			idx[n.FilePath] = append(idx[n.FilePath], n.NodeID)
		}
	}
	return idx, nil
}
