//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type StalenessChecker struct {
	store    *store.Store
	repoRoot string
	git      GitProber
}

func NewStalenessChecker(s *store.Store, repoRoot string, git GitProber) *StalenessChecker {
	return &StalenessChecker{store: s, repoRoot: repoRoot, git: git}
}

func (c *StalenessChecker) Recompute(ctx context.Context) error {

	rows, err := c.store.DB().QueryContext(ctx, `
		SELECT adr_id, node_id, link_kind
		FROM adr_links
		WHERE node_id <> '' AND link_kind IN (?, ?)`,
		string(store.LinkExplicitRef), string(store.LinkSemantic),
	)
	if err != nil {
		return fmt.Errorf("caronte/intent: staleness list links: %w", err)
	}
	type cand struct{ adrID, nodeID, kind string }
	var cands []cand
	for rows.Next() {
		var cc cand
		if err := rows.Scan(&cc.adrID, &cc.nodeID, &cc.kind); err != nil {
			rows.Close()
			return fmt.Errorf("caronte/intent: staleness scan: %w", err)
		}
		cands = append(cands, cc)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("caronte/intent: staleness rows: %w", err)
	}
	rows.Close()

	adrTouch := map[string]int64{}
	for _, cc := range cands {
		adrTS, ok := adrTouch[cc.adrID]
		if !ok {
			adrTS = c.lastTouched(ctx, cc.adrID)
			adrTouch[cc.adrID] = adrTS
		}

		node, gerr := c.store.GetNode(ctx, cc.nodeID)
		if gerr != nil {

			continue
		}
		codeTS := c.lastTouched(ctx, filepath.ToSlash(node.FilePath))
		stale := codeTS > adrTS
		if err := c.store.SetADRLinkStale(ctx, cc.adrID, cc.nodeID, store.LinkKind(cc.kind), stale); err != nil {
			return fmt.Errorf("caronte/intent: set stale %s→%s: %w", cc.nodeID, cc.adrID, err)
		}
	}
	return nil
}

func (c *StalenessChecker) lastTouched(ctx context.Context, repoRel string) int64 {
	if c.git != nil {
		if ts, ok := c.git.LastTouchedUnix(ctx, repoRel); ok {
			return ts
		}
	}

	info, err := os.Stat(filepath.Join(c.repoRoot, filepath.FromSlash(repoRel)))
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}
