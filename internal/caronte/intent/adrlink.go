// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var adrFileName = regexp.MustCompile(`^[0-9]{4}-.+\.md$`)

var adrRefInText = regexp.MustCompile(`ADR-[0-9]{4}`)

var backtickPath = regexp.MustCompile("`([a-zA-Z0-9_./-]+)`")

const goFileExt = ".go"

type ADRLinker struct {
	store    *store.Store
	repoRoot string
}

func NewADRLinker(s *store.Store, repoRoot string) *ADRLinker {
	return &ADRLinker{store: s, repoRoot: repoRoot}
}

func (l *ADRLinker) docDirs() []string {
	return []string{
		filepath.Join("docs", "decisions"),
		filepath.Join("docs", "superpowers", "specs"),
	}
}

func (l *ADRLinker) IndexAndLink(ctx context.Context) error {

	refs, err := l.parseCorpus()
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := l.linkExplicitFromDoc(ctx, ref); err != nil {
			return err
		}
	}

	if err := l.linkExplicitFromCode(ctx); err != nil {
		return err
	}

	if err := l.linkCoverageManifest(ctx); err != nil {
		return err
	}
	return nil
}

func (l *ADRLinker) parseCorpus() ([]ADRRef, error) {
	var out []ADRRef
	for _, rel := range l.docDirs() {
		dir := filepath.Join(l.repoRoot, rel)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("caronte/intent: read corpus dir %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), "_") {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			ref, err := l.parseADRRef(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

func (l *ADRLinker) parseADRRef(absPath string) (ADRRef, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ADRRef{}, fmt.Errorf("caronte/intent: read doc %s: %w", absPath, err)
	}
	repoRel, relErr := filepath.Rel(l.repoRoot, absPath)
	if relErr != nil {
		repoRel = absPath
	}
	repoRel = filepath.ToSlash(repoRel)

	id, title, body := splitFrontmatter(string(data))
	if id == "" {
		id = repoRel
	}
	ref := ADRRef{
		RepoRel:    repoRel,
		ID:         id,
		Title:      title,
		Body:       body,
		CitedPaths: citedCodePaths(body),
	}
	return ref, nil
}

func splitFrontmatter(text string) (id, title, body string) {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024*8)
	lines := []string{}
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", text
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", text
	}
	for _, fl := range lines[1:end] {
		trimmed := strings.TrimSpace(fl)
		if strings.HasPrefix(trimmed, "id:") {
			id = strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
		}
		if strings.HasPrefix(trimmed, "title:") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "title:"))
		}
	}
	body = strings.Join(lines[end+1:], "\n")
	return id, title, body
}

func citedCodePaths(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range backtickPath.FindAllStringSubmatch(body, -1) {
		p := strings.TrimRight(m[1], "/")
		if !looksLikeRepoPath(p) {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func looksLikeRepoPath(p string) bool {
	if !strings.Contains(p, "/") {
		return false
	}
	for _, root := range []string{"internal/", "cmd/", "mcp/", "plugin/", "tests/", "scripts/", "docs/"} {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	return false
}

func packageOfCitedPath(p string) string {
	if strings.HasSuffix(p, goFileExt) {
		return filepath.ToSlash(filepath.Dir(p))
	}
	return strings.TrimRight(p, "/")
}

func (l *ADRLinker) linkExplicitFromDoc(ctx context.Context, ref ADRRef) error {
	for _, cited := range ref.CitedPaths {
		pkg := packageOfCitedPath(cited)

		if err := l.linkPackageNodes(ctx, ref.RepoRel, pkg, store.LinkExplicitRef); err != nil {
			return err
		}
	}
	return nil
}

func (l *ADRLinker) linkPackageNodes(ctx context.Context, adrRepoRel, pkg string, kind store.LinkKind) error {

	if err := l.store.UpsertADRLink(ctx, store.ADRLink{
		ADRID: adrRepoRel, NodeID: "", PackageID: pkg, LinkKind: string(kind), Confidence: 1.0, Stale: false,
	}); err != nil {
		return fmt.Errorf("caronte/intent: link package %s→%s: %w", pkg, adrRepoRel, err)
	}

	rows, err := l.store.DB().QueryContext(ctx, `SELECT node_id FROM graph_nodes WHERE package_id = ?`, pkg)
	if err != nil {
		return fmt.Errorf("caronte/intent: list package nodes %s: %w", pkg, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("caronte/intent: scan package node: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("caronte/intent: package node rows: %w", err)
	}
	for _, id := range ids {
		if err := l.store.UpsertADRLink(ctx, store.ADRLink{
			ADRID: adrRepoRel, NodeID: id, PackageID: pkg, LinkKind: string(kind), Confidence: 1.0, Stale: false,
		}); err != nil {
			return fmt.Errorf("caronte/intent: link node %s→%s: %w", id, adrRepoRel, err)
		}
	}
	return nil
}

func (l *ADRLinker) linkExplicitFromCode(ctx context.Context) error {
	adrPathByID, err := l.adrPathIndex()
	if err != nil {
		return err
	}
	srcRoots := []string{"internal", "cmd", "mcp", "plugin"}
	for _, sr := range srcRoots {
		root := filepath.Join(l.repoRoot, sr)
		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, goFileExt) {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil // skip unreadable file (do not fail the whole scan)
			}
			cited := adrRefInText.FindAllString(string(data), -1)
			if len(cited) == 0 {
				return nil
			}
			repoRel, _ := filepath.Rel(l.repoRoot, path)
			pkg := filepath.ToSlash(filepath.Dir(repoRel))
			for _, adrID := range dedupe(cited) {
				adrRepoRel, ok := adrPathByID[adrID]
				if !ok {
					continue
				}
				if err := l.linkPackageNodes(ctx, adrRepoRel, pkg, store.LinkExplicitRef); err != nil {
					return err
				}
			}
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("caronte/intent: scan code root %s: %w", sr, walkErr)
		}
	}
	return nil
}

// adrPathIndex maps ADR id (ADR-NNNN) → repo-rel path (architecture records).
//
// The id is sourced from each file's YAML frontmatter `id:` field (canonical
// per invariant); when the frontmatter is absent or has no usable `id:`
// field, the index falls back to filename-derivation ("ADR-" + first 4 chars).
//
// Why frontmatter wins: across the repo, ADR identity is declared in the
// frontmatter and the filename is a mnemonic — they can diverge legitimately
// (renumber-on-merge cycles update the frontmatter id but defer the file
// rename, or a deliberate decision keeps the original-author filename when
// the canonical id shifts). The linker MUST resolve ADR-NNNN via the
// canonical id; otherwise coverage_manifest + code-citation links silently
// drop the renumbered ADRs (the exact failure mode that surfaced
// TestCoverageManifestLinks pre-v0.20.2).
func (l *ADRLinker) adrPathIndex() (map[string]string, error) {
	out := map[string]string{}
	dir := filepath.Join(l.repoRoot, "docs", "decisions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("caronte/intent: index ADR paths: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !adrFileName.MatchString(e.Name()) {
			continue
		}
		repoRel := filepath.ToSlash(filepath.Join("docs", "decisions", e.Name()))

		var id string
		if data, rerr := os.ReadFile(filepath.Join(dir, e.Name())); rerr == nil {
			if fmID, _, _ := splitFrontmatter(string(data)); fmID != "" {
				id = fmID
			}
		}

		if id == "" {
			id = "ADR-" + e.Name()[:4]
		}
		out[id] = repoRel
	}
	return out, nil
}

func (l *ADRLinker) linkCoverageManifest(ctx context.Context) error {
	m, err := LoadCoverageManifest(ManifestPathFor(l.repoRoot))
	if err != nil {
		return fmt.Errorf("caronte/intent: load coverage manifest: %w", err)
	}
	adrPathByID, err := l.adrPathIndex()
	if err != nil {
		return err
	}
	for _, c := range m.Coverage {
		for _, adrID := range c.ADRs {
			adrRepoRel, ok := adrPathByID[adrID]
			if !ok {
				return fmt.Errorf("caronte/intent: manifest references %s but no docs/decisions/ file matches", adrID)
			}

			if err := l.store.UpsertADRLink(ctx, store.ADRLink{
				ADRID:      adrRepoRel,
				NodeID:     "",
				PackageID:  c.Package,
				LinkKind:   string(store.LinkCoverageManifest),
				Confidence: 1.0,
				Stale:      false,
			}); err != nil {
				return fmt.Errorf("caronte/intent: coverage link package %s→%s: %w", c.Package, adrRepoRel, err)
			}
		}
	}
	return nil
}

func dedupe(s []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
