//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Engine struct {
	store     *store.Store
	semantic  *SemanticIndexer
	adrTitles map[string]string
}

func NewEngine(s *store.Store, semantic *SemanticIndexer, adrTitles map[string]string) *Engine {
	if adrTitles == nil {
		adrTitles = map[string]string{}
	}
	return &Engine{store: s, semantic: semantic, adrTitles: adrTitles}
}

func (e *Engine) GetWhy(ctx context.Context, subject string) (WhyAnswer, error) {
	ans := WhyAnswer{Subject: subject}
	if e.store == nil {
		ans.Degraded = true
		return ans, nil
	}

	nodeIDs, packageID, err := e.resolveSubject(ctx, subject)
	if err != nil {
		return WhyAnswer{}, err
	}

	adrSeen := map[string]struct{}{}
	for _, nid := range nodeIDs {
		links, lerr := e.store.ListADRLinksForNode(ctx, nid)
		if lerr != nil {
			return WhyAnswer{}, fmt.Errorf("caronte/intent: list links %s: %w", nid, lerr)
		}
		e.appendLinks(&ans, links, adrSeen)
	}
	if packageID != "" {
		pkgLinks, perr := e.packageLinks(ctx, packageID)
		if perr != nil {
			return WhyAnswer{}, perr
		}
		e.appendLinks(&ans, pkgLinks, adrSeen)
	}

	if e.semantic != nil {

		queryKey := subject
		if len(nodeIDs) == 1 && nodeIDs[0] != subject {
			queryKey = nodeIDs[0]
		}
		passages, serr := e.semantic.RetrieveForSymbol(ctx, queryKey)
		if serr != nil {
			return WhyAnswer{}, fmt.Errorf("caronte/intent: semantic %q: %w", subject, serr)
		}
		ans.SemanticPassages = passages
	} else {
		ans.Degraded = true
	}

	loreSeen := map[string]struct{}{}
	for _, nid := range nodeIDs {
		trailers, terr := e.store.ListLoreTrailersForNode(ctx, nid)
		if terr != nil {
			return WhyAnswer{}, fmt.Errorf("caronte/intent: list lore %s: %w", nid, terr)
		}
		for _, tr := range trailers {
			key := tr.CommitSHA + "|" + tr.TrailerKind + "|" + tr.Body
			if _, dup := loreSeen[key]; dup {
				continue
			}
			loreSeen[key] = struct{}{}
			ans.LoreTrailers = append(ans.LoreTrailers, LoreEntry{
				CommitSHA: tr.CommitSHA, TrailerKind: tr.TrailerKind, Body: tr.Body, AuthoredAt: tr.AuthoredAt,
			})
		}
	}
	return ans, nil
}

func (e *Engine) resolveSubject(ctx context.Context, subject string) ([]string, string, error) {

	if node, err := e.store.GetNode(ctx, subject); err == nil {
		return []string{subject}, node.PackageID, nil
	}

	rows, err := e.store.DB().QueryContext(ctx,
		`SELECT node_id FROM graph_nodes WHERE file_path = ? ORDER BY node_id`, subject)
	if err != nil {
		return nil, "", fmt.Errorf("caronte/intent: resolve file %q: %w", subject, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", fmt.Errorf("caronte/intent: scan file node: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("caronte/intent: file node rows: %w", err)
	}
	if len(ids) == 0 {

		return []string{subject}, "", nil
	}
	pkg := fileDir(subject)
	return ids, pkg, nil
}

func fileDir(p string) string {
	slash := strings.ReplaceAll(p, "\\", "/")
	idx := strings.LastIndex(slash, "/")
	if idx < 0 {
		return ""
	}
	return slash[:idx]
}

func (e *Engine) packageLinks(ctx context.Context, packageID string) ([]store.ADRLink, error) {
	rows, err := e.store.DB().QueryContext(ctx, `
		SELECT adr_id, node_id, package_id, link_kind, confidence, stale
		FROM adr_links WHERE package_id = ? AND node_id = '' ORDER BY adr_id, link_kind`, packageID)
	if err != nil {
		return nil, fmt.Errorf("caronte/intent: package links %q: %w", packageID, err)
	}
	defer rows.Close()
	var out []store.ADRLink
	for rows.Next() {
		var l store.ADRLink
		var conf float64
		var stale int
		if err := rows.Scan(&l.ADRID, &l.NodeID, &l.PackageID, &l.LinkKind, &conf, &stale); err != nil {
			return nil, fmt.Errorf("caronte/intent: scan package link: %w", err)
		}
		l.Confidence = conf
		l.Stale = stale != 0
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caronte/intent: package link rows: %w", err)
	}
	return out, nil
}

func (e *Engine) appendLinks(ans *WhyAnswer, links []store.ADRLink, seen map[string]struct{}) {
	for _, l := range links {
		key := l.ADRID + "|" + l.NodeID + "|" + l.LinkKind
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		ans.LinkedADRs = append(ans.LinkedADRs, LinkedADR{
			ADRID:      l.ADRID,
			ADRTitle:   e.adrTitles[l.ADRID],
			LinkKind:   l.LinkKind,
			Confidence: l.Confidence,
			Stale:      l.Stale,
		})
	}
}
