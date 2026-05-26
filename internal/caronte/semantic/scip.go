// SPDX-License-Identifier: MIT
package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type IndexerKind string

const (
	IndexerSCIPTypeScript IndexerKind = "scip-typescript"

	IndexerSCIPPython IndexerKind = "scip-python"

	IndexerRustAnalyzer IndexerKind = "rust-analyzer"
)

func (k IndexerKind) Binary() string { return string(k) }

func IndexerKindForLanguage(language string) (IndexerKind, bool) {
	switch language {
	case "typescript":
		return IndexerSCIPTypeScript, true
	case "python":
		return IndexerSCIPPython, true
	case "rust":
		return IndexerRustAnalyzer, true
	default:
		return "", false
	}
}

type SCIPRunner interface {
	Available(kind IndexerKind) bool
	Index(ctx context.Context, kind IndexerKind, srcDir string) ([]byte, error)
}

type osSCIPRunner struct {
	lookPath func(string) (string, error)
}

func NewOSSCIPRunner() SCIPRunner { return &osSCIPRunner{lookPath: exec.LookPath} }

func newOSSCIPRunnerForTest(lookPath func(string) (string, error)) *osSCIPRunner {
	return &osSCIPRunner{lookPath: lookPath}
}

// Available reports whether the indexer binary resolves in $PATH. A nil
// lookPath (programmer error) reports false (degrade, do not panic).
func (r *osSCIPRunner) Available(kind IndexerKind) bool {
	if r.lookPath == nil {
		return false
	}
	_, err := r.lookPath(kind.Binary())
	return err == nil
}

func (r *osSCIPRunner) Index(ctx context.Context, kind IndexerKind, srcDir string) ([]byte, error) {
	bin, err := r.lookPath(kind.Binary())
	if err != nil {
		return nil, fmt.Errorf("caronte/semantic: %s not in PATH: %w", kind.Binary(), err)
	}
	args := indexerArgs(kind)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = srcDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return nil, fmt.Errorf("caronte/semantic: %s index failed: %w (stderr: %s)", kind.Binary(), runErr, stderr.String())
	}
	return stdout.Bytes(), nil
}

func indexerArgs(kind IndexerKind) []string {
	switch kind {
	case IndexerSCIPTypeScript:
		return []string{"index", "--output", "-", "--emit-json"}
	case IndexerSCIPPython:
		return []string{"index", "--output", "-", "--emit-json"}
	case IndexerRustAnalyzer:
		return []string{"scip", ".", "--output", "-"}
	default:
		return nil
	}
}

type scipIndex struct {
	Documents []scipDocument `json:"documents"`
}

type scipDocument struct {
	RelativePath string           `json:"relative_path"`
	Language     string           `json:"language"`
	Symbols      []scipSymbolInfo `json:"symbols"`
	Occurrences  []scipOccurrence `json:"occurrences"`
}

type scipSymbolInfo struct {
	Symbol        string             `json:"symbol"`
	Relationships []scipRelationship `json:"relationships"`
}

type scipRelationship struct {
	Symbol           string `json:"symbol"`
	IsImplementation bool   `json:"is_implementation"`
	IsReference      bool   `json:"is_reference"`
}

type scipOccurrence struct {
	Symbol          string `json:"symbol"`
	SymbolRoles     int    `json:"symbol_roles"`
	EnclosingSymbol string `json:"enclosing_symbol"`
	Range           []int  `json:"range"`
}

const scipRoleDefinition = 1

type scipPos struct {
	file string
	line int
}

func parseSCIPIndex(raw []byte, language string, lookup func(file string, line int) (string, bool)) ([]store.Edge, error) {
	var idx scipIndex
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("caronte/semantic: parse SCIP index (%s): %w", language, err)
	}

	defPos := make(map[string]scipPos)
	for _, doc := range idx.Documents {
		for _, occ := range doc.Occurrences {
			if occ.SymbolRoles&scipRoleDefinition == 0 || occ.Symbol == "" || len(occ.Range) == 0 {
				continue
			}
			if _, seen := defPos[occ.Symbol]; !seen {

				defPos[occ.Symbol] = scipPos{file: doc.RelativePath, line: occ.Range[0] + 1}
			}
		}
	}

	resolve := func(symbol string) (string, bool) {
		p, ok := defPos[symbol]
		if !ok {
			return "", false
		}
		return lookup(p.file, p.line)
	}

	var edges []store.Edge
	for _, doc := range idx.Documents {

		for _, sym := range doc.Symbols {
			for _, rel := range sym.Relationships {
				if !rel.IsImplementation {
					continue
				}
				src, ok := resolve(sym.Symbol)
				dst, ok2 := resolve(rel.Symbol)
				if !ok || !ok2 {

					continue
				}
				edges = append(edges, store.Edge{
					SourceID:   src,
					TargetID:   dst,
					Kind:       string(store.EdgeImplements),
					Confidence: store.ConfSCIPImpl,
				})
			}
		}

		for _, occ := range doc.Occurrences {
			if occ.SymbolRoles&scipRoleDefinition != 0 {
				continue
			}
			if occ.EnclosingSymbol == "" || occ.Symbol == "" {
				continue
			}
			src, ok := resolve(occ.EnclosingSymbol)
			dst, ok2 := resolve(occ.Symbol)
			if !ok || !ok2 {

				continue
			}
			line := 0
			if len(occ.Range) > 0 {
				line = occ.Range[0] + 1
			}
			edges = append(edges, store.Edge{
				SourceID:   src,
				TargetID:   dst,
				Kind:       string(store.EdgeReferences),
				Confidence: store.ConfSCIPImpl,
				SiteFile:   doc.RelativePath,
				SiteLine:   line,
			})
		}
	}

	sortEdgesByKey(edges)
	return edges, nil
}

func sortEdgesByKey(edges []store.Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SiteLine < b.SiteLine
	})
}
